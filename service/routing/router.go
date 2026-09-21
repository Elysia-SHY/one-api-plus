// Package routing 实现 One API Plus 的智能路由：同一个模型可绑定多个渠道，
// 按「延迟优先 / 成本优先 / 稳定性优先 / 权重 / 优先级」策略自动挑选最佳线路。
package routing

import (
	"errors"
	"math/rand"
	"sort"
	"strings"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/service/cost"
)

// Candidate 是一个可参与打分的候选渠道
type Candidate struct {
	Channel   *model.Channel       `json:"channel"`
	Health    *model.ChannelHealth `json:"health"`
	PricePerM float64              `json:"price_per_million"`
	Score     float64              `json:"score"`
}

// Strategy 返回当前生效的路由策略
func Strategy() string {
	switch strings.ToLower(strings.TrimSpace(config.RoutingStrategy)) {
	case config.StrategyWeight, config.StrategyLatency, config.StrategyCost, config.StrategyStability:
		return strings.ToLower(strings.TrimSpace(config.RoutingStrategy))
	default:
		return config.StrategyPriority
	}
}

// Candidates 返回分组下模型的所有候选渠道，按策略排序（分数高者优先）
func Candidates(group string, modelName string, exclude map[int]bool) ([]Candidate, error) {
	channels, err := model.GetChannelsForModel(group, modelName)
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(channels))
	for _, channel := range channels {
		if exclude != nil && exclude[channel.Id] {
			continue
		}
		health, err := model.GetChannelHealth(channel.Id)
		if err != nil {
			health = &model.ChannelHealth{
				ChannelId:    channel.Id,
				Status:       config.HealthStatusUnknown,
				WeightFactor: 0.8,
			}
		}
		input, _, _ := cost.GetPrice(modelName, channel.Type)
		candidates = append(candidates, Candidate{
			Channel:   channel,
			Health:    health,
			PricePerM: input,
		})
	}
	if len(candidates) == 0 {
		return nil, errors.New("无可用渠道")
	}
	score(candidates)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return candidates, nil
}

func score(candidates []Candidate) {
	strategy := Strategy()
	maxLatency := 0
	maxPrice := 0.0
	for _, c := range candidates {
		if c.Health.LatencyMs > maxLatency {
			maxLatency = c.Health.LatencyMs
		}
		if c.PricePerM > maxPrice {
			maxPrice = c.PricePerM
		}
	}
	if maxLatency == 0 {
		maxLatency = 1
	}
	if maxPrice == 0 {
		maxPrice = 1
	}
	for i := range candidates {
		c := &candidates[i]
		weight := 1.0
		if c.Channel.Weight != nil && *c.Channel.Weight > 0 {
			weight = float64(*c.Channel.Weight)
		}
		healthFactor := c.Health.WeightFactor
		if healthFactor <= 0 {
			healthFactor = 0.8
		}
		// 失效渠道几乎不再被选中，但仍保留一线机会，避免全部渠道被判死时无路可走
		if c.Health.Status == config.HealthStatusDead {
			healthFactor *= 0.05
		}
		switch strategy {
		case config.StrategyLatency:
			latencyScore := 1 - float64(minPositive(c.Health.LatencyMs, maxLatency))/float64(maxLatency)
			c.Score = latencyScore*100 + healthFactor*20 + weight
		case config.StrategyCost:
			priceScore := 1 - (c.PricePerM / maxPrice)
			c.Score = priceScore*100 + healthFactor*20 + weight
		case config.StrategyStability:
			stabilityScore := 1 - c.Health.ErrorRate
			c.Score = stabilityScore*100 + healthFactor*20 + weight
		case config.StrategyWeight:
			c.Score = weight * healthFactor
		default:
			// priority：先按渠道优先级，同级内按权重与健康度
			c.Score = float64(c.Channel.GetPriority())*1000 + weight*healthFactor
		}
	}
}

func minPositive(v int, max int) int {
	if v <= 0 {
		return max
	}
	if v > max {
		return max
	}
	return v
}

// SelectChannel 按当前策略挑选一个渠道；exclude 里的渠道会被跳过（用于 fallback 重试）
func SelectChannel(group string, modelName string, exclude map[int]bool) (*model.Channel, error) {
	strategy := Strategy()
	if strategy == config.StrategyPriority && len(exclude) == 0 {
		// 与原版行为完全一致，且能吃到内存缓存
		channel, err := model.CacheGetRandomSatisfiedChannel(group, modelName, false)
		if err == nil && channel != nil {
			return channel, nil
		}
	}
	candidates, err := Candidates(group, modelName, exclude)
	if err != nil {
		return nil, err
	}
	if strategy == config.StrategyWeight || strategy == config.StrategyPriority {
		// 权重 / 优先级同级内做加权随机，避免所有流量打到同一条线
		return weightedPick(candidates), nil
	}
	return candidates[0].Channel, nil
}

func weightedPick(candidates []Candidate) *model.Channel {
	if len(candidates) == 1 {
		return candidates[0].Channel
	}
	total := 0.0
	for _, c := range candidates {
		w := c.Score
		if w <= 0 {
			w = 0.01
		}
		total += w
	}
	if total <= 0 {
		return candidates[rand.Intn(len(candidates))].Channel
	}
	target := rand.Float64() * total
	acc := 0.0
	for _, c := range candidates {
		w := c.Score
		if w <= 0 {
			w = 0.01
		}
		acc += w
		if acc >= target {
			return c.Channel
		}
	}
	return candidates[len(candidates)-1].Channel
}

// FallbackModels 返回模型失败后可以尝试的备用模型
func FallbackModels(modelName string) []string {
	return config.GetFallbackModels(modelName)
}

// LogStrategy 启动时打印一次生效的路由策略
func LogStrategy() {
	logger.SysLog("routing strategy: " + Strategy())
}
