// Package responses 提供 OpenAI Responses API ⇄ Chat Completions API 的双向转换。
//
// 为什么需要：Codex CLI、新版 SDK 与部分 Agent 框架已经迁到 /v1/responses，
// 而绝大多数上游渠道（含 OpenAI 自己的旧版兼容层、各种中转）只有 chat/completions。
// 网关负责把两边对齐，让上层客户端可以直接用 Responses 协议调用任意渠道。
//
// 转换要点：
//   - input 可以是纯字符串，也可以是 input_text / input_image 的结构化消息数组
//   - instructions 落到 messages 的 system 角色
//   - max_output_tokens → max_tokens（部分渠道认后者）
//   - 不支持的上游字段（previous_response_id / store / reasoning）在本地消化，
//     其中 previous_response_id 会从本地响应仓库里取回上一轮的 output 作为上下文
package responses

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

// Request 是 Responses API 的请求体（只取网关需要的字段）
type Request struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Instructions       string          `json:"instructions"`
	MaxOutputTokens    int             `json:"max_output_tokens"`
	Temperature        *float64        `json:"temperature"`
	TopP               *float64        `json:"top_p"`
	Stream             bool            `json:"stream"`
	Tools              []Tool          `json:"tools"`
	ToolChoice         interface{}     `json:"tool_choice"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls"`
	PreviousResponseID string          `json:"previous_response_id"`
	Store              *bool           `json:"store"`
	Reasoning          json.RawMessage `json:"reasoning"`
	Metadata           json.RawMessage `json:"metadata"`
	User               string          `json:"user"`
}

// Tool 是 Responses 形态的工具定义
type Tool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Strict      *bool                  `json:"strict"`
}

// ---------------------------------------------------------------------------
// 请求：Responses → Chat Completions
// ---------------------------------------------------------------------------

type chatMessage struct {
	Role         string        `json:"role"`
	Content      interface{}   `json:"content"`
	Name         string        `json:"name,omitempty"`
	ToolCalls    []interface{} `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	FunctionName string        `json:"-"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

// ConvertRequest 把 Responses 请求转成 Chat Completions 请求
func ConvertRequest(req *Request) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("nil responses request")
	}
	out := map[string]interface{}{
		"model": req.Model,
	}
	messages := make([]interface{}, 0, 8)
	if strings.TrimSpace(req.Instructions) != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": req.Instructions})
	}
	// previous_response_id：把上一轮的输出取回来当上下文，弥补上游没有该语义
	if req.PreviousResponseID != "" {
		if recalled := Recall(req.PreviousResponseID); recalled != "" {
			messages = append(messages, map[string]interface{}{"role": "assistant", "content": recalled})
		}
	}
	parsed, err := convertInput(req.Input)
	if err != nil {
		return nil, err
	}
	messages = append(messages, parsed...)
	if len(messages) == 0 {
		return nil, fmt.Errorf("responses request has no usable input")
	}
	out["messages"] = messages
	if req.MaxOutputTokens > 0 {
		out["max_tokens"] = req.MaxOutputTokens
		out["max_completion_tokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if req.Stream {
		out["stream"] = true
	}
	if req.User != "" {
		out["user"] = req.User
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			if t.Type != "" && t.Type != "function" {
				// web_search 等托管工具透传不了，直接丢弃并记录
				logger.SysLog(fmt.Sprintf("responses: skipped unsupported tool type %s", t.Type))
				continue
			}
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			})
		}
		if len(tools) > 0 {
			out["tools"] = tools
			if req.ToolChoice != nil {
				out["tool_choice"] = normalizeToolChoice(req.ToolChoice)
			}
			if req.ParallelToolCalls != nil {
				out["parallel_tool_calls"] = *req.ParallelToolCalls
			}
		}
	}
	if req.Reasoning != nil && len(req.Reasoning) > 2 {
		// 把 reasoning effort 折成 thinking-style 参数，供部分国产推理模型识别
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if err := json.Unmarshal(req.Reasoning, &reasoning); err == nil && reasoning.Effort != "" {
			out["reasoning_effort"] = reasoning.Effort
		}
	}
	return out, nil
}

