package routing

import (
	"testing"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/model"
)

func TestLoadScoreMonotonic(t *testing.T) {
	// 同样候选集里，负载越高的渠道得分应越低
	const maxLoad = int64(10)
	const soft = 32
	low := loadScore(0, maxLoad, soft)
	mid := loadScore(5, maxLoad, soft)
	high := loadScore(10, maxLoad, soft)
	if !(low > mid && mid > high) {
		t.Errorf("load score should decrease with load: low=%f mid=%f high=%f", low, mid, high)
	}
	if low != 1 {
		t.Errorf("idle channel should score 1, got %f", low)
	}
}

func TestLoadPenalty(t *testing.T) {
	if got := loadPenalty(0, 32); got != 1 {
		t.Errorf("no load should have no penalty, got %f", got)
	}
	// 软上限以内的一半以下不受惩罚
	if got := loadPenalty(16, 32); got != 1 {
		t.Errorf("half soft load should not be penalized, got %f", got)
	}
	heavy := loadPenalty(64, 32)
	if heavy != 0.2 {
		t.Errorf("doubled soft load should be capped at 0.2, got %f", heavy)
	}
	// 惩罚随负载单调下降
	prev := 1.0
	for load := int64(17); load <= 60; load += 8 {
		cur := loadPenalty(load, 32)
		if cur > prev {
			t.Errorf("penalty should be monotonic decreasing at load %d", load)
		}
		prev = cur
	}
}

func TestScoreBalancedStrategy(t *testing.T) {
	config.RoutingStrategy = config.StrategyBalanced
	config.RoutingLoadAware = true
	if err := config.SetRoutingWeights(`{"latency":0.4,"cost":0.2,"stability":0.3,"load":0.1}`); err != nil {
		t.Fatalf("failed to set weights: %v", err)
	}
	defer func() {
		config.RoutingStrategy = config.StrategyPriority
		_ = config.SetRoutingWeights("")
	}()
	candidates := []Candidate{
		{
			Channel: &model.Channel{Id: 1, Name: "fast"},
			Health:  &model.ChannelHealth{ChannelId: 1, Status: config.HealthStatusHealthy, LatencyMs: 100, WeightFactor: 1},
			PricePerM: 5,
			Load:      0,
		},
		{
			Channel: &model.Channel{Id: 2, Name: "slow"},
			Health:  &model.ChannelHealth{ChannelId: 2, Status: config.HealthStatusDegraded, LatencyMs: 3000, ErrorRate: 0.4, WeightFactor: 0.5},
			PricePerM: 1,
			Load:      8,
		},
	}
	score(candidates)
	if candidates[0].Detail.Latency <= candidates[1].Detail.Latency {
		t.Errorf("faster channel should have higher latency score")
	}
	if candidates[1].Detail.Cost <= candidates[0].Detail.Cost {
		t.Errorf("cheaper channel should have higher cost score")
	}
	if candidates[0].Detail.Stability <= candidates[1].Detail.Stability {
		t.Errorf("healthier channel should have higher stability score")
	}
	if candidates[0].Detail.Load <= candidates[1].Detail.Load {
		t.Errorf("idle channel should have higher load score")
	}
	if candidates[0].Score <= candidates[1].Score {
		t.Errorf("under balanced strategy the fast healthy idle channel should win: %f vs %f",
			candidates[0].Score, candidates[1].Score)
	}
}

func TestScorePrioritisesHealthyChannel(t *testing.T) {
	config.RoutingStrategy = config.StrategyStability
	config.RoutingLoadAware = false
	defer func() { config.RoutingStrategy = config.StrategyPriority }()
	candidates := []Candidate{
		{
			Channel: &model.Channel{Id: 1},
			Health:  &model.ChannelHealth{Status: config.HealthStatusHealthy, ErrorRate: 0.05, LatencyMs: 200, WeightFactor: 1},
		},
		{
			Channel: &model.Channel{Id: 2},
			Health:  &model.ChannelHealth{Status: config.HealthStatusDegraded, ErrorRate: 0.6, LatencyMs: 200, WeightFactor: 0.5},
		},
	}
	score(candidates)
	if candidates[0].Score <= candidates[1].Score {
		t.Errorf("low error rate channel should score higher: %f vs %f", candidates[0].Score, candidates[1].Score)
	}
}

func TestScoreKeepsDeadChannelUsable(t *testing.T) {
	config.RoutingStrategy = config.StrategyLatency
	defer func() { config.RoutingStrategy = config.StrategyPriority }()
	candidates := []Candidate{
		{
			Channel: &model.Channel{Id: 1},
			Health:  &model.ChannelHealth{Status: config.HealthStatusDead, LatencyMs: 20000, WeightFactor: 0, ErrorRate: 1},
		},
	}
	score(candidates)
	// 即使被判死也要留一线机会：分数大于 0，且不会因为健康因子为 0 而变成 NaN/负
	if candidates[0].Score < 0 {
		t.Errorf("score should never go negative, got %f", candidates[0].Score)
	}
}

func TestLoadMeterLifecycle(t *testing.T) {
	id := 99181
	if got := GetLoad(id); got != 0 {
		t.Errorf("unknown channel should have zero load, got %d", got)
	}
	EnterLoad(id)
	EnterLoad(id)
	if got := GetLoad(id); got != 2 {
		t.Errorf("expected load 2, got %d", got)
	}
	if got := Snapshot()[id]; got != 2 {
		t.Errorf("snapshot mismatch: %d", got)
	}
	if got := Total(); got < 2 {
		t.Errorf("total load should at least include ours, got %d", got)
	}
	LeaveLoad(id)
	LeaveLoad(id)
	if got := GetLoad(id); got != 0 {
		t.Errorf("load should return to zero, got %d", got)
	}
	// 多减不应变成负数
	LeaveLoad(id)
	if got := GetLoad(id); got != 0 {
		t.Errorf("over-release should clamp at zero, got %d", got)
	}
}

func TestClamp01(t *testing.T) {
	cases := map[float64]float64{-1: 0, 0: 0, 0.5: 0.5, 1: 1, 2: 1}
	for in, want := range cases {
		if got := clamp01(in); got != want {
			t.Errorf("clamp01(%f) = %f, want %f", in, got, want)
		}
	}
}
