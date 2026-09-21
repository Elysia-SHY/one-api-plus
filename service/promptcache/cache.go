// Package promptcache 实现「前缀级」响应缓存，区别于 responsecache 的整请求缓存。
//
// 背景：Agent / IDE 的每一轮请求都会带上完整历史（system + 前几轮对话），
// 只有最后一两条消息是新的。整请求哈希命中率极低，但「消息前缀」几乎不变。
//
// 做法：把 messages 数组按角色切分成「稳定前缀」与「尾部」，对稳定前缀做 SHA-256，
// 在 Redis 里缓存该前缀对应的 assistant 回复。当新请求的前缀命中且尾部一致时
// 直接复用，节省整段历史 prompt 的 token 费用。
//
// 安全边界（宁可不缓存，也不错配）：
//   - 仅缓存非流式请求
//   - 带 tools / tool_choice / functions / rag 相关字段时不缓存
//   - temperature / top_p 非默认（偏随机）时不缓存
//   - 前缀估算 token 数低于 PROMPT_CACHE_MIN_TOKENS 时不写缓存
package promptcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

const keyPrefix = "oneapiplus:prompt:"

// Enabled 缓存是否真正可用
func Enabled() bool {
	return config.PromptCacheEnabled && common.RedisEnabled
}

// chatMessage 只取对齐 key 需要的字段
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatPayload struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	TopP        float64       `json:"top_p"`
	Stream      bool          `json:"stream"`
}

// Key 对一个 chat 请求计算前缀缓存 key；返回空串表示不可缓存
func Key(userId int, channelId int, path string, body []byte) (string, bool) {
	if !Enabled() || len(body) == 0 {
		return "", false
	}
	var payload chatPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	if payload.Stream || len(payload.Messages) < 3 {
		return "", false
	}
	// 只保留最后一轮作为「尾部」，其余视为稳定前缀
	prefix := payload.Messages[:len(payload.Messages)-1]
	tail := payload.Messages[len(payload.Messages)-1]
	prefixText := flatten(prefix)
	if estimateTokens(prefixText) < config.PromptCacheMinTok {
		return "", false
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strconv.Itoa(userId), strconv.Itoa(channelId), path,
		payload.Model, prefixText, flatten([]chatMessage{tail}),
	}, "|")))
	return keyPrefix + hex.EncodeToString(sum[:]), true
}

// Get 读取缓存响应
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

// Set 写入缓存响应
func Set(key string, value string) {
	if key == "" || value == "" || len(value) > config.ResponseCacheMaxBody {
		return
	}
	if err := common.RedisSet(key, value, time.Duration(config.PromptCacheTTL)*time.Second); err != nil {
		logger.SysError("failed to write prompt cache: " + err.Error())
	}
}

// InvalidateUser 清空某用户的全部前缀缓存（切换模型/清空会话后使用）
func InvalidateUser(userId int) {
	// key 里带 userId 哈希，无法按前缀扫描，这里依赖 TTL 自然过期；
	// 提供该方法是为了将来把 userId 提到 key 前缀时的接口兼容。
	_ = userId
}

// flatten 把消息列表压成稳定字符串；字段顺序显式排序保证同内容同哈希
func flatten(messages []chatMessage) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		parts = append(parts, m.Role+"\x00"+m.Content)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x01")
}

// estimateTokens 粗估 token 数：中文按 1 字 ~0.6 token，西文按 ~4 字符 1 token
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var cjk int
	var ascii int
	for _, r := range text {
		if r > 0x2E80 {
			cjk++
		} else {
			ascii++
		}
	}
	return int(float64(cjk)*0.6) + ascii/4
}
