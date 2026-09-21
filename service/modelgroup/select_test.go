package modelgroup

import (
	"strings"
	"testing"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/model"
)

// injectGroupCache 在测试里绕过数据库，直接往组缓存塞一个组。
//
// 生产的组缓存由 Refresh() 从 DB 加载；这里只验证「分类 → 选路」这段纯逻辑，
// 不引入 SQLite（本机 CGO 不可用，跑不了真实 DB）。
func injectGroupCache(name string, g *model.ModelGroup) {
	mu.Lock()
	if cache == nil {
		cache = make(map[string]*model.ModelGroup)
	}
	cache[strings.ToLower(name)] = g
	cacheAt = time.Now()
	mu.Unlock()
}

// TestSelectFromAutoClassifiedGroup 验证「自动归类 → 建组成员 → 按策略选路」的完整链路。
//
// 这里不碰数据库：手工构造一个 ModelGroup（含成员与权重/优先级），
// 直接注入缓存，然后按真实调用路径 IsGroup → Select 解析出模型名。
func TestSelectFromAutoClassifiedGroup(t *testing.T) {
	// 典型的一家聚合网关下会有的模型清单
	available := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"claude-opus-4-20250514",
		"claude-3-5-haiku-20241022",
		"gemini-2.5-pro",
		"gemini-2.0-flash",
		"deepseek-v3",
		"qwen2.5-vl-72b-instruct",
		"text-embedding-3-small",
	}
	assign := classifyModels(available)

	// 断言归类结果符合直觉
	expect := map[string][]string{
		"vision":    {"gpt-4o", "gemini-2.5-pro", "gemini-2.0-flash", "qwen2.5-vl-72b-instruct", "claude-opus-4-20250514"},
		"cheap":     {"gpt-4o-mini", "gemini-2.0-flash", "claude-3-5-haiku-20241022"},
		"coding":    {"gpt-4o", "claude-opus-4-20250514", "deepseek-v3"},
		"embedding": {"text-embedding-3-small"},
	}
	for group, wants := range expect {
		got := assign[group]
		for _, w := range wants {
			if !containsStr(got, w) {
				t.Errorf("auto group %q 应包含 %q，实际=%v", group, w, got)
			}
		}
	}
	// chat 不应包含 embedding
	if containsStr(assign["chat"], "text-embedding-3-small") {
		t.Errorf("chat 不应包含 embedding 模型，实际=%v", assign["chat"])
	}
	// cheap 不应包含 embedding
	if containsStr(assign["cheap"], "text-embedding-3-small") {
		t.Errorf("cheap 不应包含 embedding 模型，实际=%v", assign["cheap"])
	}
	// gemini-2.5-pro 不该被当成廉价模型
	if containsStr(assign["cheap"], "gemini-2.5-pro") {
		t.Errorf("cheap 不应包含 gemini-2.5-pro（mini 子串误伤），实际=%v", assign["cheap"])
	}

	// 用分类结果构造一个逻辑组，注入缓存后走 Select 选路
	groupName := "auto-coding"
	group := &model.ModelGroup{
		Id:        1,
		GroupName: groupName,
		Strategy:  config.GroupStrategyFirstAvailable,
		Enabled:   true,
	}
	for _, m := range assign["coding"] {
		group.Members = append(group.Members, &model.ModelGroupMember{
			GroupName: groupName,
			ModelName: m,
			Weight:    1,
			Priority:  autoMemberPriority(m),
			Enabled:   true,
		})
	}

	oldEnabled := config.ModelGroupEnabled
	config.ModelGroupEnabled = true
	defer func() { config.ModelGroupEnabled = oldEnabled }()

	injectGroupCache(groupName, group)

	if !IsGroup(groupName) {
		t.Fatalf("IsGroup(%q) 应为 true", groupName)
	}
	selected, ok := Select(groupName, SelectOption{})
	if !ok || selected == "" {
		t.Fatalf("Select(%q) 失败", groupName)
	}
	if !containsStr(assign["coding"], selected) {
		t.Errorf("Select 返回 %q，不在 coding 组成员 %v 中", selected, assign["coding"])
	}
	t.Logf("auto-coding 解析结果: %s（成员 %v）", selected, assign["coding"])

	// 带能力要求时应能过滤：要求视觉时，纯代码模型（deepseek-v3）不该被选中
	// 这里仅验证 Select 在 Available 约束下仍能返回成员之一
	selected2, ok2 := Select(groupName, SelectOption{
		Available: func(name string) bool { return name == "deepseek-v3" },
	})
	if !ok2 || selected2 != "deepseek-v3" {
		t.Errorf("带 Available 约束时 Select 应返回 deepseek-v3，实际 %q ok=%v", selected2, ok2)
	}

	// Resolve：非组名原样返回
	if out, isGroup := Resolve("gpt-4o", SelectOption{}); isGroup || out != "gpt-4o" {
		t.Errorf("Resolve(非组名) 应原样返回，实际 out=%q isGroup=%v", out, isGroup)
	}
	// Resolve：组名解析成真实模型
	if out, isGroup := Resolve(groupName, SelectOption{}); !isGroup || out == groupName {
		t.Errorf("Resolve(%q) 应解析为真实模型名，实际 out=%q isGroup=%v", groupName, out, isGroup)
	}
}

// TestNamesReturnsEnabledOnly 验证 Names() 会过滤掉禁用组与空名组
func TestNamesReturnsEnabledOnly(t *testing.T) {
	injectGroupCache("auto-on", &model.ModelGroup{GroupName: "auto-on", Enabled: true, Members: []*model.ModelGroupMember{{ModelName: "x"}}})
	injectGroupCache("auto-off", &model.ModelGroup{GroupName: "auto-off", Enabled: false, Members: []*model.ModelGroupMember{{ModelName: "y"}}})

	names := Names()
	if !containsStr(names, "auto-on") {
		t.Errorf("Names() 应包含 auto-on，实际=%v", names)
	}
	if containsStr(names, "auto-off") {
		t.Errorf("Names() 不应包含禁用的 auto-off，实际=%v", names)
	}
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
