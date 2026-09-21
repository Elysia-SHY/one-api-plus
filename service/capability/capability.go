// Package capability 维护 One API Plus 的模型能力数据库。
//
// 三层信息来源，按可靠性递增依次覆盖：
//  1. seed     —— 内置知识库：常见模型的上下文窗口、视觉、工具调用、推理、嵌入能力
//  2. rule     —— 名称规则推断：从模型名里的 vision / embed / reasoning / thinking 等关键词推断
//  3. probe    —— 在线探测：真正请求一次上游（/chat/completions 极小请求 + /embeddings），
//     只有前两者都给不出结论时才用，可在 CAPABILITY_DETECT 关闭
//
// 结论写入 model_capabilities 表，并在必要时回填到 model_catalog，
// 供路由打分、模型组挑选、Dashboard 展示使用。
package capability

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
)

const cacheTTL = 5 * time.Minute

var (
	cache   map[string]*model.ModelCapability
	cacheAt time.Time
	mu      sync.RWMutex
)

// ---------------------------------------------------------------------------
// 内置知识库
// ---------------------------------------------------------------------------

// seed 记录了主力模型的已知能力：上下文长度、输出上限，以及各能力标记
type seed struct {
	Context   int
	MaxOutput int
	Vision    int
	Tool      int
	Reason    int
	Embed     int
	Stream    int
	JSON      int
}

func s(ctx int, out int) seed {
	return seed{Context: ctx, MaxOutput: out, Stream: model.CapSupported, JSON: model.CapSupported}
}

