// Package mcp 是 One API Plus 的 MCP（Model Context Protocol）网关。
//
// 职责是把外部 MCP server 暴露的工具统一注册进来，转换成 OpenAI 风格的
// tools 数组，一方面可以直接塞进 chat/completions 请求让模型调用，
// 另一方面提供 /v1/mcp/tools 与 /v1/mcp/call 让 Agent 自己驱动。
//
// 协议支持：这里实现的是 MCP 的 HTTP/SSE 传输层（streamable HTTP），
// 通过 JSON-RPC 2.0 调用对端的 tools/list 与 tools/call 方法。
// stdio 形态的 server 需要先由外部桥接成 HTTP，再登记到本网关。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
)

// Tool 是 OpenAI tools[] 的形态，用于直接注入 chat 请求
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function 描述一个可被模型调用的函数
type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Server 表示一个已登记的 MCP server 及其工具缓存
type Server struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Protocol  string    `json:"protocol"`
	Enabled   bool      `json:"enabled"`
	Tools     []Tool    `json:"tools"`
	LastError string    `json:"last_error"`
	UpdatedAt time.Time `json:"updated_at"`
}

var (
	mu      sync.RWMutex
	servers = make(map[string]*Server)
	client  = &http.Client{Timeout: 30 * time.Second}
	reqID   int64
)

// Load 从配置与数据库双向合并 MCP server 列表
func Load() error {
	next := make(map[string]*Server)
	for _, cfg := range config.GetMCPServers() {
		next[cfg.Name] = &Server{
			Name:     cfg.Name,
			URL:      trimTransport(cfg.URL),
			Protocol: "http",
			Enabled:  cfg.Enabled,
		}
	}
	if model.DB != nil {
		list, err := model.GetAllMCPServers()
		if err != nil {
			logger.SysError("failed to load MCP servers: " + err.Error())
		}
		for _, item := range list {
			server, ok := next[item.Name]
			if !ok {
				server = &Server{Name: item.Name}
				next[item.Name] = server
			}
			server.URL = trimTransport(item.URL)
			server.Protocol = item.Protocol
			server.Enabled = item.Enabled
		}
	}
	mu.Lock()
	servers = next
	mu.Unlock()
	return nil
}

// trimTransport 允许登记 "sse://host/path" 这类写法，统一转成 http(s) URL
func trimTransport(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{"sse://", "stdio://", "http://", "https://"} {
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimPrefix(raw, prefix)
			if prefix == "sse://" || prefix == "stdio://" {
				return "http://" + rest
			}
			return prefix + rest
		}
	}
	if raw != "" && !strings.HasPrefix(raw, "http") {
		return "http://" + raw
	}
	return raw
}

// Enabled 网关总开关是否打开
func Enabled() bool {
	return config.AgentGatewayEnabled && config.MCPEnabled
}

// ListServers 返回全部 server 快照
func ListServers() []Server {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Server, 0, len(servers))
	for _, s := range servers {
		out = append(out, *s)
	}
	return out
}

// Discover 拉取所有已启用 server 的工具列表并缓存
func Discover() int {
	if !Enabled() {
		return 0
	}
	if err := Load(); err != nil {
		return 0
	}
	mu.RLock()
	list := make([]*Server, 0, len(servers))
	for _, s := range servers {
		if s.Enabled {
			list = append(list, s)
		}
	}
	mu.RUnlock()
	total := 0
	var wg sync.WaitGroup
	for _, s := range list {
		wg.Add(1)
		go func(server *Server) {
			defer wg.Done()
			tools, err := listTools(server)
			mu.Lock()
			if err != nil {
				server.LastError = truncate(err.Error(), 256)
			} else {
				server.Tools = tools
				server.LastError = ""
				server.UpdatedAt = time.Now()
				total += len(tools)
			}
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	logger.SysLog(fmt.Sprintf("mcp discovered %d tools from %d servers", total, len(list)))
	return total
}

// Tools 返回聚合后的全部工具（OpenAI 形态）
func Tools() []Tool {
	if !Enabled() {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Tool, 0)
	seen := make(map[string]bool)
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		for _, t := range s.Tools {
			if seen[t.Function.Name] {
				continue
			}
			seen[t.Function.Name] = true
			out = append(out, t)
		}
	}
	return out
}

// CallResult 是一次工具调用的返回值
type CallResult struct {
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
	Elapsed int64  `json:"elapsed_ms"`
}

// Call 找到能处理该工具的 server 并调用
func Call(toolName string, arguments string) (*CallResult, error) {
	if !Enabled() {
		return nil, errors.New("mcp gateway is disabled")
	}
	mu.RLock()
	var target *Server
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		for _, t := range s.Tools {
			if t.Function.Name == toolName {
				target = s
				break
			}
		}
		if target != nil {
			break
		}
	}
	mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("no MCP server provides tool %s", toolName)
	}
	start := time.Now()
	content, isError, err := callTool(target, toolName, arguments)
	if err != nil {
		return nil, err
	}
	return &CallResult{
		Server:  target.Name,
		Tool:    toolName,
		Content: content,
		IsError: isError,
		Elapsed: time.Since(start).Milliseconds(),
	}, nil
}

// ---------------------------------------------------------------------------
// JSON-RPC 传输层
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func nextID() int64 {
	// 单进程内的请求序号，配合 server URL 足以区分不同会话
	reqID++
	if reqID <= 0 {
		reqID = 1
	}
	return reqID
}

func post(server *Server, method string, params interface{}) (*rpcResponse, error) {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: nextID(), Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(config.MCPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp server %s returned %d: %s", server.Name, resp.StatusCode, truncate(string(body), 256))
	}
	var parsed rpcResponse
	if err = json.Unmarshal(parseSSEPayload(body), &parsed); err != nil {
		return nil, fmt.Errorf("invalid json-rpc response from %s: %w", server.Name, err)
	}
	if parsed.Error != nil {
		return nil, errors.New(parsed.Error.Message)
	}
	return &parsed, nil
}

// parseSSEPayload 处理 SSE 形态响应：取最后一条 data: 行
func parseSSEPayload(body []byte) []byte {
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "data:") {
		return body
	}
	last := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if last == "" {
		return body
	}
	return []byte(last)
}

func listTools(server *Server) ([]Tool, error) {
	resp, err := post(server, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err = json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	tools := make([]Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tools = append(tools, Tool{
			Type: "function",
			Function: Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return tools, nil
}

func callTool(server *Server, name string, argumentsJSON string) (string, bool, error) {
	var args map[string]interface{}
	if strings.TrimSpace(argumentsJSON) == "" {
		args = map[string]interface{}{}
	} else if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return "", true, fmt.Errorf("invalid tool arguments: %w", err)
	}
	resp, err := post(server, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	if err != nil {
		return "", true, err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err = json.Unmarshal(resp.Result, &result); err != nil {
		return "", true, err
	}
	parts := make([]string, 0, len(result.Content))
	for _, c := range result.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return "", result.IsError, nil
	}
	return strings.Join(parts, "\n"), result.IsError, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
