// Package loadmeter 统计渠道的实时并发负载，为智能路由提供第四个打分维度。
//
// 之所以放在独立的包：健康度是「慢变化」的历史指标（分钟级），而并发负载是
// 「快变化」的瞬时指标（毫秒级），两者的更新频率与失效策略完全不同。
// 同一个渠道被打了 20 个并发长请求时，即使历史健康度再好，也应该把新请求
// 让给空闲渠道，否则会持续排队。
package routing

import (
	"sync"
	"time"
)

type entry struct {
	inflight   int64
	lastUpdate int64 // unix 秒，仅在 mu 保护下访问
}

var (
	mu      sync.RWMutex
	loads   = make(map[int]*entry)
	staleAt = 5 * time.Minute
)

// EnterLoad 在开始中继前调用，返回该渠道当前并发数
func EnterLoad(channelId int) int64 {
	mu.Lock()
	e, ok := loads[channelId]
	if !ok {
		e = &entry{}
		loads[channelId] = e
	}
	e.inflight++
	e.lastUpdate = time.Now().Unix()
	current := e.inflight
	mu.Unlock()
	return current
}

// LeaveLoad 在中继结束后调用（务必配合 defer）
func LeaveLoad(channelId int) {
	mu.Lock()
	if e, ok := loads[channelId]; ok {
		e.inflight--
		if e.inflight < 0 {
			e.inflight = 0
		}
		e.lastUpdate = time.Now().Unix()
	}
	mu.Unlock()
}

// GetLoad 返回渠道当前并发数
func GetLoad(channelId int) int64 {
	mu.RLock()
	defer mu.RUnlock()
	if e, ok := loads[channelId]; ok {
		return e.inflight
	}
	return 0
}

// Snapshot 返回全部渠道的负载快照
func Snapshot() map[int]int64 {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[int]int64, len(loads))
	for k, v := range loads {
		out[k] = v.inflight
	}
	return out
}

// Total 返回全局在途请求数
func Total() int64 {
	mu.RLock()
	defer mu.RUnlock()
	var total int64
	for _, v := range loads {
		total += v.inflight
	}
	return total
}

// Cleanup 清理长时间没有更新的残留计数，防止进程重启前后的计数漂移
func Cleanup() {
	mu.Lock()
	defer mu.Unlock()
	cutoff := time.Now().Add(-staleAt).Unix()
	for k, v := range loads {
		if v.inflight <= 0 && v.lastUpdate < cutoff {
			delete(loads, k)
		}
	}
}