var seeds = map[string]seed{
	// OpenAI
	"gpt-4o":                 {128000, 16384, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gpt-4o-mini":            {128000, 16384, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gpt-4-turbo":            {128000, 4096, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gpt-4":                  {8192, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gpt-3.5-turbo":          {16385, 4096, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"o1":                     {200000, 100000, model.CapSupported, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported},
	"o1-mini":                {128000, 65536, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported},
	"o3-mini":                {200000, 100000, model.CapUnsupported, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported},
	"text-embedding-3-large": {8191, 0, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},
	"text-embedding-3-small": {8191, 0, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},
	"text-embedding-ada-002": {8191, 0, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},

	// Anthropic
	"claude-3-5-sonnet-20241022": {200000, 8192, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"claude-3-5-haiku-20241022":  {200000, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"claude-3-opus-20240229":     {200000, 4096, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"claude-3-sonnet-20240229":   {200000, 4096, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"claude-3-haiku-20240307":    {200000, 4096, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},

	// Google
	"gemini-2.5-flash-preview": {1048576, 65536, model.CapSupported, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gemini-2.5-pro-preview":   {1048576, 65536, model.CapSupported, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gemini-2.0-flash":         {1048576, 8192, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gemini-1.5-pro":           {2097152, 8192, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"gemini-1.5-flash":         {1048576, 8192, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"text-embedding-004":       {2048, 0, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported},

	// DeepSeek
	"deepseek-chat":     {65536, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"deepseek-reasoner": {65536, 65536, model.CapUnsupported, model.CapSupported, model.CapSupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"deepseek-coder":    {65536, 8192, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},

	// Qwen / Moonshot / Zhipu / Yi
	"qwen-max":                   {32768, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"qwen-plus":                  {131072, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"qwen-turbo":                 {131072, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"qwen2.5-coder-32b-instruct": {131072, 8192, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"glm-4":                      {128000, 4096, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"glm-4-flash":                {128000, 4096, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"moonshot-v1-128k":           {128000, 4096, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
	"yi-large":                   {32768, 4096, model.CapUnsupported, model.CapSupported, model.CapUnsupported, model.CapUnsupported, model.CapSupported, model.CapSupported},
}

var (
	reEmbed    = regexp.MustCompile(`(?i)(embed|bge|gte|text-similarity|text-embedding)`)
	reVision   = regexp.MustCompile(`(?i)(vision|vl|multimodal|image|omni)`)
	reReason   = regexp.MustCompile(`(?i)(reason|thinking|o1|o3|r1|qwq)`)
	reRerank   = regexp.MustCompile(`(?i)(rerank)`)
	reAudio    = regexp.MustCompile(`(?i)(whisper|tts|audio|speech|asr)`)
	reImageGen = regexp.MustCompile(`(?i)(dall-e|dalle|flux|sdxl|stable-diffusion|midjourney|image-gen)`)
)

// Infer 在不查库的情况下，仅依据模型名推断能力画像
func Infer(modelName string) *model.ModelCapability {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return nil
	}
	item := &model.ModelCapability{
		ModelName:   modelName,
		Source:      "rule",
		Streaming:   model.CapSupported,
		JSONMode:    model.CapSupported,
		InputTypes:  "text",
		OutputTypes: "text",
	}
	if v, ok := seeds[name]; ok {
		applySeed(item, v)
		item.Source = "seed"
		return item
	}
	// 去掉 :latest / 供应商前缀后再试一次
	for _, variant := range variants(name) {
		if v, ok := seeds[variant]; ok {
			applySeed(item, v)
			item.Source = "seed"
			return item
		}
	}
	switch {
	case reEmbed.MatchString(name):
		item.Embedding = model.CapSupported
		item.ToolCall = model.CapUnsupported
		item.Vision = model.CapUnsupported
		item.ContextLength = 8192
	case reRerank.MatchString(name):
		item.Embedding = model.CapUnsupported
		item.ToolCall = model.CapUnsupported
		item.ContextLength = 8192
	case reImageGen.MatchString(name):
		item.ToolCall = model.CapUnsupported
		item.OutputTypes = "image"
		item.ContextLength = 4096
	case reAudio.MatchString(name):
		item.InputTypes = "audio"
		item.OutputTypes = "audio"
		item.ToolCall = model.CapUnsupported
	default:
		// 通用对话模型：默认具备工具调用能力（现代模型基本都支持）
		item.ToolCall = model.CapSupported
	}
	if reVision.MatchString(name) {
		item.Vision = model.CapSupported
		item.InputTypes = "text,image"
	}
	if reReason.MatchString(name) {
		item.Reasoning = model.CapSupported
	}
	item.ContextLength = guessContext(name, item.ContextLength)
	if item.MaxOutput == 0 {
		item.MaxOutput = 4096
	}
	return item
}

func applySeed(item *model.ModelCapability, v seed) {
	item.ContextLength = v.Context
	item.MaxOutput = v.MaxOutput
	item.Vision = v.Vision
	item.ToolCall = v.Tool
	item.Reasoning = v.Reason
	item.Embedding = v.Embed
	item.Streaming = v.Stream
	item.JSONMode = v.JSON
	item.InputTypes = "text"
	if v.Vision == model.CapSupported {
		item.InputTypes = "text,image"
	}
	item.OutputTypes = "text"
	if v.JSON == model.CapSupported {
		item.OutputTypes = "text,json"
	}
}

// variants 生成可能命中的别名形式：去掉供应商前缀、去掉日期后缀等
func variants(name string) []string {
	out := make([]string, 0, 4)
	base := name
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
		out = append(out, base)
	}
	if idx := strings.Index(name, ":"); idx > 0 {
		out = append(out, name[:idx])
	}
	out = append(out, strings.TrimSuffix(name, "-latest"), strings.TrimSuffix(name, "-preview"))
	return out
}

// guessContext 从名字里的 8k/32k/128k/1m 等标记猜上下文窗口
func guessContext(name string, fallback int) int {
	if fallback > 0 {
		return fallback
	}
	n := len(name)
	for i := 0; i < n; i++ {
		if name[i] < '0' || name[i] > '9' {
			continue
		}
		j := i
		for j < n && name[j] >= '0' && name[j] <= '9' {
			j++
		}
		num := 0
		for _, ch := range name[i:j] {
			num = num*10 + int(ch-'0')
		}
		suffix := name[j:]
		if strings.HasPrefix(suffix, "k") {
			return num * 1000
		}
		if strings.HasPrefix(suffix, "m") {
			return num * 1000000
		}
		i = j
	}
	return 0
}

// ---------------------------------------------------------------------------
// 查询入口
// ---------------------------------------------------------------------------

func ensureLoaded() bool {
	mu.RLock()
	fresh := cache != nil && time.Since(cacheAt) < cacheTTL
	mu.RUnlock()
	if fresh {
		return true
	}
	if err := Refresh(); err != nil {
		logger.SysError("failed to refresh capability cache: " + err.Error())
		return cache != nil
	}
	return true
}

// Refresh 重新从数据库加载全部能力记录到本地缓存
func Refresh() error {
	if model.DB == nil {
		return nil
	}
	items, err := model.GetAllCapabilities()
	if err != nil {
		return err
	}
	next := make(map[string]*model.ModelCapability, len(items))
	for _, item := range items {
		next[strings.ToLower(item.ModelName)] = item
	}
	mu.Lock()
	cache = next
	cacheAt = time.Now()
	mu.Unlock()
	return nil
}

// Get 返回模型的能力画像：优先查库，查不到则用规则推断并落库（merge 模式）
func Get(modelName string) *model.ModelCapability {
	if !config.CapabilityEnabled || strings.TrimSpace(modelName) == "" {
		return nil
	}
	if model.DB == nil {
		return Infer(modelName)
	}
	ensureLoaded()
	mu.RLock()
	item, ok := cache[strings.ToLower(modelName)]
	mu.RUnlock()
	if ok && item != nil {
		return item
	}
	// 未入库：用规则推断一份并写回，下次就能直接命中
	inferred := Infer(modelName)
	if inferred != nil {
		if err := model.UpsertCapability(inferred, true); err != nil {
			logger.SysError("failed to persist inferred capability: " + err.Error())
		}
		mu.Lock()
		if cache != nil {
			cache[strings.ToLower(modelName)] = inferred
		}
		mu.Unlock()
	}
	return inferred
}

// Ensure 为一个模型建立能力记录；返回是否新写入
func Ensure(modelName string) (*model.ModelCapability, bool) {
	existing, err := model.GetCapability(modelName)
	if err != nil {
		logger.SysError("failed to query capability: " + err.Error())
	}
	if existing != nil && existing.ContextLength > 0 {
		return existing, false
	}
	inferred := Infer(modelName)
	if inferred == nil {
		return existing, false
	}
	if err := model.UpsertCapability(inferred, true); err != nil {
		logger.SysError("failed to upsert capability: " + err.Error())
	}
	return inferred, true
}

// BulkEnsure 批量为模型名建立能力记录，通常在模型同步完成后调用
func BulkEnsure(channelId int, modelNames []string) int {
	created := 0
	for _, name := range modelNames {
		item, isNew := Ensure(name)
		if isNew {
			created++
		}
		if item != nil && channelId > 0 {
			if err := model.SyncCapabilityToChannel(channelId, name, item); err != nil {
				logger.SysError("failed to sync capability to catalog: " + err.Error())
			}
		}
	}
	return created
}

// Filter 按能力条件筛选已登记的模型名（用于模型组的 capability 策略）
func Filter(names []string, ctxLen int, vision bool, toolCall bool, reasoning bool) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		item := Get(name)
		if item == nil {
			continue
		}
		if ctxLen > 0 && item.ContextLength > 0 && item.ContextLength < ctxLen {
			continue
		}
		if vision && item.Vision == model.CapUnsupported {
			continue
		}
		if toolCall && item.ToolCall == model.CapUnsupported {
			continue
		}
		if reasoning && item.Reasoning == model.CapUnsupported {
			continue
		}
		out = append(out, name)
	}
	return out
}

// Set 人工写入/覆盖一条能力记录
func Set(item *model.ModelCapability) error {
	item.Source = "manual"
	if err := model.UpsertCapability(item, false); err != nil {
		return err
	}
	return Refresh()
}