// convertInput 处理 input：字符串或结构化消息数组
func convertInput(raw json.RawMessage) ([]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	// 形态一：纯字符串
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []interface{}{map[string]interface{}{"role": "user", "content": asString}}, nil
	}
	// 形态二：消息数组
	var items []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Name    string          `json:"name"`
		Type    string          `json:"type"`
		Status  string          `json:"status"`
		// function_call / function_call_output 形态
		CallID   string `json:"call_id"`
		ToolType string `json:"tool_type"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		// 形态三：单条消息对象
		var single struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return nil, fmt.Errorf("unsupported responses input: %w", err)
		}
		items = []struct {
			Role     string          `json:"role"`
			Content  json.RawMessage `json:"content"`
			Name     string          `json:"name"`
			Type     string          `json:"type"`
			Status   string          `json:"status"`
			CallID   string          `json:"call_id"`
			ToolType string          `json:"tool_type"`
		}{{Role: single.Role, Content: single.Content}}
	}
	messages := make([]interface{}, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		switch role {
		case "":
			role = "user"
		case "developer":
			role = "system"
		}
		switch {
		case item.ToolType == "function_call_output" || (role == "tool" && item.CallID != ""):
			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": item.CallID,
				"content":      stringifyContent(item.Content),
			})
		case item.Type == "function_call" || item.ToolType == "function_call":
			messages = append(messages, map[string]interface{}{
				"role":         "assistant",
				"content":      "",
				"tool_call_id": item.CallID,
			})
		default:
			content := normalizeContent(item.Content)
			if content == nil {
				continue
			}
			message := map[string]interface{}{"role": role, "content": content}
			if item.Name != "" {
				message["name"] = item.Name
			}
			messages = append(messages, message)
		}
	}
	return messages, nil
}

// normalizeContent 处理 content：字符串、或 input_text / input_image / text 的内容块数组
//
// 返回值可能是 string（纯文本，兼容性最好）或 []map（多模态，保留 image_url / data URI）
func normalizeContent(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if strings.TrimSpace(asString) == "" {
			return nil
		}
		return asString
	}
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		Source   struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	parts := make([]map[string]interface{}, 0, len(blocks))
	textOnly := true
	for _, b := range blocks {
		switch b.Type {
		case "input_text", "output_text", "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			parts = append(parts, map[string]interface{}{"type": "text", "text": b.Text})
		case "input_image":
			textOnly = false
			url := b.ImageURL
			if url == "" {
				if b.Source.Data != "" {
					media := b.Source.MediaType
					if media == "" {
						media = "image/png"
					}
					url = fmt.Sprintf("data:%s;base64,%s", media, b.Source.Data)
				} else if b.Source.Type == "url" {
					continue
				}
			}
			if url == "" {
				continue
			}
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": url,
				},
			})
		default:
			continue
		}
	}
	if len(parts) == 0 {
		return nil
	}
	if textOnly {
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			if v, ok := p["text"].(string); ok {
				texts = append(texts, v)
			}
		}
		return strings.Join(texts, "\n")
	}
	return parts
}

func stringifyContent(raw json.RawMessage) string {
	switch v := normalizeContent(raw).(type) {
	case string:
		return v
	default:
		out, _ := json.Marshal(raw)
		return string(out)
	}
}

func normalizeToolChoice(choice interface{}) interface{} {
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto", "none", "required":
			return v
		default:
			// Responses 里可以是 "file_search" 之类的托管工具名
			return "auto"
		}
	case map[string]interface{}:
		if name, ok := v["name"].(string); ok {
			return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}
		}
		return "auto"
	default:
		return "auto"
	}
}

// ---------------------------------------------------------------------------
// 响应：Chat Completions → Responses
// ---------------------------------------------------------------------------

// Response 是 Responses API 的响应体
type Response struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	CreatedAt         int64              `json:"created_at"`
	Model             string             `json:"model"`
	Status            string             `json:"status"`
	Output            []OutputItem       `json:"output"`
	OutputText        string             `json:"output_text"`
	Usage             *Usage             `json:"usage,omitempty"`
	Error             *ErrorObject       `json:"error,omitempty"`
	IncompleteDetails *IncompleteDetails `json:"incomplete_details,omitempty"`
	ParallelToolCalls bool               `json:"parallel_tool_calls"`
	ToolChoice        string             `json:"tool_choice"`
}

// OutputItem 是 output 数组里的一项（message 或 function_call）
type OutputItem struct {
	ID        string           `json:"id"`
	Type      string           `json:"type"`
	Role      string           `json:"role,omitempty"`
	Status    string           `json:"status,omitempty"`
	Content   []map[string]any `json:"content,omitempty"`
	CallID    string           `json:"call_id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Arguments string           `json:"arguments,omitempty"`
}

