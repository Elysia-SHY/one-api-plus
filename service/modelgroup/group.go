// Package modelgroup 实现「逻辑模型 → 真实模型」的组抽象。
//
// 用户侧只看到一个稳定名字（如 coding-assistant），网关在下层成员模型里
// 按策略挑出一个当前可用的真实模型转发：
//   - first_available：取优先级最高且当前有可用渠道的第一个
//   - random         ：按成员权重随机
//   - round_robin    ：轮转，尽量把流量摊平
//   - capability     ：先按能力要求（上下文/视觉/工具/推理）过滤，再按优先级与权重挑
//
// 这样上游换型号（GPT-4o → GPT-5、Claude 换版本）时，调用方无需改代码。
package modelgroup

import (
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/service/capability"
)

const cacheTTL = 60 * time.Second

var (
	cache    map[string]*model.ModelGroup
	cacheAt  time.Time
	mu       sync.RWMutex
	rrCursor uint64
)

// Refresh 重新加载模型组与其成员
func Refresh() error {
	if model.DB == nil {
		return nil
	}
	groups, err := model.GetAllModelGroups()
	if err != nil {
		return err
	}
	next := make(map[string]*model.ModelGroup, len(groups))
	for _, g := range groups {
		if g.GroupName == "" {
			continue
		}
		if !g.Enabled {
			continue
		}
		next[strings.ToLower(g.GroupName)] = g
	}
	mu.Lock()
	cache = next
	cacheAt = time.Now()
	mu.Unlock()
	return nil
}

func ensureLoaded() {
	mu.RLock()
	fresh := cache != nil && time.Since(cacheAt) < cacheTTL
	mu.RUnlock()
	if fresh {
		return
	}
	if err := Refresh(); err != nil {
		logger.SysError("failed to refresh model groups: " + err.Error())
	}
}

// IsGroup 判断一个模型名是否是逻辑组名
func IsGroup(modelName string) bool {
	if !config.ModelGroupEnabled || modelName == "" {
		return false
	}
	ensureLoaded()
	mu.RLock()
	group, ok := cache[strings.ToLower(modelName)]
	mu.RUnlock()
	return ok && group != nil && len(group.Members) > 0
}

// Members 返回一组的成员模型名（已按启用过滤）
func Members(groupName string) []string {
	ensureLoaded()
	mu.RLock()
	group := cache[strings.ToLower(groupName)]
	mu.RUnlock()
	if group == nil {
		return nil
	}
	names := make([]string, 0, len(group.Members))
	for _, m := range group.Members {
		if m != nil && m.Enabled && strings.TrimSpace(m.ModelName) != "" {
			names = append(names, m.ModelName)
		}
	}
	return names
}

// SelectOption 描述一次挑选的约束条件
type SelectOption struct {
	NeedVision    bool
	NeedToolCall  bool
	NeedReasoning bool
	MinContext    int
	// Exclude 需要排除的成员（上一轮失败的），用于组内 Fallback
	Exclude map[string]bool
	// Available 回调用于过滤当前真正有可用渠道的成员；nil 表示不做该过滤
	Available func(modelName string) bool
}

// Select 从逻辑组里选出一个真实模型名
func Select(groupName string, opt SelectOption) (string, bool) {
	if !config.ModelGroupEnabled || groupName == "" {
		return "", false
	}
	ensureLoaded()
	mu.RLock()
	group := cache[strings.ToLower(groupName)]
	mu.RUnlock()
	if group == nil || len(group.Members) == 0 {
		return "", false
	}
	// 1) 收集启用的成员
	members := make([]*model.ModelGroupMember, 0, len(group.Members))
	for _, m := range group.Members {
		if m == nil || !m.Enabled {
			continue
		}
		if opt.Exclude != nil && opt.Exclude[m.ModelName] {
			continue
		}
		members = append(members, m)
	}
	if len(members) == 0 {
		return "", false
	}
	// 2) 能力过滤
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.ModelName)
	}
	if opt.NeedVision || opt.NeedToolCall || opt.NeedReasoning || opt.MinContext > 0 {
		names = capability.Filter(names, opt.MinContext, opt.NeedVision, opt.NeedToolCall, opt.NeedReasoning)
		if len(names) == 0 {
			// 全部不满足要求时退回全部成员，交给渠道可用性兜底
			names = names[:0]
			for _, m := range members {
				names = append(names, m.ModelName)
			}
		}
	}
	// 3) 渠道可用性过滤
	if opt.Available != nil {
		available := make([]string, 0, len(names))
		for _, n := range names {
			if opt.Available(n) {
				available = append(available, n)
			}
		}
		if len(available) > 0 {
			names = available
		}
	}
	if len(names) == 0 {
		return "", false
	}
	// 4) 按策略挑一个
	strategy := group.Strategy
	if strings.TrimSpace(strategy) == "" {
		strategy = config.ModelGroupStrategy
	}
	switch strings.ToLower(strategy) {
	case config.GroupStrategyFirstAvailable:
		return names[0], true
	case config.GroupStrategyRoundRobin:
		idx := int(atomic.AddUint64(&rrCursor, 1)-1) % len(names)
		return names[idx], true
	case config.GroupStrategyRandom, config.GroupStrategyCapability:
		return weightedPick(members, names), true
	default:
		return names[0], true
	}
}

// weightedPick 在被选中的候选集合中按成员权重做加权随机
func weightedPick(members []*model.ModelGroupMember, names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	weightOf := make(map[string]float64, len(members))
	for _, m := range members {
		w := float64(m.Weight)
		if w <= 0 {
			w = 1
		}
		// 优先级作为量级因子：priority 3 的成员大致比 priority 0 高一个数量级
		w *= 1 + float64(m.Priority)
		weightOf[m.ModelName] = w
	}
	total := 0.0
	for _, n := range names {
		total += weightOf[n]
	}
	if total <= 0 {
		return names[rand.Intn(len(names))]
	}
	target := rand.Float64() * total
	acc := 0.0
	for _, n := range names {
		acc += weightOf[n]
		if acc >= target {
			return n
		}
	}
	return names[len(names)-1]
}

// All 返回全部模型组（含成员），供后台展示
func All() []*model.ModelGroup {
	ensureLoaded()
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*model.ModelGroup, 0, len(cache))
	for _, g := range cache {
		out = append(out, g)
	}
	return out
}

// Resolve 解析逻辑组名到真实模型名；不是组名时原样返回
func Resolve(modelName string, opt SelectOption) (string, bool) {
	if !IsGroup(modelName) {
		return modelName, false
	}
	target, ok := Select(modelName, opt)
	if !ok {
		return modelName, false
	}
	return target, true
}
