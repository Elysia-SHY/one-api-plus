package capability

import (
	"strings"
	"testing"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/model"
)

func TestInferKnownModel(t *testing.T) {
	cases := []struct {
		name       string
		wantCtx    int
		wantVision int
		wantTool   int
		wantReason int
		wantEmbed  int
	}{
		{"gpt-4o", 128000, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},
		{"claude-3-5-sonnet-20241022", 200000, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},
		{"deepseek-reasoner", 65536, model.CapUnsupported, model.CapSupported, model.CapSupported, model.CapUnsupported},
		{"text-embedding-3-large", 8191, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported},
		{"gemini-2.5-flash-preview", 1048576, model.CapSupported, model.CapSupported, model.CapSupported, model.CapUnsupported},
	}
	for _, c := range cases {
		got := Infer(c.name)
		if got == nil {
			t.Fatalf("Infer(%s) returned nil", c.name)
		}
		if got.ContextLength != c.wantCtx {
			t.Errorf("Infer(%s).ContextLength = %d, want %d", c.name, got.ContextLength, c.wantCtx)
		}
		if got.Vision != c.wantVision {
			t.Errorf("Infer(%s).Vision = %d, want %d", c.name, got.Vision, c.wantVision)
		}
		if got.ToolCall != c.wantTool {
			t.Errorf("Infer(%s).ToolCall = %d, want %d", c.name, got.ToolCall, c.wantTool)
		}
		if got.Reasoning != c.wantReason {
			t.Errorf("Infer(%s).Reasoning = %d, want %d", c.name, got.Reasoning, c.wantReason)
		}
		if got.Embedding != c.wantEmbed {
			t.Errorf("Infer(%s).Embedding = %d, want %d", c.name, got.Embedding, c.wantEmbed)
		}
	}
}

func TestInferByRule(t *testing.T) {
	// 视觉 + 上下文
	got := Infer("some-vendor/Qwen2-VL-72k-preview")
	if got.Vision != model.CapSupported {
		t.Errorf("vision keyword should be detected, got %d", got.Vision)
	}
	if got.ContextLength != 72000 {
		t.Errorf("context from suffix: got %d, want 72000", got.ContextLength)
	}
	// 嵌入模型不应该被判成支持工具调用
	got = Infer("bge-large-zh-v1.5")
	if got.Embedding != model.CapSupported {
		t.Errorf("embedding model should support embedding, got %d", got.Embedding)
	}
	if got.ToolCall != model.CapUnsupported {
		t.Errorf("embedding model should not support tool call, got %d", got.ToolCall)
	}
	// 推理模型
	got = Infer("QwQ-32B-Preview")
	if got.Reasoning != model.CapSupported {
		t.Errorf("reasoning model should be detected, got %d", got.Reasoning)
	}
	// 图像生成
	got = Infer("stable-diffusion-xl")
	if strings.Index(got.OutputTypes, "image") < 0 {
		t.Errorf("image model output types should include image, got %s", got.OutputTypes)
	}
}

func TestInferEmptyAndTagged(t *testing.T) {
	if got := Infer(""); got != nil {
		t.Errorf("empty name should return nil")
	}
	// :latest 后缀要能命中底座；且 keep 原样记录模型名
	got := Infer("gpt-4o-mini:latest")
	if got.Source != "seed" {
		t.Errorf("variant lookup should hit seed, got source %s", got.Source)
	}
	if got.ModelName != "gpt-4o-mini:latest" {
		t.Errorf("model name should be kept verbatim, got %s", got.ModelName)
	}
}

func TestGuessContext(t *testing.T) {
	cases := map[string]int{
		"model-32k": 32000,
		"model-128k": 128000,
		"model-1m": 1000000,
		"model": 0,
	}
	for in, want := range cases {
		if got := guessContext(in, 0); got != want {
			t.Errorf("guessContext(%s,0) = %d, want %d", in, got, want)
		}
	}
	// 已有值时优先使用已有值
	if got := guessContext("model-1m", 4096); got != 4096 {
		t.Errorf("fallback should take precedence, got %d", got)
	}
}

func TestFilterByCapability(t *testing.T) {
	names := []string{"gpt-4o", "deepseek-chat", "text-embedding-3-large"}
	// 只要支持工具调用的，应当排除纯嵌入模型
	got := Filter(names, 0, false, true, false)
	for _, n := range got {
		if n == "text-embedding-3-large" {
			t.Errorf("embedding model should be filtered out when tool_call required")
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(got))
	}
	// 上下文门槛：32k 以上应排除 16k 的 gpt-3.5
	got = Filter([]string{"gpt-3.5-turbo", "gpt-4o"}, 32768, false, false, false)
	if len(got) != 1 || got[0] != "gpt-4o" {
		t.Errorf("context filter failed: %v", got)
	}
}

func TestGetWithoutDB(t *testing.T) {
	config.CapabilityEnabled = true
	// model.DB 为 nil 时应当退化为纯规则推断，而不是 panic
	got := Get("gpt-4o")
	if got == nil {
		t.Fatalf("Get should fall back to inference when DB is unavailable")
	}
	if got.ContextLength != 128000 {
		t.Errorf("expected gpt-4o context 128000, got %d", got.ContextLength)
	}
	if Infer("unknown-model-xyz") == nil {
		t.Errorf("unknown model should still get a rule-inferred profile")
	}
}
