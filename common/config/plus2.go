package config

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Elysia-SHY/one-api-plus/common/env"
)

// ---------------------------------------------------------------------------
// One API Plus —— 第二阶段（AI Gateway）配置
//
// 目标：把 One API Plus 从单纯的 API 中转升级成个人 AI Gateway：
// 具备模型能力库、模型组抽象、负载感知路由、上下文缓存、Agent / MCP / Memory、
// Responses API 兼容、可观测 Dashboard、频率限制与 Lite 裁剪。
//
// 所有开关均可通过环境变量设置，也可在管理后台「设置」页在线修改。
// ---------------------------------------------------------------------------

// 模型能力数据库
var (
	// CapabilityEnabled 是否启用模型能力数据库
	CapabilityEnabled = env.Bool("CAPABILITY_ENABLED", true)
	// CapabilityDetect 是否在模型首次出现时做一次在线能力探测（推断不出时才发请求）
	CapabilityDetect = env.Bool("CAPABILITY_DETECT", false)
	// CapabilityTTLSeconds 能力记录的本地缓存有效期
	CapabilityTTLSeconds = env.Int("CAPABILITY_TTL", 3600)
)

// 模型组系统（逻辑模型层）
var (
	ModelGroupEnabled = env.Bool("MODEL_GROUP_ENABLED", true)
	// ModelGroupStrategy: first_available | random | round_robin | capability
	ModelGroupStrategy = env.String("MODEL_GROUP_STRATEGY", "capability")
)

// 智能路由（第二阶段：负载感知）
var (
	// RoutingLoadAware 是否把「当前并发负载」计入路由评分
	RoutingLoadAware = env.Bool("ROUTING_LOAD_AWARE", true)
	// RoutingMaxLoad 单渠道软并发上限，超过后评分快速下降
	RoutingMaxLoad = env.Int("ROUTING_MAX_LOAD", 32)
	// RoutingWeights 四维权重：{"latency":0.3,"cost":0.2,"stability":0.3,"load":0.2}
	RoutingWeightsRaw = env.String("ROUTING_WEIGHTS", "")
)

// Prompt / 上下文缓存
var (
	PromptCacheEnabled = env.Bool("PROMPT_CACHE_ENABLED", false)
	PromptCacheTTL     = env.Int("PROMPT_CACHE_TTL", 1800)       // 单位：秒
	PromptCacheMinTok  = env.Int("PROMPT_CACHE_MIN_TOKENS", 512) // 低于该前缀长度不写缓存
)

// Agent Gateway
var (
	AgentGatewayEnabled = env.Bool("AGENT_GATEWAY_ENABLED", true)
	ToolCallEnabled     = env.Bool("TOOL_CALL_ENABLED", true)
	// ToolLoopMaxSteps 网关侧自动执行工具调用的最大轮数（0 表示不做自动编排，仅透传）
	ToolLoopMaxSteps = env.Int("TOOL_LOOP_MAX_STEPS", 0)

	MCPEnabled    = env.Bool("MCP_ENABLED", false)
	MCPServersRaw = env.String("MCP_SERVERS", "")
	MCPTimeout    = env.Int("MCP_TIMEOUT", 30)

	MemoryEnabled  = env.Bool("MEMORY_ENABLED", true)
	MemoryMaxItems = env.Int("MEMORY_MAX_ITEMS", 200)
	MemoryTTL      = env.Int("MEMORY_TTL", 604800) // 单位：秒，默认 7 天
	MemoryMaxChars = env.Int("MEMORY_MAX_CHARS", 4096)
)

// Responses API（新版 OpenAI 接口，Codex CLI / Responses SDK 使用）
var (
	ResponsesAPIEnabled = env.Bool("RESPONSES_API_ENABLED", true)
	ResponsesStoreItems = env.Bool("RESPONSES_STORE_ITEMS", true)
)

// Dashboard 与统计
var (
	DashboardEnabled = env.Bool("DASHBOARD_ENABLED", true)
	// DashboardTopN Dashboard 里 Top N 模型/渠道
	DashboardTopN = env.Int("DASHBOARD_TOP_N", 10)
)

