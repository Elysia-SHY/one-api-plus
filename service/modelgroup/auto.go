package modelgroup

import (
	"context"
	"sort"
	"strings"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
)

// ---------------------------------------------------------------------------
// 自动建组（Auto Group）
//
// 目标：用户不需要理解「模型」「渠道」「分组」三者关系，也不需要手工挑型号。
// 网关按内置的用途分类规则，把当前分组下所有真实可用的模型自动聚合成若干
// 逻辑组（如 auto-chat / auto-coding / auto-vision / auto-cheap / auto-all），
// 令牌里勾选这些逻辑名即可，请求时 distributor 会自动解析到当前可用的真实模型。
//
// 「自动」体现在两个层面：
//   1. 建组自动化：一次调用把分类结果写库，无需人工配置成员；
//   2. 成员自愈：每次调用都按「当前有哪些模型真的有可用渠道」重新计算，
//      上游换型号、渠道被禁用后，重新执行一次即跟上现状。
//
// 自动组的组名以 auto- 前缀标识，方便与用户手工创建的组区分，
// 重跑时只覆盖自己的 auto- 组，不动用户手工组。
// ---------------------------------------------------------------------------

// AutoGroupPrefix 自动生成组的组名前缀
const AutoGroupPrefix = "auto-"

// autoGroupRule 描述一条自动分组的匹配规则
type autoGroupRule struct {
	// Name 组名（不含前缀）
	Name string
	// Remark 组说明
	Remark string
	// Strategy 组内挑选策略
	Strategy string
	// Priority 优先级：越大越先建，用来自动建组的排序
	Priority int
	// Match 判断一个模型是否属于该组
	Match func(modelName string) bool
}

// autoGroupRules 内置自动分组规则表。
//
// 顺序即优先级：越靠前的组越「专」，一个模型可以同时属于多个组
// （例如 gpt-4o 既在 auto-chat，也在 auto-vision）。
// 除 auto-all 外，每个模型最多进入前 maxAutoGroupsPerModel 个命中组，
// 避免一个模型到处出现把选择界面撑爆。
var autoGroupRules = []autoGroupRule{
	{
		Name:     "coding",
		Remark:   "自动分组：代码 / 工具调用能力较强的模型",
		Strategy: config.GroupStrategyCapability,
		Priority: 100,
		Match: func(name string) bool {
			return containsAny(name,
				"claude", "gpt-4", "gpt-5", "o1", "o3", "o4", "deepseek-coder",
				"deepseek-v3", "deepseek-r1", "qwen3-coder", "qwen-coder",
				"codestral", "codex", "grok-code", "kimi-k2", "glm-4.6", "glm-4.5")
		},
	},
	{
		Name:     "vision",
		Remark:   "自动分组：支持图片输入的模型",
		Strategy: config.GroupStrategyCapability,
		Priority: 90,
		Match: func(name string) bool {
			if containsAny(name, "embedding", "embed", "rerank", "tts", "whisper",
				"audio", "speech", "moderation", "image-1", "dall-e", "stable-diffusion") {
				return false
			}
			return containsAny(name, "vision", "-vl", "vl-", "gpt-4o", "gpt-4.1",
				"gpt-4-turbo", "gpt-5", "claude-3", "claude-4", "claude-sonnet",
				"claude-opus", "claude-haiku", "gemini", "qwen-vl", "qwen2-vl",
				"qwen2.5-vl", "qwen3-vl", "glm-4v", "glm-4.5v", "internvl",
				"llava", "pixtral", "grok-vision", "step-1v", "moonshot-v1-vision")
		},
	},
	{
		Name:     "reasoning",
		Remark:   "自动分组：具备深度推理能力的模型",
		Strategy: config.GroupStrategyCapability,
		Priority: 80,
		Match: func(name string) bool {
			return containsAny(name, "o1", "o3", "o4-mini", "r1", "reason",
				"thinking", "-think", "qwq", "qwen3-max", "deepseek-reasoner",
				"glm-z1", "magistral", "grok-3-reasoner")
		},
	},
	{
		Name:     "cheap",
		Remark:   "自动分组：轻量 / 廉价模型，适合批量任务",
		Strategy: config.GroupStrategyFirstAvailable,
		Priority: 70,
		Match: func(name string) bool {
			// 这些非对话模型本来也不该出现在廉价对话组里
			if isNonChatModel(name) {
				return false
			}
			// 用词元级匹配，避免 "mini" 命中 "gemini"
			return hasTierToken(name, "mini", "flash", "turbo", "lite", "small",
				"nano", "haiku", "8b", "7b", "4b", "3b", "1.5b", "0.5b",
				"free", "spark", "air")
		},
	},
	{
		Name:     "longcontext",
		Remark:   "自动分组：长上下文模型",
		Strategy: config.GroupStrategyCapability,
		Priority: 60,
		Match: func(name string) bool {
			return containsAny(name, "128k", "200k", "256k", "1m", "long",
				"gemini-1.5-pro", "gemini-2", "gemini-2.5", "qwen-long",
				"moonshot-v1-128k", "moonshot-v1-32k", "yi-large")
		},
	},
	{
		Name:     "embedding",
		Remark:   "自动分组：向量 / 重排模型",
		Strategy: config.GroupStrategyFirstAvailable,
		Priority: 50,
		Match: func(name string) bool {
			return containsAny(name, "embedding", "embed", "bge-", "gte-", "rerank",
				"jina-embed", "text-embedding")
		},
	},
	{
		Name:     "chat",
		Remark:   "自动分组：通用对话模型（兜底大类）",
		Strategy: config.GroupStrategyFirstAvailable,
		Priority: 10,
		Match: func(name string) bool {
			return !isNonChatModel(name)
		},
	},
}

