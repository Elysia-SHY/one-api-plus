package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common/config"
)

func testRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(ResponsesAdapter())
	engine.POST("/v1/responses", handler)
	return engine
}

func TestResponsesAdapterNonStream(t *testing.T) {
	config.ResponsesAPIEnabled = true
	defer func() { config.ResponsesAPIEnabled = true }()

	// 假的下游 handler：拿到被改写后的请求，返回一份 chat completion
	var capturedBody string
	var capturedPath string
	engine := testRouter(func(c *gin.Context) {
		buf := make([]byte, c.Request.ContentLength)
		_, _ = c.Request.Body.Read(buf)
		capturedBody = string(buf)
		capturedPath = c.Request.URL.Path
		c.JSON(http.StatusOK, gin.H{
			"id":    "chatcmpl-xyz",
			"model": "gpt-4o",
			"choices": []gin.H{
				{"message": gin.H{"role": "assistant", "content": "你好，世界"}, "finish_reason": "stop"},
			},
			"usage": gin.H{"prompt_tokens": 9, "completion_tokens": 4, "total_tokens": 13},
		})
	})

	body := `{"model":"gpt-4o","input":"打个招呼","instructions":"简洁回答","max_output_tokens":200}`
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	// 1) 请求已被改写成 chat 形态，并且指向 chat/completions
	if capturedPath != "/v1/chat/completions" {
		t.Errorf("path should be rewritten, got %s", capturedPath)
	}
	if !strings.Contains(capturedBody, `"messages"`) || strings.Contains(capturedBody, `"input"`) {
		t.Errorf("body not converted to chat form: %s", capturedBody)
	}
	// 2) 响应已变回 Responses 形态
	var out map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid json: %v (%s)", err, recorder.Body.String())
	}
	if out["object"] != "response" {
		t.Errorf("object should be response, got %v", out["object"])
	}
	if out["status"] != "completed" {
		t.Errorf("status should be completed, got %v", out["status"])
	}
	if out["output_text"] != "你好，世界" {
		t.Errorf("output_text mismatch: %v", out["output_text"])
	}
	if !strings.HasPrefix(out["id"].(string), "resp_") {
		t.Errorf("id should be resp_ prefixed, got %v", out["id"])
	}
	usage, ok := out["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage missing")
	}
	if usage["total_tokens"] != float64(13) {
		t.Errorf("usage mismatch: %+v", usage)
	}
}

func TestResponsesAdapterStream(t *testing.T) {
	config.ResponsesAPIEnabled = true
	engine := testRouter(func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		writer := c.Writer
		_, _ = writer.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
		writer.Flush()
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"gpt-4o","input":"hi","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	out := recorder.Body.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		"event: response.completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in stream output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "chatcmpl") {
		t.Errorf("stream should not leak chat events:\n%s", out)
	}
	// delta 事件里应带上累计前的增量内容
	if !strings.Contains(out, `"delta":"你"`) || !strings.Contains(out, `"delta":"好"`) {
		t.Errorf("delta payloads missing:\n%s", out)
	}
}

func TestResponsesAdapterErrorPassthrough(t *testing.T) {
	config.ResponsesAPIEnabled = true
	engine := testRouter(func(c *gin.Context) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "quota exceeded",
				"type":    "insufficient_quota",
				"code":    "quota_exceeded",
			},
		})
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"gpt-4o","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Errorf("status code should be preserved, got %d", recorder.Code)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["object"] != "response" || out["status"] != "failed" {
		t.Errorf("error response shape mismatch: %+v", out)
	}
	errObj, ok := out["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %+v", out)
	}
	if errObj["message"] != "quota exceeded" {
		t.Errorf("error message should be preserved, got %v", errObj["message"])
	}
}

func TestResponsesAdapterRejectsBadRequest(t *testing.T) {
	config.ResponsesAPIEnabled = true
	engine := testRouter(func(c *gin.Context) {
		t.Errorf("handler should not be reached")
	})
	cases := map[string]int{
		`{}`:                 http.StatusBadRequest, // 缺少 model
		`{"model":"gpt-4o"}`: http.StatusBadRequest, // 缺少 input
		`not-json`:           http.StatusBadRequest,
		``:                   http.StatusBadRequest,
	}
	for body, want := range cases {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Errorf("body %q: expected %d, got %d (%s)", body, want, recorder.Code, recorder.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
			t.Errorf("body %q: error response should be json, got %s", body, recorder.Body.String())
		}
	}
}

func TestResponsesAdapterDisabled(t *testing.T) {
	config.ResponsesAPIEnabled = false
	defer func() { config.ResponsesAPIEnabled = true }()
	reached := false
	engine := testRouter(func(c *gin.Context) {
		reached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"gpt-4o","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)
	if !reached {
		t.Errorf("adapter disabled should pass through untouched")
	}
	if strings.Contains(recorder.Body.String(), `"object":"response"`) {
		t.Errorf("disabled adapter must not convert anything, got %s", recorder.Body.String())
	}
}