// 频率限制与安全增强
var (
	RateLimitEnabled = env.Bool("RATE_LIMIT_ENABLED", false)
	// RateLimitQPM 默认每分钟请求数；0 表示不限（仍可用 per-token 覆盖）
	RateLimitQPM = env.Int("RATE_LIMIT_QPM", 60)
	// RateLimitBurst 令牌桶容量，允许的瞬时突发
	RateLimitBurst = env.Int("RATE_LIMIT_BURST", 10)
	// RateLimitPerModel 是否按「用户 + 模型」维度分别限流
	RateLimitPerModel = env.Bool("RATE_LIMIT_PER_MODEL", false)
	// RateLimitConcurrent 单用户最大并发中继数，0 表示不限制
	RateLimitConcurrent = env.Int("RATE_LIMIT_CONCURRENT", 0)
)

// ---------------------------------------------------------------------------
// 模型组策略
// ---------------------------------------------------------------------------

const (
	GroupStrategyFirstAvailable = "first_available"
	GroupStrategyRandom         = "random"
	GroupStrategyRoundRobin     = "round_robin"
	GroupStrategyCapability     = "capability"
)

// ---------------------------------------------------------------------------
// 路由权重表
// ---------------------------------------------------------------------------

// RoutingScoreWeights 四维评分权重，缺省使用内置默认值
type RoutingScoreWeights struct {
	Latency   float64 `json:"latency"`
	Cost      float64 `json:"cost"`
	Stability float64 `json:"stability"`
	Load      float64 `json:"load"`
}

var (
	scoreWeights   = RoutingScoreWeights{Latency: 0.3, Cost: 0.2, Stability: 0.3, Load: 0.2}
	scoreWeightMux sync.RWMutex
)

// DefaultWeights 返回内置默认权重
func DefaultWeights() RoutingScoreWeights {
	return RoutingScoreWeights{Latency: 0.3, Cost: 0.2, Stability: 0.3, Load: 0.2}
}

// SetRoutingWeights 用 JSON 字符串更新权重表；非法输入返回错误
func SetRoutingWeights(raw string) error {
	w := DefaultWeights()
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &w); err != nil {
			return err
		}
	}
	if total := w.Latency + w.Cost + w.Stability + w.Load; total <= 0 {
		w = DefaultWeights()
	} else if !floatsAlmostOne(total) {
		w.Latency /= total
		w.Cost /= total
		w.Stability /= total
		w.Load /= total
	}
	scoreWeightMux.Lock()
	scoreWeights = w
	scoreWeightMux.Unlock()
	return nil
}

func floatsAlmostOne(v float64) bool {
	diff := v - 1
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-6
}

// GetRoutingWeights 返回当前生效的评分权重
func GetRoutingWeights() RoutingScoreWeights {
	scoreWeightMux.RLock()
	defer scoreWeightMux.RUnlock()
	return scoreWeights
}

// ---------------------------------------------------------------------------
// MCP 服务器注册表：{"name":{"url":"http://127.0.0.1:8765/sse","enabled":true}}
// ---------------------------------------------------------------------------

// MCPServerConfig 描述一个外部 MCP 服务器
type MCPServerConfig struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

var (
	mcpServers   map[string]MCPServerConfig
	mcpServerMux sync.RWMutex
)

// SetMCPServers 用 JSON 字符串更新 MCP 服务器注册表
func SetMCPServers(raw string) error {
	next := make(map[string]MCPServerConfig)
	if strings.TrimSpace(raw) == "" {
		mcpServerMux.Lock()
		mcpServers = next
		mcpServerMux.Unlock()
		return nil
	}
	var list map[string]MCPServerConfig
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return err
	}
	for k, v := range list {
		if k == "" || v.URL == "" {
			continue
		}
		v.Name = k
		v.Enabled = true
		next[k] = v
	}
	mcpServerMux.Lock()
	mcpServers = next
	mcpServerMux.Unlock()
	return nil
}

// GetMCPServers 返回当前注册的 MCP 服务器（拷贝）
func GetMCPServers() []MCPServerConfig {
	mcpServerMux.RLock()
	defer mcpServerMux.RUnlock()
	out := make([]MCPServerConfig, 0, len(mcpServers))
	for _, v := range mcpServers {
		out = append(out, v)
	}
	return out
}

func init() {
	_ = SetRoutingWeights(RoutingWeightsRaw)
	_ = SetMCPServers(MCPServersRaw)
}
