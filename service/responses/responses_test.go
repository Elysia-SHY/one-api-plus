package responses

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertRequestStringInput(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","input":"帮我写个快排","instructions":"你是代码助手","max_output_tokens":1000}`)
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	out, err := ConvertRequest(&req)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	messages, ok := out["messages"].([]interface{})
	if !ok {
		t.Fatalf("messages missing")
	}
	// system + user 两条
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	system := messages[0].(map[string]interface{})
	if system["role"] != "system" || system["content"] != "你是代码助手" {
		t.Errorf("system message mismatch: %+v", system)
	}
	user := messages[1].(map[string]interface{})
	if user["role"] != "user" || user["content"] != "帮我写个快排" {
		t.Errorf("user message mismatch: %+v", user)
	}
	if out["max_tokens"] != 1000 {
		t.Errorf("max_tokens should be 1000, got %v", out["max_tokens"])
	}
}

func TestConvertRequestStructuredInput(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","input":[
		{"role":"developer","content":[{"type":"input_text","text":"SYS"}]},
		{"role":"user","content":[{"type":"input_text","text":"HI"},{"type":"input_image","image_url":"https://x/a.png"}]}
	]}`)
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	out, err := ConvertRequest(&req)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	messages := out["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	first := messages[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Errorf("developer role should map to system, got %v", first["role"])
	}
	second := messages[1].(map[string]interface{})
	content, ok := second["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("multimodal content should stay structured, got %T", second["content"])
	}
	foundImage := false
	for _, part := range content {
		if part["type"] == "image_url" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Errorf("image part lost during conversion: %+v", content)
	}
}

func TestConvertRequestTools(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","input":"hi","tools":[
		{"type":"function","name":"get_weather","description":"查天气","parameters":{"type":"object"}},
		{"type":"web_search"}
	],"tool_choice":"auto","parallel_tool_calls":false}`)
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	out, err := ConvertRequest(&req)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	tools, ok := out["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tools missing")
	}
	// web_search 这类托管工具被丢弃，只保留 function
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if out["tool_choice"] != "auto" {
		t.Errorf("tool_choice should pass through, got %v", out["tool_choice"])
	}
	if out["parallel_tool_calls"] != false {
		t.Errorf("parallel_tool_calls should be false, got %v", out["parallel_tool_calls"])
	}
}

func TestConvertRequestRejectsEmpty(t *testing.T) {
	var req Request
	req.Model = "gpt-4o"
	if _, err := ConvertRequest(&req); err == nil {
		t.Errorf("empty input should be rejected")
	}
	if _, err := ConvertRequest(nil); err == nil {
		t.Errorf("nil request should be rejected")
	}
}

func TestConvertResponse(t *testing.T) {
	chat := map[string]interface{}{
		"id":    "chatcmpl-abc",
		"model": "gpt-4o",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{"role": "assistant", "content": "你好"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
			"total_tokens":      float64(15),
		},
	}
	resp := ConvertResponse(chat, "gpt-4o")
	if !strings.HasPrefix(resp.ID, "resp_") {
		t.Errorf("response id should be prefixed, got %s", resp.ID)
	}
	if resp.Object != "response" || resp.Status != "completed" {
		t.Errorf("unexpected object/status: %s/%s", resp.Object, resp.Status)
	}
	if resp.OutputText != "你好" {
		t.Errorf("output_text mismatch: %q", resp.OutputText)
	}
	if len(resp.Output) != 1 || resp.Output[0].Type != "message" {
		t.Fatalf("unexpected output: %+v", resp.Output)
	}
	content := resp.Output[0].Content[0]
	if content["type"] != "output_text" || content["text"] != "你好" {
		t.Errorf("content mismatch: %+v", content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Errorf("usage mismatch: %+v", resp.Usage)
	}
}

func TestConvertResponseToolCalls(t *testing.T) {
	chat := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id": "call_1",
							"function": map[string]interface{}{
								"name":      "get_weather",
								"arguments": "{\"city\":\"合肥\"}",
							},
						},
					},
				},
			},
		},
	}
	resp := ConvertResponse(chat, "gpt-4o")
	found := false
	for _, item := range resp.Output {
		if item.Type == "function_call" {
			found = true
			if item.Name != "get_weather" {
				t.Errorf("tool name mismatch: %s", item.Name)
			}
			if item.Arguments != "{\"city\":\"合肥\"}" {
				t.Errorf("arguments mismatch: %s", item.Arguments)
			}
		}
	}
	if !found {
		t.Errorf("function_call item missing: %+v", resp.Output)
	}
}

