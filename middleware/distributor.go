package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/relay/channeltype"
	"github.com/Elysia-SHY/one-api-plus/service/alias"
	"github.com/Elysia-SHY/one-api-plus/service/modelgroup"
	"github.com/Elysia-SHY/one-api-plus/service/routing"
)

type ModelRequest struct {
	Model string `json:"model" form:"model"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		c.Set(ctxkey.Group, userGroup)
		var requestModel string
		var channel *model.Channel
		channelId, ok := c.Get(ctxkey.SpecificChannelId)
		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			if channel.Status != model.ChannelStatusEnabled {
				abortWithMessage(c, http.StatusForbidden, "该渠道已被禁用")
				return
			}
		} else {
			requestModel = c.GetString(ctxkey.RequestModel)
			// One API Plus: 先解析模型别名，再走智能路由
			requestModel = alias.Resolve(requestModel)
			originalRequestModel := requestModel
			// One API Plus: 模型组 —— 逻辑模型名解析成当前可用的真实模型
			if modelgroup.IsGroup(requestModel) {
				needs := parseRequestNeeds(c)
				selected, ok := modelgroup.Select(requestModel, modelgroup.SelectOption{
					NeedVision:    needs.vision,
					NeedToolCall:  needs.toolCall,
					NeedReasoning: needs.reasoning,
					MinContext:    needs.minContext,
					Available: func(name string) bool {
						return hasChannel(userGroup, name)
					},
				})
				if ok && selected != "" {
					logger.Debugf(ctx, "model group %s resolved to %s", requestModel, selected)
					c.Set(ctxkey.GroupResolvedFrom, requestModel)
					requestModel = selected
				}
			}
			c.Set(ctxkey.RequestModel, requestModel)
			// One API Plus: 强制校验 API Key 的模型白名单。
			// 令牌里可能配置的是「模型组逻辑名」，此时 requestModel 已被解析为真实模型名，
			// 因此逻辑名或解析后的真实名任一命中白名单即放行。
			if allowed := c.GetString(ctxkey.AvailableModels); allowed != "" {
				permitted := false
				for _, m := range strings.Split(allowed, ",") {
					m = strings.TrimSpace(m)
					if m == requestModel || m == originalRequestModel {
						permitted = true
						break
					}
				}
				if !permitted {
					abortWithMessage(c, http.StatusForbidden, fmt.Sprintf("该令牌无权访问模型 %s", requestModel))
					return
				}
			}
			var err error
			channel, err = routing.SelectChannel(userGroup, requestModel, nil)
			if err != nil {
				message := fmt.Sprintf("当前分组 %s 下对于模型 %s 无可用渠道", userGroup, requestModel)
				if channel != nil {
					logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
					message = "数据库一致性已被破坏，请联系管理员"
				}
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			}
		}
		logger.Debugf(ctx, "user id %d, user group: %s, request model: %s, using channel #%d", userId, userGroup, requestModel, channel.Id)
		SetupContextForSelectedChannel(c, channel, requestModel)
		c.Next()
	}
}

// requestNeeds 描述一次请求对模型能力的硬性要求
type requestNeeds struct {
	vision     bool
	toolCall   bool
	reasoning  bool
	minContext int
}

// parseRequestNeeds 从请求体里推断能力要求：带了图片必须视觉，带了 tools 必须工具调用，
// 显式给了 reasoning_effort 说明要推理模型；上下文需求按输入文本长度粗估。
func parseRequestNeeds(c *gin.Context) requestNeeds {
	needs := requestNeeds{}
	body, err := common.GetRequestBody(c)
	if err != nil || len(body) == 0 {
		return needs
	}
	var payload struct {
		Messages  []json.RawMessage `json:"messages"`
		Tools     []json.RawMessage `json:"tools"`
		Functions []json.RawMessage `json:"functions"`
		Reasoning json.RawMessage   `json:"reasoning_effort"`
		MaxTokens int               `json:"max_tokens"`
		Input     json.RawMessage   `json:"input"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return needs
	}
	if len(payload.Tools) > 0 || len(payload.Functions) > 0 {
		needs.toolCall = true
	}
	if len(payload.Reasoning) > 2 {
		needs.reasoning = true
	}
	raw := strings.ToLower(string(body))
	if strings.Contains(raw, "image_url") || strings.Contains(raw, "input_image") ||
		strings.Contains(raw, "base64") {
		needs.vision = true
	}
	// 粗估：请求体每 4 个字符约 1 token，再留 2 倍余量给回复
	needs.minContext = len(body)/2 + payload.MaxTokens*2
	return needs
}

// hasChannel 判断某模型在当前分组下是否有可用渠道
func hasChannel(group string, modelName string) bool {
	channels, err := model.GetChannelsForModel(group, modelName)
	return err == nil && len(channels) > 0
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) {
	c.Set(ctxkey.Channel, channel.Type)
	c.Set(ctxkey.ChannelId, channel.Id)
	c.Set(ctxkey.ChannelName, channel.Name)
	if channel.SystemPrompt != nil && *channel.SystemPrompt != "" {
		c.Set(ctxkey.SystemPrompt, *channel.SystemPrompt)
	}
	c.Set(ctxkey.ModelMapping, channel.GetModelMapping())
	c.Set(ctxkey.OriginalModel, modelName) // for retry
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
	c.Set(ctxkey.BaseURL, channel.GetBaseURL())
	cfg, _ := channel.LoadConfig()
	// this is for backward compatibility
	if channel.Other != nil {
		switch channel.Type {
		case channeltype.Azure:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.Xunfei:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.Gemini:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.AIProxyLibrary:
			if cfg.LibraryID == "" {
				cfg.LibraryID = *channel.Other
			}
		case channeltype.Ali:
			if cfg.Plugin == "" {
				cfg.Plugin = *channel.Other
			}
		}
	}
	c.Set(ctxkey.Config, cfg)
}