// isNonChatModel 判断一个模型是否不属于「对话类」，例如向量、重排、
// 语音、图像生成、审核等。这些模型不应进入 chat / cheap 等对话兜底组。
func isNonChatModel(name string) bool {
	return containsAny(name, "embedding", "embed", "bge-", "gte-", "rerank",
		"jina-embed", "text-embedding", "tts", "whisper", "audio", "speech",
		"moderation", "dall-e", "stable-diffusion", "image-1", "sora", "veo",
		"midjourney", "flux", "voice", "asr", "-tts", "cosyvoice", "sambert")
}

// maxAutoGroupsPerModel 单个模型最多进入的分类组数量（不含 auto-all）
const maxAutoGroupsPerModel = 3

// AutoResult 描述一次自动建组的结果
type AutoResult struct {
	// Created 新建的组
	Created []*model.ModelGroup `json:"created"`
	// Updated 已存在被刷新成员的组
	Updated []*model.ModelGroup `json:"updated"`
	// Skipped 因为没有可用模型而被跳过的组名
	Skipped []string `json:"skipped"`
	// Models 参与分组的可用模型总数
	Models int `json:"models"`
	// GroupNames 最终可用的逻辑组名（推荐给用户勾选）
	GroupNames []string `json:"group_names"`
}

// AutoGroups 按当前分组下真实可用的模型自动建组 / 刷新组。
//
// userGroup 为空时使用默认分组。dryRun 为 true 时只计算结果不落库。
func AutoGroups(ctx context.Context, userGroup string, dryRun bool) (*AutoResult, error) {
	if userGroup == "" {
		userGroup = "default"
	}
	available, err := listUsableModels(ctx, userGroup)
	if err != nil {
		return nil, err
	}

	result := &AutoResult{
		Created:    make([]*model.ModelGroup, 0),
		Updated:    make([]*model.ModelGroup, 0),
		Skipped:    make([]string, 0),
		Models:     len(available),
		GroupNames: make([]string, 0),
	}

	assign := classifyModels(available)
	existing, err := model.GetAllModelGroups()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*model.ModelGroup, len(existing))
	for _, g := range existing {
		if g != nil {
			byName[strings.ToLower(g.GroupName)] = g
		}
	}

	// 1) 分类组
	for _, rule := range autoGroupRules {
		members := assign[rule.Name]
		if len(members) == 0 {
			result.Skipped = append(result.Skipped, rule.groupName())
			continue
		}
		group, isNew, err := upsertAutoGroup(rule, members, byName, dryRun)
		if err != nil {
			return nil, err
		}
		if group == nil {
			continue
		}
		if isNew {
			result.Created = append(result.Created, group)
		} else {
			result.Updated = append(result.Updated, group)
		}
		result.GroupNames = append(result.GroupNames, group.GroupName)
	}

	// 2) auto-all：全量兜底组，用户勾一个名字就能用上所有可用模型
	if len(available) > 0 {
		allRule := autoGroupRule{
			Name:     "all",
			Remark:   "自动分组：当前分组下全部可用模型（全量兜底）",
			Strategy: config.GroupStrategyRoundRobin,
			Priority: 0,
		}
		group, isNew, err := upsertAutoGroup(allRule, available, byName, dryRun)
		if err != nil {
			return nil, err
		}
		if group != nil {
			if isNew {
				result.Created = append(result.Created, group)
			} else {
				result.Updated = append(result.Updated, group)
			}
			result.GroupNames = append(result.GroupNames, group.GroupName)
		}
	}

	sort.Strings(result.GroupNames)
	if !dryRun {
		if err := Refresh(); err != nil {
			logger.SysError("failed to refresh model group cache: " + err.Error())
		}
	}
	return result, nil
}