func TestConvertStreamChunk(t *testing.T) {
	state := &StreamState{FallbackModel: "gpt-4o"}
	// 首个分片应当发出 created + in_progress
	state.Done = false
	events, _ := ConvertStreamChunk(state, `{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"delta":{"content":"你"}}]}`)
	if len(events) == 0 || events[0].Event != "response.created" {
		t.Fatalf("first chunk should emit response.created, got %+v", events)
	}
	foundDelta := false
	for _, e := range events {
		if e.Event == "response.output_text.delta" {
			foundDelta = true
		}
	}
	if !foundDelta {
		t.Errorf("expected output_text.delta event, got %+v", events)
	}
	// 继续追加
	events, _ = ConvertStreamChunk(state, `{"choices":[{"delta":{"content":"好"}}]}`)
	hasDelta := false
	for _, e := range events {
		if e.Event == "response.output_text.delta" {
			hasDelta = true
		}
	}
	if !hasDelta || state.Text != "你好" {
		t.Errorf("stream state accumulation failed: text=%q events=%+v", state.Text, events)
	}
	// 结束
	events, done := ConvertStreamChunk(state, "[DONE]")
	if !done {
		t.Errorf("DONE chunk should mark stream finished")
	}
	if len(events) != 1 || events[0].Event != "response.completed" {
		t.Fatalf("expect response.completed, got %+v", events)
	}
	built := state.BuildResponse()
	if built.Status != "completed" || built.OutputText != "你好" {
		t.Errorf("final response mismatch: %+v", built)
	}
}

func TestConvertStreamToolCall(t *testing.T) {
	state := &StreamState{}
	_, _ = ConvertStreamChunk(state, `{"id":"c1","model":"gpt-4o","choices":[{"delta":{"content":""}}]}`)
	_, _ = ConvertStreamChunk(state, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"search","arguments":"{\"q\""}}]}}]}`)
	_, _ = ConvertStreamChunk(state, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"go\"}"}}]}}]}`)
	if len(state.ToolCalls) != 1 {
		t.Fatalf("tool call not accumulated: %+v", state.ToolCalls)
	}
	if state.ToolCalls[0].Name != "search" || state.ToolCalls[0].Arguments != "{\"q\":\"go\"}" {
		t.Errorf("tool call accumulation mismatch: %+v", state.ToolCalls[0])
	}
	built := state.BuildResponse()
	foundCall := false
	for _, item := range built.Output {
		if item.Type == "function_call" && item.Name == "search" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Errorf("built response missing function_call: %+v", built.Output)
	}
}

func TestPreviousResponseRecall(t *testing.T) {
	Remember("resp_seed_1", "上一轮的答复")
	if got := Recall("resp_seed_1"); got != "上一轮的答复" {
		t.Errorf("recall failed, got %q", got)
	}
	// 不是直接的记得，但不存在的 id 必须返回空而不是报错
	if got := Recall("not-exist"); got != "" {
		t.Errorf("unknown id should return empty, got %q", got)
	}
}

func TestRandomSuffixStable(t *testing.T) {
	a := randomSuffix("same-seed")
	b := randomSuffix("same-seed")
	if a != b {
		t.Errorf("same seed should produce same suffix: %s vs %s", a, b)
	}
	if a == "" {
		t.Errorf("suffix should not be empty")
	}
	// nil seed 走随机分支，多次调用不应全部相同
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		seen[randomSuffix(nil)] = true
	}
	if len(seen) < 2 {
		t.Errorf("nil seed should produce varying suffixes")
	}
}
