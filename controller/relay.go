package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/common/helper"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/middleware"
	"github.com/Elysia-SHY/one-api-plus/monitor"
	"github.com/Elysia-SHY/one-api-plus/relay/controller"
	"github.com/Elysia-SHY/one-api-plus/relay/model"
	"github.com/Elysia-SHY/one-api-plus/relay/relaymode"
	"github.com/Elysia-SHY/one-api-plus/service/health"
	"github.com/Elysia-SHY/one-api-plus/service/promptcache"
	"github.com/Elysia-SHY/one-api-plus/service/responsecache"
	"github.com/Elysia-SHY/one-api-plus/service/routing"
	"github.com/gin-gonic/gin"
)

// https://platform.openai.com/docs/api-reference/chat

func relayHelper(c *gin.Context, relayMode int) *model.ErrorWithStatusCode {
	var err *model.ErrorWithStatusCode
	switch relayMode {
	case relaymode.ImagesGenerations:
		err = controller.RelayImageHelper(c, relayMode)
	case relaymode.AudioSpeech:
		fallthrough
	case relaymode.AudioTranslation:
		fallthrough
	case relaymode.AudioTranscription:
		err = controller.RelayAudioHelper(c, relayMode)
	case relaymode.Proxy:
		err = controller.RelayProxyHelper(c, relayMode)
	default:
		err = controller.RelayTextHelper(c)
	}
	return err
}

// responseRecorder 在不改变流式行为的前提下复制一份响应体，用于写入缓存
type responseRecorder struct {
	gin.ResponseWriter
	body []byte
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if len(r.body) < config.ResponseCacheMaxBody {
		r.body = append(r.body, b...)
	}
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) WriteString(s string) (int, error) {
	if len(r.body) < config.ResponseCacheMaxBody {
		r.body = append(r.body, s...)
	}
	return r.ResponseWriter.WriteString(s)
}

func (r *responseRecorder) Body() string {
	return string(r.body)
}

