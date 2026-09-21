package routing

import (
	"testing"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/model"
)

func newChannel(id int, priority int64, weight uint) *model.Channel {
	return &model.Channel{
		Id:       id,
		Priority: &priority,
		Weight:   &weight,
		Name:     "ch",
	}
}

func newHealth(status string, latency int, errRate float64, factor float64) *model.ChannelHealth {
	return &model.ChannelHealth{
		Status:       status,
		LatencyMs:    latency,
		ErrorRate:    errRate,
		WeightFactor: factor,
	}
}

func TestStrategyFallback(t *testing.T) {
	config.RoutingStrategy = "unknown-strategy"
	if got := Strategy(); got != config.StrategyPriority {
		t.Fatalf("expected priority fallback, got %s", got)
	}
	config.RoutingStrategy = config.StrategyLatency
	if got := Strategy(); got != config.StrategyLatency {
		t.Fatalf("expected latency, got %s", got)
	}
	config.RoutingStrategy = config.StrategyPriority
}

func TestScoreLatencyPrefersFastChannel(t *testing.T) {
	config.RoutingStrategy = config.StrategyLatency
	candidates := []Candidate{
		{Channel: newChannel(1, 0, 1), Health: newHealth(config.HealthStatusHealthy, 2000, 0, 1), PricePerM: 1},
		{Channel: newChannel(2, 0, 1), Health: newHealth(config.HealthStatusHealthy, 200, 0, 1), PricePerM: 1},
	}
	score(candidates)
	if candidates[0].Score >= candidates[1].Score {
		t.Fatalf("fast channel should score higher: %f vs %f", candidates[0].Score, candidates[1].Score)
	}
}

func TestScoreCostPrefersCheapChannel(t *testing.T) {
	config.RoutingStrategy = config.StrategyCost
	candidates := []Candidate{
		{Channel: newChannel(1, 0, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0, 1), PricePerM: 30},
		{Channel: newChannel(2, 0, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0, 1), PricePerM: 0.5},
	}
	score(candidates)
	if candidates[0].Score >= candidates[1].Score {
		t.Fatalf("cheap channel should score higher: %f vs %f", candidates[0].Score, candidates[1].Score)
	}
}

func TestScoreStabilityPrefersLowErrorRate(t *testing.T) {
	config.RoutingStrategy = config.StrategyStability
	candidates := []Candidate{
		{Channel: newChannel(1, 0, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0.8, 0.2)},
		{Channel: newChannel(2, 0, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0.0, 1)},
	}
	score(candidates)
	if candidates[0].Score >= candidates[1].Score {
		t.Fatalf("stable channel should score higher: %f vs %f", candidates[0].Score, candidates[1].Score)
	}
}

func TestScorePriority(t *testing.T) {
	config.RoutingStrategy = config.StrategyPriority
	candidates := []Candidate{
		{Channel: newChannel(1, 5, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0, 1)},
		{Channel: newChannel(2, 50, 1), Health: newHealth(config.HealthStatusHealthy, 100, 0, 1)},
	}
	score(candidates)
	if candidates[1].Score <= candidates[0].Score {
		t.Fatalf("higher priority channel should score higher: %f vs %f", candidates[1].Score, candidates[0].Score)
	}
}

func TestWeightedPickSingleCandidate(t *testing.T) {
	candidates := []Candidate{{Channel: newChannel(7, 0, 1), Score: 1}}
	if got := weightedPick(candidates); got.Id != 7 {
		t.Fatalf("expected channel 7, got %d", got.Id)
	}
}