// groupName 返回带前缀的完整组名
func (r autoGroupRule) groupName() string {
	return AutoGroupPrefix + r.Name
}

// upsertAutoGroup 建组或刷新成员。已存在同名组时只更新成员与说明，
// 保留用户对该组策略 / 启用状态的修改（仅当策略为空才补默认值）。
func upsertAutoGroup(rule autoGroupRule, members []string, byName map[string]*model.ModelGroup, dryRun bool) (*model.ModelGroup, bool, error) {
	name := rule.groupName()
	sort.Strings(members)

	var group *model.ModelGroup
	if existing, ok := byName[strings.ToLower(name)]; ok {
		group = existing
	} else {
		group = &model.ModelGroup{
			GroupName: name,
			Strategy:  rule.Strategy,
			Enabled:   true,
			Remark:    rule.Remark,
		}
	}
	if group.Remark == "" || strings.HasPrefix(group.Remark, "自动分组") {
		group.Remark = rule.Remark
	}
	if strings.TrimSpace(group.Strategy) == "" {
		group.Strategy = rule.Strategy
	}

	isNew := group.Id == 0
	if dryRun {
		group.Members = membersToGroupMembers(name, members)
		return group, isNew, nil
	}

	if isNew {
		if err := model.CreateModelGroup(group); err != nil {
			return nil, false, err
		}
	} else {
		if err := model.UpdateModelGroup(group); err != nil {
			return nil, false, err
		}
	}

	// 成员全量对齐：先算出现有成员差集，再逐个 upsert / 删除
	existingMembers, err := model.GetAllGroupMembers(name)
	if err != nil {
		return nil, false, err
	}
	want := make(map[string]bool, len(members))
	for _, m := range members {
		want[m] = true
	}
	have := make(map[string]bool, len(existingMembers))
	for _, m := range existingMembers {
		have[m.ModelName] = true
		if !want[m.ModelName] {
			if err := model.DeleteGroupMember(m.Id); err != nil {
				return nil, false, err
			}
		}
	}
	for _, m := range members {
		if have[m] {
			continue
		}
		member := &model.ModelGroupMember{
			GroupName: name,
			ModelName: m,
			Weight:    1,
			Priority:  autoMemberPriority(m),
			Enabled:   true,
		}
		if err := model.AddGroupMember(member); err != nil {
			return nil, false, err
		}
	}
	group.Members = membersToGroupMembers(name, members)
	return group, isNew, nil
}

// membersToGroupMembers 把模型名列表包装成组成员对象，用于 dryRun 与返回值展示
func membersToGroupMembers(groupName string, names []string) []*model.ModelGroupMember {
	out := make([]*model.ModelGroupMember, 0, len(names))
	for _, n := range names {
		out = append(out, &model.ModelGroupMember{
			GroupName: groupName,
			ModelName: n,
			Weight:    1,
			Priority:  autoMemberPriority(n),
			Enabled:   true,
		})
	}
	return out
}

