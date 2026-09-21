// Package responsecache 在 Redis 中缓存完全一致的非流式请求结果，
// 命中时直接返回上游响应，不消耗额度也不产生 token 计费。
package responsecache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

const keyPrefix = "oneapiplus:resp:"

// Enabled 缓存是否真正可用
func Enabled() bool {
	return config.ResponseCacheEnabled && common.RedisEnabled
}

// BuildKey 根据请求内容生成缓存 key；返回空字符串表示不可缓存
func BuildKey(userId int, path string, body []byte) string {
	if !Enabled() || len(body) == 0 {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if v, ok := payload["stream"]; ok {
		if b, isBool := v.(bool); isBool && b {
			return ""
		}
	}
	// 工具调用 / 随机性参数不缓存
	for _, field := range []string{"tools", "tool_choice", "functions", "function_call"} {
		if _, ok := payload[field]; ok {
			return ""
		}
	}
	if v, ok := payload["temperature"]; ok {
		if f, isFloat := v.(float64); isFloat && f > 0 {
			return ""
		}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", userId, path, string(body))))
	return keyPrefix + hex.EncodeToString(sum[:])
}

// Get 读取缓存响应，第二个返回值表示是否命中
func Get(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	value, err := common.RedisGet(key)
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}

// Set 写回缓存
func Set(key string, body string) {
	if key == "" || body == "" {
		return
	}
	if len(body) > config.ResponseCacheMaxBody {
		return
	}
	ttl := time.Duration(config.ResponseCacheTTL) * time.Second
	if err := common.RedisSet(key, body, ttl); err != nil {
		logger.SysError("failed to cache response: " + err.Error())
	}
}

// ShouldCacheResponse 判断上游响应是否值得缓存（只缓存成功的完整响应）
func ShouldCacheResponse(statusCode int, body string) bool {
	if statusCode < 200 || statusCode >= 300 {
		return false
	}
	if body == "" || strings.Contains(body, "\"error\"") {
		return false
	}
	return true
}
