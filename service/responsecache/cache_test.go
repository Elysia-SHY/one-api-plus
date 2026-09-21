package responsecache

import (
	"encoding/json"
	"testing"

	"github.com/Elysia-SHY/one-api-plus/common/config"
)

func TestBuildKeySkipsStream(t *testing.T) {
	config.ResponseCacheEnabled = true
	defer func() { config.ResponseCacheEnabled = false }()
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o", "stream": true})
	if key := BuildKey(1, "/v1/chat/completions", body); key != "" {
		t.Fatalf("stream request should not be cached, got %s", key)
	}
}

func TestBuildKeyDeterministic(t *testing.T) {
	config.ResponseCacheEnabled = true
	defer func() { config.ResponseCacheEnabled = false }()
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o", "temperature": 0})
	k1 := BuildKey(1, "/v1/chat/completions", body)
	k2 := BuildKey(1, "/v1/chat/completions", body)
	if k1 == "" || k1 != k2 {
		t.Fatalf("cache key should be deterministic, got %q and %q", k1, k2)
	}
	k3 := BuildKey(2, "/v1/chat/completions", body)
	if k1 == k3 {
		t.Fatal("different users must not share a cache key")
	}
}

func TestShouldCacheResponse(t *testing.T) {
	if ShouldCacheResponse(429, "{}") {
		t.Fatal("429 must not be cached")
	}
	if ShouldCacheResponse(200, "") {
		t.Fatal("empty body must not be cached")
	}
	if ShouldCacheResponse(200, `{"error":{"message":"x"}}`) {
		t.Fatal("error payload must not be cached")
	}
	if !ShouldCacheResponse(200, `{"choices":[]}`) {
		t.Fatal("normal response should be cached")
	}
}