// autoMemberPriority 给成员一个稳定的组内优先级：越「大」的模型越优先。
//
// 数值本身没有外部语义，只用于让 capability / first_available 策略
// 在同组内有一个稳定且合理的偏好顺序。
//
// 注意判定顺序：轻量后缀（mini / flash / haiku / air …）必须先于
// 大模型关键词判断，否则 "gpt-4o-mini" 会因为命中 "gpt-4o" 而被误判成大模型。
func autoMemberPriority(modelName string) int {
	name := strings.ToLower(modelName)
	switch {
	// 1) 轻量级最先判，避免被子串误伤（用词元匹配防止 "mini" 命中 "gemini"）
	case hasTierToken(name, "mini", "flash", "haiku", "nano", "air", "lite",
		"turbo", "small", "0.5b", "1.5b", "3b", "4b", "7b", "8b", "tiny"):
		return 10
	// 2) 旗舰
	case containsAny(name, "opus", "gpt-5", "-o3", "o3-", "gemini-2.5-pro", "grok-4",
		"gemini-3", "-max", "ultra"):
		return 50
	// 3) 主力
	case containsAny(name, "sonnet", "gpt-4.1", "gpt-4o", "o4", "gemini-2",
		"glm-4.6", "glm-4.5", "deepseek-v3", "kimi-k2"):
		return 40
	// 4) 中等
	case containsAny(name, "pro", "plus", "large", "72b", "70b", "32b", "30b", "14b"):
		return 30
	default:
		return 20
	}
}

// classifyModels 把可用模型按规则归类，返回 组名 -> 模型名列表。
//
// 一个模型最多进入 maxAutoGroupsPerModel 个「专」组；未被任何专组命中的
// 模型只会留在 auto-chat 与 auto-all 里。
func classifyModels(models []string) map[string][]string {
	out := make(map[string][]string, len(autoGroupRules))
	// 先按规则优先级排序，保证「专」组优先占位
	rules := make([]autoGroupRule, len(autoGroupRules))
	copy(rules, autoGroupRules)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority > rules[j].Priority })

	hits := make(map[string]int, len(models))
	for _, rule := range rules {
		if rule.Match == nil {
			continue
		}
		name := rule.Name
		for _, m := range models {
			if !rule.Match(m) {
				continue
			}
			// chat 是兜底大类，不占用模型的专组名额
			if name != "chat" && hits[m] >= maxAutoGroupsPerModel {
				continue
			}
			out[name] = append(out[name], m)
			if name != "chat" {
				hits[m]++
			}
		}
	}
	return out
}

// listUsableModels 返回某分组下真正有启用渠道的模型名（已排序去重）
func listUsableModels(ctx context.Context, userGroup string) ([]string, error) {
	models, err := model.CacheGetGroupModels(ctx, userGroup)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		if !hasChannel(userGroup, m) {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// hasChannel 判断某模型在当前分组下是否有可用渠道
func hasChannel(group string, modelName string) bool {
	channels, err := model.GetChannelsForModel(group, modelName)
	return err == nil && len(channels) > 0
}

// containsAny 判断 name 是否包含任意一个关键词（不区分大小写）
func containsAny(name string, keywords ...string) bool {
	lower := strings.ToLower(name)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// hasTierToken 判断型号串里是否出现了某个「档位标记」词。
//
// 直接用 strings.Contains 会被子串误伤：例如 "mini" 会命中 "gemini"、
// "air" 会命中 "repair"。因此这里要求在匹配位置左右都不是字母/数字，
// 即该标记必须是一个独立的词元（如 gpt-4o-mini、qwen-air、glm-4-flash）。
func hasTierToken(name string, keywords ...string) bool {
	lower := strings.ToLower(name)
	for _, k := range keywords {
		idx := 0
		for {
			pos := strings.Index(lower[idx:], k)
			if pos < 0 {
				break
			}
			start := idx + pos
			end := start + len(k)
			leftOK := start == 0 || !isWordByte(lower[start-1])
			rightOK := end == len(lower) || !isWordByte(lower[end])
			if leftOK && rightOK {
				return true
			}
			idx = start + 1
			if idx >= len(lower) {
				break
			}
		}
	}
	return false
}

// isWordByte 判断是否属于「词元字符」（字母或数字）
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// IsAutoGroup 判断一个组名是否由自动建组生成
func IsAutoGroup(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), AutoGroupPrefix)
}

// Names 返回当前缓存中全部逻辑组名（含自动组与手工组）
func Names() []string {
	groups := All()
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if g == nil || g.GroupName == "" || !g.Enabled {
			continue
		}
		out = append(out, g.GroupName)
	}
	sort.Strings(out)
	return out
}
