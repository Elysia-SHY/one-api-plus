package responses

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

// randomSuffix 基于 seed 生成一个短后缀。
// 传入 id 时用它的哈希（同一次请求多次调用结果稳定），传 nil 时用随机数。
func randomSuffix(seed interface{}) string {
	if seed == nil {
		return fmt.Sprintf("%06x", rand.Int31n(0xFFFFFF+1)) + fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)
	}
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%v", seed)
	return fmt.Sprintf("%08x", h.Sum32())
}

// ---------------------------------------------------------------------------
// 响应仓库：为 previous_response_id 提供本地回溯
//
// 上游渠道基本都不认 previous_response_id，这里在网关侧保留最近若干条
// Responses 输出，客户端带 previous_response_id 时，把那一轮的 assistant
// 输出拼进 messages 里，让「多轮 Responses 会话」在没有服务端状态时也能连续。
// ---------------------------------------------------------------------------

const (
	storeKeyPrefix = "oneapiplus:resp:"
	storeTTL       = 24 * time.Hour
	maxStoreItems  = 500
)

var (
	memMu    sync.Mutex
	memStore = make(map[string]string)
	memOrder = make([]string, 0, maxStoreItems)
)

// Remember 存下一轮响应文本
// redisReady Redis 是否真正可用
//
// common.RedisEnabled 只表示「配置了 Redis」，不代表客户端已初始化。
// 在单元测试或部分启动路径下两者可能不一致，这里统一兜一层。
func redisReady() bool {
	return common.RedisEnabled && common.RDB != nil
}

func Remember(responseID string, text string) {
	if !config.ResponsesStoreItems || responseID == "" || text == "" {
		return
	}
	if redisReady() {
		if err := common.RedisSet(storeKeyPrefix+responseID, text, storeTTL); err != nil {
			logger.SysError("failed to store responses item: " + err.Error())
		}
		return
	}
	memMu.Lock()
	defer memMu.Unlock()
	if _, exists := memStore[responseID]; !exists {
		memOrder = append(memOrder, responseID)
	}
	memStore[responseID] = text
	for len(memOrder) > maxStoreItems {
		oldest := memOrder[0]
		memOrder = memOrder[1:]
		delete(memStore, oldest)
	}
}

// Recall 取回上一轮响应文本
func Recall(responseID string) string {
	if responseID == "" {
		return ""
	}
	if redisReady() {
		value, err := common.RedisGet(storeKeyPrefix + responseID)
		if err != nil || value == "" {
			return ""
		}
		return value
	}
	memMu.Lock()
	defer memMu.Unlock()
	return memStore[responseID]
}

// MarshalEvent 把一个 SSE 事件序列化成 wire 格式
func MarshalEvent(event StreamEvent) string {
	data, err := json.Marshal(event.Data)
	if err != nil {
		data = []byte("{}")
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, string(data))
}
