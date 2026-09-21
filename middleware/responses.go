package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/service/responses"
)

// ResponsesAdapter 把 /v1/responses 请求适配到内部的 chat/completions 链路上。
//
// 前置：把 Responses 请求体翻译成 chat 请求体，并把 URL 改写成 /v1/chat/completions，
//
//	后面的 TokenAuth / Distribute / Controller 完全不用感知协议差异。
//
// 后置：用一个替换过的 ResponseWriter 接住上游返回，再翻译回 Responses 形态
//
//	—— 非流式直接转换 JSON，流式则逐条把 SSE chunk 转成 Responses 事件。
func ResponsesAdapter() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !responses.Enabled() {
			return
		}
		ctx := c.Request.Context()
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			abortWithResponsesError(c, http.StatusBadRequest, "invalid_request", "failed to read request body")
			return
		}
		_ = c.Request.Body.Close()
		if len(bytes.TrimSpace(body)) == 0 {
			abortWithResponsesError(c, http.StatusBadRequest, "invalid_request", "empty request body")
			return
		}
		var req responses.Request
		if err = json.Unmarshal(body, &req); err != nil {
			abortWithResponsesError(c, http.StatusBadRequest, "invalid_request", "invalid responses request: "+err.Error())
			return
		}
		if req.Model == "" {
			abortWithResponsesError(c, http.StatusBadRequest, "invalid_request", "model is required")
			return
		}
		chatBody, err := responses.ConvertRequest(&req)
		if err != nil {
			abortWithResponsesError(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		encoded, err := json.Marshal(chatBody)
		if err != nil {
			abortWithResponsesError(c, http.StatusInternalServerError, "server_error", "failed to encode request")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(encoded))
		c.Request.ContentLength = int64(len(encoded))
		c.Request.Header.Set("Content-Length", fmt.Sprintf("%d", len(encoded)))
		c.Request.URL.Path = "/v1/chat/completions"
		c.Request.Header.Del("Accept-Encoding")

		if req.Stream {
			c.Writer = newResponsesSSEWriter(c, req.Model)
			c.Next()
			return
		}
		// 非流式：必须显式收尾。
		//
		// gin 在 handler 链结束后只会对它自己持有的原始 writer 调用 WriteHeaderNow()，
		// 替换后的 c.Writer 不会被自动 flush——如果这里不收尾，改写结果就永远发不出去。
		writer := newResponsesJSONWriter(c, req.Model)
		c.Writer = writer
		defer writer.finish()
		logger.Debugf(ctx, "responses request adapted: model=%s stream=false", req.Model)
		c.Next()
	}
}

// ---------------------------------------------------------------------------
// 非流式响应改写
// ---------------------------------------------------------------------------

type responsesJSONWriter struct {
	gin.ResponseWriter
	captured    *bytes.Buffer
	model       string
	once        sync.Once
	passthrough bool // 遇到非 JSON 响应后转为透传模式
}

func newResponsesJSONWriter(c *gin.Context, model string) *responsesJSONWriter {
	return &responsesJSONWriter{
		ResponseWriter: c.Writer,
		captured:       &bytes.Buffer{},
		model:          model,
	}
}

func (w *responsesJSONWriter) Write(b []byte) (int, error) {
	contentType := w.Header().Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "json") {
		// 非 JSON 响应（例如图片、音频）没有转换意义，直接透过去
		w.passthrough = true
		return w.ResponseWriter.Write(b)
	}
	w.captured.Write(b)
	return len(b), nil
}

func (w *responsesJSONWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *responsesJSONWriter) Flush() {
	w.ResponseWriter.Flush()
}

