package config

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Elysia-SHY/one-api-plus/common/env"
)

// ---------------------------------------------------------------------------
// One API Plus —— 增强特性配置
//
// 所有开关都可以通过环境变量设置，也可以在管理后台「设置」页面里在线修改
// （见 model/option.go 中注册的 Plus 选项）。
// ---------------------------------------------------------------------------

// 模型自动同步
var (
	ModelSyncEnabled       = env.Bool("MODEL_SYNC_ENABLED", false)
	ModelSyncInterval      = env.Int("MODEL_SYNC_INTERVAL", 360) // 单位：分钟
	ModelSyncTimeout       = env.Int("MODEL_SYNC_TIMEOUT", 20)   // 单位：秒
	ModelSyncConcurrency   = env.Int("MODEL_SYNC_CONCURRENCY", 4)
	ModelSyncAutoEnable    = env.Bool("MODEL_SYNC_AUTO_ENABLE", false) // 自动把新模型追加进渠道
	ModelSyncDefaultStatus = env.String("MODEL_SYNC_DEFAULT_STATUS", ModelStatusNew)
)

// 渠道健康检测
var (
	HealthCheckEnabled         = env.Bool("HEALTH_CHECK_ENABLED", false)
	HealthCheckInterval        = env.Int("HEALTH_CHECK_INTERVAL", 5)            // 单位：分钟
	HealthCheckTimeout         = env.Int("HEALTH_CHECK_TIMEOUT", 15)            // 单位：秒
	HealthCheckDegradedLatency = env.Int("HEALTH_CHECK_DEGRADED_LATENCY", 3000) // ms，超过即降级
	HealthCheckDeadLatency     = env.Int("HEALTH_CHECK_DEAD_LATENCY", 15000)    // ms，超过即失效
	HealthCheckErrorRatePause  = env.Float64("HEALTH_CHECK_ERROR_RATE_PAUSE", 0.6)
	HealthCheckFailPause       = env.Int("HEALTH_CHECK_FAIL_PAUSE", 3) // 连续失败次数
	HealthCheckAutoPause       = env.Bool("HEALTH_CHECK_AUTO_PAUSE", false)
)

// 智能路由
var (
	// priority | weight | latency | cost | stability
	RoutingStrategy = env.String("ROUTING_STRATEGY", StrategyPriority)
)

// 自动 Fallback
var (
	FallbackEnabled = env.Bool("FALLBACK_ENABLED", true)
	// FallbackModels 形如 {"gpt-4o":["claude-3-5-sonnet-20241022","deepseek-chat"]}
	FallbackModelsRaw = env.String("FALLBACK_MODELS", "")
)

// 模型别名；DEFAULT_ALIASES 形如 {"gemini-flash":"gemini-2.5-flash-preview"}
var (
	AliasEnabled  = env.Bool("ALIAS_ENABLED", true)
	DefaultAliases = env.String("DEFAULT_ALIASES", "")
)

// 重复请求缓存（Redis）
var (
	ResponseCacheEnabled = env.Bool("RESPONSE_CACHE_ENABLED", false)
	ResponseCacheTTL     = env.Int("RESPONSE_CACHE_TTL", 600)        // 单位：秒
	ResponseCacheMaxBody = env.Int("RESPONSE_CACHE_MAX_BODY", 65536) // 单条响应最大字节
)

// 成本与预算
var (
	BudgetEnabled        = env.Bool("BUDGET_ENABLED", false)
	DefaultDailyBudget   = env.Int("DEFAULT_DAILY_BUDGET", 0)   // 单位：额度点，0 表示不限制
	DefaultMonthlyBudget = env.Int("DEFAULT_MONTHLY_BUDGET", 0) // 单位：额度点，0 表示不限制
)

// Lite 模式：面向 OpenWrt / 随身 WiFi 等小内存设备，关闭同步/检测类后台任务
var LiteMode = env.Bool("LITE_MODE", false)

// ---------------------------------------------------------------------------
// 模型状态
// ---------------------------------------------------------------------------

const (
	ModelStatusNormal   = "normal"   // 正常
	ModelStatusDegraded = "degraded" // 降级
	ModelStatusPaused   = "paused"   // 暂停
	ModelStatusInvalid  = "invalid"  // 失效
	ModelStatusNew      = "new"      // 新发现，待确认
)

// ---------------------------------------------------------------------------
// 渠道健康状态
// ---------------------------------------------------------------------------

const (
	HealthStatusHealthy  = "healthy"
	HealthStatusDegraded = "degraded"
	HealthStatusPaused   = "paused"
	HealthStatusDead     = "dead"
	HealthStatusUnknown  = "unknown"
)

// ---------------------------------------------------------------------------
// 路由策略
// ---------------------------------------------------------------------------

const (
	StrategyPriority  = "priority"  // 按渠道优先级（兼容原版行为）
	StrategyWeight    = "weight"    // 权重调度
	StrategyLatency   = "latency"   // 延迟优先
	StrategyCost      = "cost"      // 成本优先
	StrategyStability = "stability" // 稳定性优先
)

// ---------------------------------------------------------------------------
// Fallback 模型映射表
// ---------------------------------------------------------------------------

var (
	fallbackModelsMap map[string][]string
	fallbackMapLock   sync.RWMutex
)

// SetFallbackModels 用 JSON 字符串更新 fallback 映射表
func SetFallbackModels(raw string) error {
	m := make(map[string][]string)
	if strings.TrimSpace(raw) == "" {
		fallbackMapLock.Lock()
		fallbackModelsMap = m
		fallbackMapLock.Unlock()
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return err
	}
	fallbackMapLock.Lock()
	fallbackModelsMap = m
	fallbackMapLock.Unlock()
	return nil
}

// GetFallbackModels 返回某个模型的备用模型列表
func GetFallbackModels(model string) []string {
	fallbackMapLock.RLock()
	defer fallbackMapLock.RUnlock()
	if fallbackModelsMap == nil {
		return nil
	}
	return fallbackModelsMap[model]
}

// AllFallbackModels 返回完整的 fallback 映射（拷贝）
func AllFallbackModels() map[string][]string {
	fallbackMapLock.RLock()
	defer fallbackMapLock.RUnlock()
	copied := make(map[string][]string, len(fallbackModelsMap))
	for k, v := range fallbackModelsMap {
		copied[k] = append([]string(nil), v...)
	}
	return copied
}

func init() {
	_ = SetFallbackModels(FallbackModelsRaw)
}
