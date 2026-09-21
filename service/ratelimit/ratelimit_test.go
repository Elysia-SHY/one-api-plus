package ratelimit

import (
	"testing"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
)

func TestAllowWhenDisabled(t *testing.T) {
	config.RateLimitEnabled = false
	if res := Allow("user:1", "gpt-4o"); !res.Allowed {
		t.Errorf("rate limit disabled should always allow")
	}
}

func TestTokenBucketBurst(t *testing.T) {
	config.RateLimitEnabled = true
	config.RateLimitQPM = 5
	config.RateLimitBurst = 2
	config.RateLimitPerModel = false
	defer func() {
		config.RateLimitEnabled = false
		ClearCounters()
	}()
	ClearCounters()
	key := "test:burst"
	allowed := 0
	// 上限 5 + 突发 2，最多放行 7 次
	for i := 0; i < 20; i++ {
		if Allow(key, "").Allowed {
			allowed++
		}
	}
	if allowed == 0 {
		t.Fatalf("at least some requests should be allowed")
	}
	if allowed > 7 {
		t.Errorf("burst should cap permits at qpm+burst=7, got %d", allowed)
	}
	if Allow(key, "").Allowed {
		t.Errorf("bucket should be exhausted after burst")
	}
	// 被拒绝时应给出退避秒数，方便客户端重试
	res := Allow(key, "")
	if res.RetryAfter <= 0 {
		t.Errorf("rejected request should carry Retry-After > 0, got %d", res.RetryAfter)
	}
	if res.Remaining != 0 {
		t.Errorf("rejected request should report 0 remaining, got %d", res.Remaining)
	}
}

func TestPerModelBucket(t *testing.T) {
	config.RateLimitEnabled = true
	config.RateLimitQPM = 3
	config.RateLimitBurst = 0
	config.RateLimitPerModel = true
	defer func() {
		config.RateLimitEnabled = false
		config.RateLimitPerModel = false
		ClearCounters()
	}()
	ClearCounters()
	// 按模型分桶时，不同模型各自独立计数
	for i := 0; i < 5; i++ {
		Allow("user:pm", "model-a")
	}
	res := Allow("user:pm", "model-b")
	if !res.Allowed {
		t.Errorf("separate model should have its own bucket")
	}
}

func TestConcurrencyGate(t *testing.T) {
	ClearCounters()
	key := "conc:test"
	if !AcquireConcurrency(key, 2) {
		t.Fatalf("first acquire should succeed")
	}
	if !AcquireConcurrency(key, 2) {
		t.Fatalf("second acquire should succeed")
	}
	if AcquireConcurrency(key, 2) {
		t.Errorf("third acquire should be rejected at limit 2")
	}
	if got := ConcurrencyOf(key); got != 2 {
		t.Errorf("concurrency should be 2, got %d", got)
	}
	ReleaseConcurrency(key)
	if AcquireConcurrency(key, 2) == false {
		t.Errorf("should be able to acquire after release")
	}
	ReleaseConcurrency(key)
	ReleaseConcurrency(key)
	if got := ConcurrencyOf(key); got != 0 {
		t.Errorf("over-release should clamp at zero, got %d", got)
	}
}

func TestConcurrencyUnlimited(t *testing.T) {
	ClearCounters()
	for i := 0; i < 100; i++ {
		if !AcquireConcurrency("conc:free", 0) {
			t.Fatalf("limit 0 means unlimited")
		}
	}
}

func TestCleanupMemory(t *testing.T) {
	ClearCounters()
	consumeTokenMemory("cleanup:key", 10, 100)
	if len(memStore) == 0 {
		t.Errorf("counter should exist before cleanup")
	}
	CleanupMemory()
	if len(memStore) != 1 {
		t.Errorf("current window counter must survive cleanup, left %d", len(memStore))
	}
	// 把窗口拨回到两分钟前，模拟过期计数
	memMu.Lock()
	now := time.Now().Unix() / 60
	for _, v := range memStore {
		v.window = now - 5
	}
	memMu.Unlock()
	CleanupMemory()
	if len(memStore) != 0 {
		t.Errorf("expired counters should be dropped, left %d", len(memStore))
	}
}