// Usage 是 Responses 的用量统计
type Usage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
}

// ErrorObject 是 Responses 的错误形态
type ErrorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// IncompleteDetails 描述未完成的原因
type IncompleteDetails struct {
	Reason string `json:"reason"`
}

// ConvertResponse 把 chat completion 响应转成 Responses 响应
func ConvertResponse(chatResp map[string]interface{}, model string) *Response {
	now := time.Now().Unix()
	id := "resp_" + randomSuffix(chatResp["id"])
	respModel := stringField(chatResp["model"])
	if respModel == "" {
		respModel = model
	}
	out := &Response{
		ID:                id,
		Object:            "response",
		CreatedAt:         now,
		Model:             respModel,
		Status:            "completed",
		Output:            make([]OutputItem, 0, 2),
		ParallelToolCalls: true,
		ToolChoice:        "auto",
	}
	choices, _ := chatResp["choices"].([]interface{})
	texts := make([]string, 0, 1)
	toolCalls := make([]map[string]interface{}, 0)
	for _, raw := range choices {
		choice, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		message, ok := choice["message"].(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := message["content"].(string); ok && content != "" {
			texts = append(texts, content)
		}
		if calls, ok := message["tool_calls"].([]interface{}); ok {
			for _, callRaw := range calls {
				if call, ok := callRaw.(map[string]interface{}); ok {
					toolCalls = append(toolCalls, call)
				}
			}
		}
		if finish := stringField(choice["finish_reason"]); finish == "length" {
			out.Status = "incomplete"
			out.IncompleteDetails = &IncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	if len(texts) > 0 {
		joined := strings.Join(texts, "")
		out.Output = append(out.Output, OutputItem{
			ID:     "msg_" + randomSuffix(nil),
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []map[string]any{{
				"type":        "output_text",
				"text":        joined,
				"annotations": []interface{}{},
			}},
		})
		out.OutputText = joined
	}
	for i, call := range toolCalls {
		fn, _ := call["function"].(map[string]interface{})
		name, args := "", ""
		if fn != nil {
			name = stringField(fn["name"])
			args = stringField(fn["arguments"])
		}
		callID := stringField(call["id"])
		if callID == "" {
			callID = fmt.Sprintf("call_%s_%d", id, i)
		}
		out.Output = append(out.Output, OutputItem{
			ID:        callID,
			Type:      "function_call",
			CallID:    callID,
			Name:      name,
			Arguments: args,
			Status:    "completed",
		})
	}
	if usage, ok := chatResp["usage"].(map[string]interface{}); ok {
		u := &Usage{
			InputTokens:  intField(usage["prompt_tokens"]),
			OutputTokens: intField(usage["completion_tokens"]),
			TotalTokens:  intField(usage["total_tokens"]),
		}
		if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
			u.InputTokensDetails.CachedTokens = intField(details["cached_tokens"])
		}
		if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
			u.OutputTokensDetails.ReasoningTokens = intField(details["reasoning_tokens"])
		}
		out.Usage = u
	}
	return out
}

func stringField(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intField(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// 流式：Chat SSE chunk → Responses SSE events
// ---------------------------------------------------------------------------

// StreamEvent 是一个 SSE 事件
type StreamEvent struct {
	Event string
	Data  interface{}
}

// ConvertStreamChunk 把 chat 流式分片转成若干 Responses 流式事件
//
// 返回值：events + 该分片是否结束（收到 finish_reason）
func ConvertStreamChunk(state *StreamState, rawData string) ([]StreamEvent, bool) {
	events := make([]StreamEvent, 0, 2)
	data := strings.TrimSpace(rawData)
	if data == "" {
		return events, false
	}
	if data == "[DONE]" {
		events = append(events, StreamEvent{Event: "response.completed", Data: state.BuildResponse()})
		state.Done = true
		return events, true
	}
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Content   string          `json:"content"`
				ToolCalls []ToolCallDelta `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]interface{} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return events, false
	}
	if chunk.ID != "" && !state.Started {
		state.ID = "resp_" + randomSuffix(chunk.ID)
		state.Model = chunk.Model
		if state.Model == "" {
			state.Model = state.FallbackModel
		}
		state.Started = true
		state.ItemID = "msg_" + randomSuffix(nil)
		events = append(events, StreamEvent{Event: "response.created", Data: state.BuildResponse()})
		events = append(events, StreamEvent{Event: "response.in_progress", Data: state.BuildResponse()})
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			state.Text += choice.Delta.Content
			events = append(events, StreamEvent{Event: "response.output_text.delta", Data: map[string]interface{}{
				"type":         "response.output_text.delta",
				"item_id":      state.ItemID,
				"output_index": 0,
				"delta":        choice.Delta.Content,
			}})
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Index >= len(state.ToolCalls) {
				for i := len(state.ToolCalls); i <= call.Index; i++ {
					state.ToolCalls = append(state.ToolCalls, &ToolCallState{Index: i})
				}
			}
			tc := state.ToolCalls[call.Index]
			if call.ID != "" {
				tc.ID = call.ID
			}
			if call.Function.Name != "" {
				tc.Name = call.Function.Name
			}
			tc.Arguments += call.Function.Arguments
			events = append(events, StreamEvent{Event: "response.function_call_arguments.delta", Data: map[string]interface{}{
				"type":         "response.function_call_arguments.delta",
				"item_id":      call.ID,
				"output_index": call.Index + 1,
				"delta":        call.Function.Arguments,
			}})
		}
		if choice.FinishReason != "" {
			state.FinishReason = choice.FinishReason
		}
	}
	if chunk.Usage != nil {
		state.Usage = chunk.Usage
	}
	return events, false
}

// ToolCallDelta 是 chat 流式里的工具调用分片
type ToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolCallState 累计流式工具调用
type ToolCallState struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// StreamState 维护一次流式响应的累计状态
type StreamState struct {
	ID            string
	Model         string
	FallbackModel string
	ItemID        string
	Text          string
	ToolCalls     []*ToolCallState
	Usage         map[string]interface{}
	FinishReason  string
	Started       bool
	Done          bool
	CreatedAt     int64
}

// BuildResponse 产出当前累计状态的完整 response 对象
func (s *StreamState) BuildResponse() *Response {
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().Unix()
	}
	id := s.ID
	if id == "" {
		id = "resp_" + randomSuffix(nil)
	}
	model := s.Model
	if model == "" {
		model = s.FallbackModel
	}
	out := &Response{
		ID:                id,
		Object:            "response",
		CreatedAt:         s.CreatedAt,
		Model:             model,
		Status:            "in_progress",
		Output:            make([]OutputItem, 0, 2),
		ParallelToolCalls: true,
		ToolChoice:        "auto",
	}
	if s.Done {
		out.Status = "completed"
	}
	if s.Text != "" {
		out.Output = append(out.Output, OutputItem{
			ID:     s.ItemID,
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []map[string]any{{
				"type":        "output_text",
				"text":        s.Text,
				"annotations": []interface{}{},
			}},
		})
		out.OutputText = s.Text
	}
	for i, tc := range s.ToolCalls {
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%s_%d", id, i)
		}
		out.Output = append(out.Output, OutputItem{
			ID:        callID,
			Type:      "function_call",
			CallID:    callID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
			Status:    "completed",
		})
	}
	if s.Done && s.Usage != nil {
		u := &Usage{
			InputTokens:  intField(s.Usage["prompt_tokens"]),
			OutputTokens: intField(s.Usage["completion_tokens"]),
			TotalTokens:  intField(s.Usage["total_tokens"]),
		}
		out.Usage = u
	}
	if s.FinishReason == "length" {
		out.Status = "incomplete"
		out.IncompleteDetails = &IncompleteDetails{Reason: "max_output_tokens"}
	}
	return out
}

// Enabled 判断是否启用了 Responses 兼容层
func Enabled() bool {
	return config.ResponsesAPIEnabled
}
