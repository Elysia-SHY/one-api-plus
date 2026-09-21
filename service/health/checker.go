// Package health 定时探测上游渠道的可用性与延迟，维护渠道健康度，
// 异常渠道会被自动降权 / 暂停，供智能路由决策使用。
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/helper"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/relay/apitype"
	"github.com/Elysia-SHY/one-api-plus/relay/channeltype"
)

// Result 是一次探测的结果
type Result struct {
	ChannelId int    `json:"channel_id"`
	Channel   string `json:"channel"`
	Status    string `json:"status"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	CheckedAt int64  `json:"checked_at"`
}

// ---------------------------------------------------------------------------
// 探测 URL 构造（只做一次轻量 GET，不产生 token 消耗）
// ---------------------------------------------------------------------------

func probeURL(channel *model.Channel) (string, map[string]string) {
	base := channel.GetBaseURL()
	if base == "" && channel.Type >= 0 && channel.Type < len(channeltype.ChannelBaseURLs) {
		base = channeltype.ChannelBaseURLs[channel.Type]
	}
	base = trimSlash(base)
	if base == "" {
		return "", nil
	}
	cfg, _ := channel.LoadConfig()
	switch channeltype.ToAPIType(channel.Type) {
	case apitype.Anthropic:
		version := cfg.APIVersion
		if version == "" {
			version = "2023-06-01"
		}
		return base + "/v1/models", map[string]string{"x-api-key": channel.Key, "anthropic-version": version}
	case apitype.Gemini:
		sep := "?"
		if containsQ(base) {
			sep = "&"
		}
		return fmt.Sprintf("%s/%s/models%skey=%s", base, config.GeminiVersion, sep, channel.Key), nil
	default:
		if channel.Type == channeltype.Azure {
			version := cfg.APIVersion
			if version == "" {
				version = "2024-06-01"
			}
			return fmt.Sprintf("%s/openai/models?api-version=%s", base, version), map[string]string{"api-key": channel.Key}
		}
		return base + "/v1/models", map[string]string{"Authorization": "Bearer " + channel.Key}
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func containsQ(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '?' {
			return true
		}
	}
	return false
}

// CheckChannel 探测单个渠道并把结果写入健康表
func CheckChannel(channel *model.Channel) Result {
	result := Result{
		ChannelId: channel.Id,
		Channel:   channel.Name,
		CheckedAt: helper.GetTimestamp(),
	}
	url, headers := probeURL(channel)
	if url == "" {
		result.Error = "渠道没有可用的 Base URL"
		result.Status = config.HealthStatusUnknown
		return result
	}
	timeout := config.HealthCheckTimeout
	if timeout <= 0 {
		timeout = 15
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = err.Error()
		result.Status = config.HealthStatusDead
		_ = model.UpdateHealth(channel.Id, channel.Name, false, 0, err.Error())
		return result
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := int(time.Since(start).Milliseconds())
	result.LatencyMs = latency
	if err != nil {
		result.Error = err.Error()
		_ = model.UpdateHealth(channel.Id, channel.Name, false, latency, err.Error())
		h, _ := model.GetChannelHealth(channel.Id)
		result.Status = h.Status
		maybePause(channel)
		return result
	}
	defer resp.Body.Close()
	ok := resp.StatusCode < 500 || resp.StatusCode == http.StatusTooManyRequests
	if !ok {
		result.Error = fmt.Sprintf("上游返回状态码 %d", resp.StatusCode)
	}
	_ = model.UpdateHealth(channel.Id, channel.Name, ok, latency, result.Error)
	h, _ := model.GetChannelHealth(channel.Id)
	result.Status = h.Status
	if !ok {
		maybePause(channel)
	}
	return result
}

// maybePause 在健康度跌破阈值时自动暂停渠道
func maybePause(channel *model.Channel) {
	if !config.HealthCheckAutoPause {
		return
	}
	h, err := model.GetChannelHealth(channel.Id)
	if err != nil {
		return
	}
	if h.Status == config.HealthStatusDead || h.Status == config.HealthStatusPaused {
		if channel.Status == model.ChannelStatusEnabled {
			logger.SysLog(fmt.Sprintf("渠道 #%d 健康度为 %s，自动暂停", channel.Id, h.Status))
			model.UpdateChannelStatusById(channel.Id, model.ChannelStatusAutoDisabled)
		}
	}
}

// CheckAll 探测所有渠道
func CheckAll() ([]Result, error) {
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(channels))
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	for _, channel := range channels {
		wg.Add(1)
		sem <- struct{}{}
		go func(c *model.Channel) {
			defer wg.Done()
			defer func() { <-sem }()
			r := CheckChannel(c)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(channel)
	}
	wg.Wait()
	return results, nil
}

// Start 启动后台定时健康检测
func Start(intervalMinutes int) {
	if intervalMinutes <= 0 {
		intervalMinutes = config.HealthCheckInterval
	}
	go func() {
		for {
			time.Sleep(time.Duration(intervalMinutes) * time.Minute)
			if !config.HealthCheckEnabled {
				continue
			}
			logger.SysLog("channel health check started")
			results, err := CheckAll()
			if err != nil {
				logger.SysError("channel health check failed: " + err.Error())
				continue
			}
			bad := 0
			for _, r := range results {
				if r.Error != "" {
					bad++
				}
			}
			logger.SysLog(fmt.Sprintf("channel health check finished: %d channels, %d unhealthy", len(results), bad))
		}
	}()
}

// RecordResult 供渠道测试等外部流程直接上报一次探测结果
func RecordResult(channelId int, channelName string, ok bool, latencyMs int, errMsg string) {
	_ = model.UpdateHealth(channelId, channelName, ok, latencyMs, errMsg)
}

// RecordFailure 把一次真实中继失败计入健康度。
// 429 限流与配额类错误不计为渠道故障（是额度问题而不是线路问题）。
func RecordFailure(channelId int, channelName string, statusCode int, errMsg string) {
	if channelId <= 0 {
		return
	}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusPaymentRequired {
		return
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		// 鉴权失败属于渠道配置问题，照实记录
		_ = model.UpdateHealth(channelId, channelName, false, 0, errMsg)
		return
	}
	_ = model.UpdateHealth(channelId, channelName, false, 0, errMsg)
}
