// Package modelsync 定时调用上游 /models 接口，自动发现新增模型并写入模型目录。
//
// 支持的上游协议：
//   - OpenAI 兼容（OpenAI / DeepSeek / Moonshot / OpenRouter / Groq / SiliconFlow / 自定义 ...）
//   - Anthropic Claude
//   - Google Gemini
//   - Azure OpenAI
package modelsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/helper"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/relay/apitype"
	"github.com/Elysia-SHY/one-api-plus/relay/channeltype"
)

// SyncResult 是一次同步的结果
type SyncResult struct {
	ChannelId  int      `json:"channel_id"`
	Channel    string   `json:"channel"`
	Source     string   `json:"source"`
	Total      int      `json:"total"`
	NewModels  []string `json:"new_models"`
	Error      string   `json:"error,omitempty"`
	SyncedTime int64    `json:"synced_time"`
}

// ---------------------------------------------------------------------------
// 上游 URL / 请求头构造
// ---------------------------------------------------------------------------

type probeTarget struct {
	URL     string
	Headers map[string]string
	Source  string
}

func baseURL(channel *model.Channel) string {
	if channel.GetBaseURL() != "" {
		return strings.TrimSuffix(channel.GetBaseURL(), "/")
	}
	if channel.Type >= 0 && channel.Type < len(channeltype.ChannelBaseURLs) {
		return strings.TrimSuffix(channeltype.ChannelBaseURLs[channel.Type], "/")
	}
	return ""
}

func buildTarget(channel *model.Channel) (*probeTarget, error) {
	base := baseURL(channel)
	if base == "" {
		return nil, fmt.Errorf("渠道 %d（%s）没有可用的 Base URL", channel.Id, channel.Name)
	}
	key := channel.Key
	cfg, _ := channel.LoadConfig()
	switch channeltype.ToAPIType(channel.Type) {
	case apitype.Anthropic:
		headers := map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}
		if cfg.APIVersion != "" {
			headers["anthropic-version"] = cfg.APIVersion
		}
		return &probeTarget{URL: base + "/v1/models", Headers: headers, Source: "anthropic"}, nil
	case apitype.Gemini:
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		version := config.GeminiVersion
		return &probeTarget{URL: fmt.Sprintf("%s/%s/models%skey=%s", base, version, sep, key), Headers: nil, Source: "gemini"}, nil
	case apitype.VertexAI:
		return nil, fmt.Errorf("渠道 %d 使用 VertexAI，暂不支持自动同步", channel.Id)
	default:
		// OpenAI 兼容：OpenAI / Azure / DeepSeek / OpenRouter / ...
		url := base + "/v1/models"
		headers := map[string]string{"Authorization": "Bearer " + key}
		if channel.Type == channeltype.Azure {
			version := cfg.APIVersion
			if version == "" {
				version = "2024-06-01"
			}
			url = fmt.Sprintf("%s/openai/models?api-version=%s", base, version)
			headers = map[string]string{"api-key": key}
			return &probeTarget{URL: url, Headers: headers, Source: "azure"}, nil
		}
		return &probeTarget{URL: url, Headers: headers, Source: "openai"}, nil
	}
}

// ---------------------------------------------------------------------------
// 响应解析
// ---------------------------------------------------------------------------

func parseModels(source string, body []byte) ([]string, string, error) {
	names := make([]string, 0)
	switch source {
	case "gemini":
		var payload struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", err
		}
		for _, m := range payload.Models {
			names = append(names, strings.TrimPrefix(m.Name, "models/"))
		}
		return dedup(names), "", nil
	default:
		// OpenAI / Anthropic / Azure 都是 {"data":[{"id": "..."}], "object": "list"}
		var payload struct {
			Data []struct {
				Id      string `json:"id"`
				OwnedBy string `json:"owned_by"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", err
		}
		ownedBy := ""
		for _, m := range payload.Data {
			if m.Id == "" {
				continue
			}
			names = append(names, m.Id)
			if ownedBy == "" {
				ownedBy = m.OwnedBy
			}
		}
		if len(names) == 0 {
			// 兼容部分上游直接返回数组
			var simple []struct {
				Id string `json:"id"`
			}
			if err := json.Unmarshal(body, &simple); err == nil {
				for _, m := range simple {
					if m.Id != "" {
						names = append(names, m.Id)
					}
				}
			}
		}
		return dedup(names), ownedBy, nil
	}
}

func dedup(names []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// ---------------------------------------------------------------------------
// 同步执行
// ---------------------------------------------------------------------------

// SyncChannel 同步单个渠道的模型列表
func SyncChannel(channel *model.Channel) SyncResult {
	result := SyncResult{
		ChannelId:  channel.Id,
		Channel:    channel.Name,
		SyncedTime: helper.GetTimestamp(),
	}
	target, err := buildTarget(channel)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Source = target.Source

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.ModelSyncTimeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.Error = err.Error()
		_ = model.UpdateHealth(channel.Id, channel.Name, false, 0, err.Error())
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("上游返回状态码 %d: %s", resp.StatusCode, truncate(string(body), 200))
		result.Error = msg
		_ = model.UpdateHealth(channel.Id, channel.Name, false, 0, msg)
		return result
	}
	names, _, err := parseModels(target.Source, body)
	if err != nil {
		msg := "解析上游模型列表失败: " + err.Error()
		result.Error = msg
		_ = model.UpdateHealth(channel.Id, channel.Name, false, 0, msg)
		return result
	}
	result.Total = len(names)
	newModels, err := model.UpsertCatalogModels(channel.Id, target.Source, names, config.ModelSyncDefaultStatus)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.NewModels = newModels
	if config.ModelSyncAutoEnable {
		for _, name := range newModels {
			if err := model.EnableCatalogModel(channel.Id, name); err != nil {
				logger.SysError(fmt.Sprintf("自动启用模型 %s 失败: %s", name, err.Error()))
			}
		}
	}
	if len(newModels) > 0 {
		logger.SysLog(fmt.Sprintf("渠道 #%d 同步到 %d 个模型，新增 %d 个: %s", channel.Id, result.Total, len(newModels), strings.Join(newModels, ", ")))
	}
	return result
}

// SyncAll 同步所有启用渠道，返回每个渠道的结果
func SyncAll(scope string) ([]SyncResult, error) {
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		return nil, err
	}
	pool := make(chan struct{}, config.ModelSyncConcurrency)
	if config.ModelSyncConcurrency <= 0 {
		pool = make(chan struct{}, 1)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]SyncResult, 0, len(channels))
	for _, channel := range channels {
		if channel.Status != model.ChannelStatusEnabled {
			continue
		}
		if scope == "enabled" && channel.Status != model.ChannelStatusEnabled {
			continue
		}
		wg.Add(1)
		pool <- struct{}{}
		go func(c *model.Channel) {
			defer wg.Done()
			defer func() { <-pool }()
			r := SyncChannel(c)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(channel)
	}
	wg.Wait()
	return results, nil
}

// Start 启动后台定时同步
func Start(intervalMinutes int) {
	if intervalMinutes <= 0 {
		intervalMinutes = config.ModelSyncInterval
	}
	go func() {
		for {
			time.Sleep(time.Duration(intervalMinutes) * time.Minute)
			if !config.ModelSyncEnabled {
				continue
			}
			logger.SysLog("model sync started")
			results, err := SyncAll("all")
			if err != nil {
				logger.SysError("model sync failed: " + err.Error())
				continue
			}
			added := 0
			for _, r := range results {
				added += len(r.NewModels)
			}
			logger.SysLog(fmt.Sprintf("model sync finished: %d channels, %d new models", len(results), added))
		}
	}()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
