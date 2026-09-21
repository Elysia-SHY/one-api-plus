// Package ratelimit 为 One API Plus 提供令牌桶限流。
//
// 三个维度，任一超限即拒绝：
//  1. QPM —— 用户/令牌维度，默认每分钟请求数（RATE_LIMIT_QPM）
//  2. 并发 —— 单用户同时在途的中继请求数（RATE_LIMIT_CONCURRENT）
//  3. 模型 —— 可选的「用户 + 模型」二级桶（RATE_LIMIT_PER_MODEL）
//
// 有 Redis 时用 Redis 计数（多实例共享），没有 Redis 时退化为进程内内存计数。
// 内存态在 RATE_LIMIT_CONCURRENT > 0 时同样有效——并发限制本来就是单实例语义。
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
)

// Result 描述一次限流判定的结果
type Result struct {
	Allowed    bool   `json:"allowed"`
	Limit      int    `json:"limit"`
	Remaining  int    `json:"remaining"`
	RetryAfter int    `json:"retry_after"` // 秒
	Reason     string `json:"reason"`
	Key        string `json:"key"`
}

// Allow 判定一次请求是否可以放行；key 是限流维度键（如 user:1 或 token:abc）
func Allow(key string, modelName string) Result {
	if !config.RateLimitEnabled {
		return Result{Allowed: true, Key: key}
	}
	bucketKey := "rl:qpm:" + key
	if config.RateLimitPerModel && modelName != "" {
		bucketKey = bucketKey + ":model:" + modelName
	}
	limit := config.RateLimitQPM
	if limit > 0 {
		remaining, retryAfter, ok := consumeToken(bucketKey, config.RateLimitBurst, limit)
		if !ok {
			return Result{
				Allowed:    false,
				Limit:      limit,
				Remaining:  0,
				RetryAfter: retryAfter,
				Reason:     "rate limit exceeded",
				Key:        bucketKey,
			}
		}
		return Result{Allowed: true, Limit: limit, Remaining: remaining, Key: bucketKey}
	}
	return Result{Allowed: true, Key: key}
}

// ---------------------------------------------------------------------------
// Redis 令牌桶：以 1 分钟为窗口做计数，配合 burst 允许短时突发
// ---------------------------------------------------------------------------

// redisReady Redis 是否真正可用：RedisEnabled 只说明配了 Redis，
// 不代表客户端已初始化（单元测试与部分启动路径下两者不一致）。
func redisReady() bool {
	return common.RedisEnabled && common.RDB != nil
}

func consumeToken(key string, burst int, qpm int) (int, int, bool) {
	if !redisReady() {
		return consumeTokenMemory(key, burst, qpm)
	}
	ctx := context.Background()
	if burst <= 0 {
		burst = 1
	}
	// 令牌按秒补充：每秒补充 qpm/60 个，桶容量为 burst
	now := time.Now()
	windowKey := fmt.Sprintf("%s:%d", key, now.Unix()%60)
	pipe := common.RDB.Pipeline()
	incr := pipe.Incr(ctx, windowKey)
	pipe.Expire(ctx, windowKey, 2*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis 故障时放行，不做自我保护式拒绝，避免 Redis 抖动导致全站不可用
		return consumeTokenMemory(key, burst, qpm)
	}
	used := int(incr.Val())
	// 简易滑动配额：当前分钟窗口用量 + 上一分钟窗口的一定权重不得超过 qpm
	prevKey := fmt.Sprintf("%s:%d", key, (now.Unix()+59)%60)
	prevVal, err := common.RedisGet(prevKey) //nolint:staticcheck // Redis 故障时已在上面降级
	prevUsed := 0
	if err == nil {
		_, _ = fmt.Sscanf(prevVal, "%d", &prevUsed)
	}
	elapsed := now.Second()
	carry := 0
	if elapsed < 60 && prevUsed > 0 {
		carry = int(float64(prevUsed) * (1 - float64(elapsed)/60.0))
	}
	effective := used + carry
	if effective > qpm+burst {
		return 0, 60 - elapsed, false
	}
	remaining := qpm - effective
	if remaining < 0 {
		remaining = 0
	}
	return remaining, 0, true
}

// ---------------------------------------------------------------------------
// 进程内降级实现
// ---------------------------------------------------------------------------

type windowCounter struct {
	window int64
	count  int
}

var (
	memMu    sync.Mutex
	memStore = make(map[string]*windowCounter)
)

func consumeTokenMemory(key string, burst int, qpm int) (int, int, bool) {
	now := time.Now().Unix() / 60
	memMu.Lock()
	defer memMu.Unlock()
	counter, ok := memStore[key]
	if !ok || counter.window != now {
		counter = &windowCounter{window: now, count: 0}
		memStore[key] = counter
	}
	counter.count++
	if counter.count > qpm+burst {
		return 0, 60 - int(time.Now().Second()%60), false
	}
	remaining := qpm - counter.count
	if remaining < 0 {
		remaining = 0
	}
	return remaining, 0, true
}

// CleanupMemory 清理过期的内存计数窗口
func CleanupMemory() {
	memMu.Lock()
	defer memMu.Unlock()
	now := time.Now().Unix() / 60
	for k, v := range memStore {
		if v.window < now-1 {
			delete(memStore, k)
		}
	}
}

// ---------------------------------------------------------------------------
// 并发闸门
// ---------------------------------------------------------------------------

var (
	concMu    sync.Mutex
	concStore = make(map[string]int)
)

// AcquireConcurrency 申请一个并发名额；limit <= 0 表示不限
func AcquireConcurrency(key string, limit int) bool {
	if limit <= 0 {
		return true
	}
	concMu.Lock()
	defer concMu.Unlock()
	if concStore[key] >= limit {
		return false
	}
	concStore[key]++
	return true
}

// ReleaseConcurrency 释放并发名额
func ReleaseConcurrency(key string) {
	concMu.Lock()
	defer concMu.Unlock()
	if concStore[key] > 0 {
		concStore[key]--
	}
}

// ConcurrencyOf 查询当前并发占用
func ConcurrencyOf(key string) int {
	concMu.Lock()
	defer concMu.Unlock()
	return concStore[key]
}

// ClearCounters 清空本地计数；仅用于测试与运维排障
func ClearCounters() {
	memMu.Lock()
	memStore = make(map[string]*windowCounter)
	memMu.Unlock()
	concMu.Lock()
	concStore = make(map[string]int)
	concMu.Unlock()
}
