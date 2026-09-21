package modelgroup

import (
	"reflect"
	"sort"
	"testing"
)

func TestClassifyModels(t *testing.T) {
	models := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"claude-sonnet-4-20250514",
		"deepseek-v3",
		"qwen2.5-vl-72b-instruct",
		"gemini-2.5-pro",
		"text-embedding-3-small",
		"bge-rerank-v2-m3",
		"some-unknown-model",
	}
	got := classifyModels(models)

	contains := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}

	// 视觉组必须包含 gpt-4o / qwen-vl / gemini，且不能包含 embedding
	for _, want := range []string{"gpt-4o", "qwen2.5-vl-72b-instruct", "gemini-2.5-pro"} {
		if !contains(got["vision"], want) {
			t.Errorf("vision group missing %q, got %v", want, got["vision"])
		}
	}
	if contains(got["vision"], "text-embedding-3-small") {
		t.Errorf("vision group should not contain embedding model, got %v", got["vision"])
	}

	// chat 兜底组应该拿到除了 embedding/rerank 之外的几乎所有模型
	if !contains(got["chat"], "some-unknown-model") {
		t.Errorf("chat group should contain unknown model as fallback, got %v", got["chat"])
	}
	if contains(got["chat"], "text-embedding-3-small") {
		t.Errorf("chat group should not contain embedding model, got %v", got["chat"])
	}
	if contains(got["chat"], "bge-rerank-v2-m3") {
		t.Errorf("chat group should not contain rerank model, got %v", got["chat"])
	}

	// embedding 组：按模型名字典序
	embedding := append([]string(nil), got["embedding"]...)
	sort.Strings(embedding)
	if !reflect.DeepEqual(embedding, []string{"bge-rerank-v2-m3", "text-embedding-3-small"}) {
		t.Errorf("embedding group = %v", embedding)
	}

	// cheap 组：mini 系在内，pro / embedding 系在外
	if !contains(got["cheap"], "gpt-4o-mini") {
		t.Errorf("cheap group missing gpt-4o-mini, got %v", got["cheap"])
	}
	if contains(got["cheap"], "gemini-2.5-pro") {
		t.Errorf("cheap group should not contain pro model, got %v", got["cheap"])
	}
	if contains(got["cheap"], "text-embedding-3-small") {
		t.Errorf("cheap group should not contain embedding model, got %v", got["cheap"])
	}

	// coding 组
	if !contains(got["coding"], "claude-sonnet-4-20250514") {
		t.Errorf("coding group missing claude, got %v", got["coding"])
	}

	// 每个模型最多进入 maxAutoGroupsPerModel 个专组
	counts := make(map[string]int)
	for name, list := range got {
		if name == "chat" {
			continue
		}
		for _, m := range list {
			counts[m]++
		}
	}
	for m, c := range counts {
		if c > maxAutoGroupsPerModel {
			t.Errorf("model %q entered %d groups, want <= %d", m, c, maxAutoGroupsPerModel)
		}
	}
}

func TestAutoMemberPriority(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-20250514", 50},
		{"gpt-5", 50},
		{"gemini-2.5-pro", 50},
		{"claude-sonnet-4-20250514", 40},
		{"gpt-4o", 40},
		{"qwen3-max", 50},
		{"glm-4.6", 40},
		{"qwen2.5-72b-instruct", 30},
		{"gpt-4o-mini", 10},
		{"gemini-2.0-flash", 10},
		{"claude-3-5-haiku-20241022", 10},
		{"some-random-model", 20},
	}
	for _, tc := range cases {
		if got := autoMemberPriority(tc.model); got != tc.want {
			t.Errorf("autoMemberPriority(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
}

func TestIsAutoGroup(t *testing.T) {
	if !IsAutoGroup("auto-chat") || !IsAutoGroup("AUTO-ALL") {
		t.Error("IsAutoGroup should accept auto- prefixed names case-insensitively")
	}
	if IsAutoGroup("my-coding") || IsAutoGroup("") {
		t.Error("IsAutoGroup should reject non auto- names")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("GPT-4O-Mini", "gpt-4o") {
		t.Error("containsAny should be case-insensitive")
	}
	if containsAny("gemini-pro", "claude", "grok") {
		t.Error("containsAny should return false when nothing matches")
	}
}