func Relay(c *gin.Context) {
	ctx := c.Request.Context()
	relayMode := relaymode.GetByPath(c.Request.URL.Path)
	if config.DebugEnabled {
		requestBody, _ := common.GetRequestBody(c)
		logger.Debugf(ctx, "request body: %s", string(requestBody))
	}
	channelId := c.GetInt(ctxkey.ChannelId)
	userId := c.GetInt(ctxkey.Id)
	channelName := c.GetString(ctxkey.ChannelName)

	// 第二阶段：负载计量。必须在任何重试之前 Enter，在整条链路结束时 Leave，
	// 这样路由看到的「在途请求数」才包含重试叠加的真实压力。
	routing.EnterLoad(channelId)
	defer func() {
		routing.LeaveLoad(c.GetInt(ctxkey.ChannelId))
	}()

	// One API Plus：前缀级上下文缓存（命中率高于整请求缓存，优先命中）
	promptKey := ""
	if promptcache.Enabled() {
		body, _ := common.GetRequestBody(c)
		if key, ok := promptcache.Key(userId, channelId, c.Request.URL.Path, body); ok {
			promptKey = key
			if cached, hit := promptcache.Get(key); hit {
				c.Header("X-One-Api-Plus-Prompt-Cache", "hit")
				c.Header("Content-Type", "application/json")
				c.String(http.StatusOK, cached)
				return
			}
		}
	}

	// One API Plus：完全相同的非流式请求直接复用缓存，不再消耗额度
	cacheKey := ""
	var recorder *responseRecorder
	if responsecache.Enabled() {
		body, _ := common.GetRequestBody(c)
		cacheKey = responsecache.BuildKey(userId, c.Request.URL.Path, body)
		if cacheKey != "" {
			if cached, hit := responsecache.Get(cacheKey); hit {
				c.Header("X-One-Api-Plus-Cache", "hit")
				c.Header("Content-Type", "application/json")
				c.String(http.StatusOK, cached)
				return
			}
			recorder = &responseRecorder{ResponseWriter: c.Writer}
			c.Writer = recorder
		}
	}

	bizErr := relayHelper(c, relayMode)
	if bizErr == nil {
		monitor.Emit(channelId, true)
		// 第二阶段：成功也要回写健康度，否则没有探测任务时健康表永远空着
		health.RecordResult(channelId, channelName, true, 0, "")
		if promptKey != "" && recorder != nil {
			if responsecache.ShouldCacheResponse(c.Writer.Status(), recorder.Body()) {
				c.Header("X-One-Api-Plus-Prompt-Cache", "miss")
				promptcache.Set(promptKey, recorder.Body())
			}
		}
		if recorder != nil {
			c.Header("X-One-Api-Plus-Cache", "miss")
			if responsecache.ShouldCacheResponse(c.Writer.Status(), recorder.Body()) {
				responsecache.Set(cacheKey, recorder.Body())
			}
		}
		return
	}
	lastFailedChannelId := channelId
	group := c.GetString(ctxkey.Group)
	originalModel := c.GetString(ctxkey.OriginalModel)
	go processChannelRelayError(ctx, userId, channelId, channelName, *bizErr)
	// One API Plus: 记录本次请求已经失败的渠道，供重试 / fallback 排除
	failedChannels := map[int]bool{channelId: true}
	health.RecordFailure(channelId, channelName, bizErr.StatusCode, bizErr.Error.Message)
	requestId := c.GetString(helper.RequestIdKey)
	retryTimes := config.RetryTimes
	if !shouldRetry(c, bizErr.StatusCode) {
		logger.Errorf(ctx, "relay error happen, status code is %d, won't retry in this case", bizErr.StatusCode)
		retryTimes = 0
	}
	for i := retryTimes; i > 0; i-- {
		channel, err := routing.SelectChannel(group, originalModel, failedChannels)
		if err != nil {
			logger.Errorf(ctx, "routing.SelectChannel failed: %+v", err)
			break
		}
		logger.Infof(ctx, "using channel #%d to retry (remain times %d)", channel.Id, i)
		if channel.Id == lastFailedChannelId {
			continue
		}
		middleware.SetupContextForSelectedChannel(c, channel, originalModel)
		requestBody, err := common.GetRequestBody(c)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		bizErr = relayHelper(c, relayMode)
		if bizErr == nil {
			return
		}
		channelId := c.GetInt(ctxkey.ChannelId)
		lastFailedChannelId = channelId
		failedChannels[channelId] = true
		channelName := c.GetString(ctxkey.ChannelName)
		go processChannelRelayError(ctx, userId, channelId, channelName, *bizErr)
		health.RecordFailure(channelId, channelName, bizErr.StatusCode, bizErr.Error.Message)
	}

	// One API Plus: 同模型的渠道全部失败后，切换到备用模型（如 GPT -> Claude / DeepSeek）
	if bizErr != nil && config.FallbackEnabled && shouldRetry(c, bizErr.StatusCode) {
		for _, fallbackModel := range routing.FallbackModels(originalModel) {
			if fallbackModel == "" || fallbackModel == originalModel {
				continue
			}
			channel, err := routing.SelectChannel(group, fallbackModel, nil)
			if err != nil {
				logger.Errorf(ctx, "no channel available for fallback model %s: %s", fallbackModel, err.Error())
				continue
			}
			logger.Infof(ctx, "falling back to model %s with channel #%d", fallbackModel, channel.Id)
			if err = rewriteRequestModel(c, fallbackModel); err != nil {
				logger.Errorf(ctx, "failed to rewrite request model: %s", err.Error())
				continue
			}
			middleware.SetupContextForSelectedChannel(c, channel, fallbackModel)
			c.Set(ctxkey.RequestModel, fallbackModel)
			bizErr = relayHelper(c, relayMode)
			if bizErr == nil {
				return
			}
			failedChannelId := c.GetInt(ctxkey.ChannelId)
			fallbackChannelName := c.GetString(ctxkey.ChannelName)
			go processChannelRelayError(ctx, userId, failedChannelId, fallbackChannelName, *bizErr)
			health.RecordFailure(failedChannelId, fallbackChannelName, bizErr.StatusCode, bizErr.Error.Message)
		}
	}

	if bizErr != nil {
		if bizErr.StatusCode == http.StatusTooManyRequests {
			bizErr.Error.Message = "当前分组上游负载已饱和，请稍后再试"
		}

		// BUG: bizErr is in race condition
		bizErr.Error.Message = helper.MessageWithRequestId(bizErr.Error.Message, requestId)
		c.JSON(bizErr.StatusCode, gin.H{
			"error": bizErr.Error,
		})
	}
}

// rewriteRequestModel 在切换到备用模型时改写请求体里的 model 字段，
// 并同步更新缓存下来的请求体，保证后续解析拿到的是新模型名。
func rewriteRequestModel(c *gin.Context, modelName string) error {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	var payload map[string]interface{}
	if err = json.Unmarshal(body, &payload); err != nil {
		return err
	}
	payload["model"] = modelName
	newBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.Set(ctxkey.KeyRequestBody, newBody)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(newBody))
	c.Request.ContentLength = int64(len(newBody))
	c.Request.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
	return nil
}

func shouldRetry(c *gin.Context, statusCode int) bool {
	if _, ok := c.Get(ctxkey.SpecificChannelId); ok {
		return false
	}
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	if statusCode/100 == 5 {
		return true
	}
	if statusCode == http.StatusBadRequest {
		return false
	}
	if statusCode/100 == 2 {
		return false
	}
	return true
}

func processChannelRelayError(ctx context.Context, userId int, channelId int, channelName string, err model.ErrorWithStatusCode) {
	logger.Errorf(ctx, "relay error (channel id %d, user id: %d): %s", channelId, userId, err.Message)
	// https://platform.openai.com/docs/guides/error-codes/api-errors
	if monitor.ShouldDisableChannel(&err.Error, err.StatusCode) {
		monitor.DisableChannel(channelId, channelName, err.Message)
	} else {
		monitor.Emit(channelId, false)
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := model.Error{
		Message: "API not implemented",
		Type:    "one_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := model.Error{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}