func (w *responsesJSONWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

// finish 把捕获到的 chat 响应翻译成 Responses 形态并发出。
// 必须幂等：gin 可能在结束时再触发一次，重复写会污染响应体。
func (w *responsesJSONWriter) finish() {
	w.once.Do(func() {
		w.emit()
	})
}

func (w *responsesJSONWriter) emit() {
	raw := w.captured.Bytes()
	w.captured.Reset()
	if len(bytes.TrimSpace(raw)) == 0 || w.passthrough {
		return
	}
	status := w.Status()
	if status == 0 {
		status = http.StatusOK
	}
	var chatResp map[string]interface{}
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		// 上游返回的不是 JSON（渠道错误页、网关 HTML 等），保持原样透出
		_, _ = w.ResponseWriter.Write(raw)
		return
	}
	// 错误路径：保留 HTTP 状态码，但把错误体换成 Responses 语义
	if _, ok := chatResp["error"]; ok || status >= http.StatusBadRequest {
		converted := buildErrorResponse(chatResp, status)
		out, err := json.Marshal(converted)
		if err != nil {
			_, _ = w.ResponseWriter.Write(raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
		w.ResponseWriter.WriteHeader(status)
		_, _ = w.ResponseWriter.Write(out)
		return
	}
	converted := responses.ConvertResponse(chatResp, w.model)
	responses.Remember(converted.ID, converted.OutputText)
	out, err := json.Marshal(converted)
	if err != nil {
		_, _ = w.ResponseWriter.Write(raw)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
	_, _ = w.ResponseWriter.Write(out)
}

// buildErrorResponse 把 OpenAI 风格的错误体转成 Responses 的错误形态
func buildErrorResponse(chatResp map[string]interface{}, status int) *responses.Response {
	message := "上游请求失败"
	code := "upstream_error"
	errType := "server_error"
	if raw, ok := chatResp["error"]; ok {
		switch v := raw.(type) {
		case string:
			message = v
		case map[string]interface{}:
			if m, ok := v["message"].(string); ok && m != "" {
				message = m
			}
			if c, ok := v["code"].(string); ok && c != "" {
				code = c
			}
			if t, ok := v["type"].(string); ok && t != "" {
				errType = t
			}
		}
	}
	if status == http.StatusTooManyRequests {
		code = "rate_limit_exceeded"
	}
	return &responses.Response{
		Object:    "response",
		Model:     stringField(chatResp["model"]),
		Status:    "failed",
		Error:     &responses.ErrorObject{Code: code, Message: message, Type: errType},
		CreatedAt: timeNowUnix(),
		ID:        "resp_err_" + fmt.Sprintf("%d", status),
	}
}

func stringField(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func timeNowUnix() int64 {
	return time.Now().Unix()
}

// ---------------------------------------------------------------------------
// 流式响应改写
// ---------------------------------------------------------------------------

type responsesSSEWriter struct {
	gin.ResponseWriter
	model string
	state *responses.StreamState
	once  sync.Once
	buf   *bytes.Buffer
}

func newResponsesSSEWriter(c *gin.Context, model string) gin.ResponseWriter {
	writer := &responsesSSEWriter{
		ResponseWriter: c.Writer,
		model:          model,
		state:          &responses.StreamState{FallbackModel: model},
		buf:            &bytes.Buffer{},
	}
	// 流式一定是 SSE 输出，提前定好 header，避免 gin 追加多余内容
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	return writer
}

func (w *responsesSSEWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	// SSE 以空行作为事件分隔，逐个事件处理
	for {
		chunk, rest, ok := cutEvent(w.buf)
		if !ok {
			break
		}
		w.emit(chunk)
		w.buf = bytes.NewBuffer(rest)
	}
	return len(b), nil
}

func (w *responsesSSEWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// cutEvent 从缓冲区里切出一个完整的 SSE 事件（以 "\n\n" 结尾）
func cutEvent(buf *bytes.Buffer) ([]byte, []byte, bool) {
	data := buf.Bytes()
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return nil, nil, false
	}
	event := data[:idx]
	rest := make([]byte, len(data)-(idx+2))
	copy(rest, data[idx+2:])
	return event, rest, true
}

func (w *responsesSSEWriter) emit(raw []byte) {
	payload := extractDataLine(raw)
	events, done := responses.ConvertStreamChunk(w.state, payload)
	for _, event := range events {
		_, _ = w.ResponseWriter.Write([]byte(responses.MarshalEvent(event)))
	}
	if done || payload == "[DONE]" {
		responses.Remember(w.state.ID, w.state.Text)
		return
	}
}

// extractDataLine 取出 SSE 事件里的 data: 负载
func extractDataLine(raw []byte) string {
	var builder strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			builder.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			builder.WriteString("\n")
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (w *responsesSSEWriter) Flush() {
	// 残余不足一个完整事件时忽略；客户端只会在收到 [DONE] 后关闭
	w.ResponseWriter.Flush()
}

func (w *responsesSSEWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func abortWithResponsesError(c *gin.Context, status int, code string, message string) {
	c.Header("Content-Type", "application/json")
	c.AbortWithStatusJSON(status, responses.Response{
		Object: "response",
		Status: "failed",
		Error:  &responses.ErrorObject{Code: code, Message: message, Type: "invalid_request_error"},
	})
}
