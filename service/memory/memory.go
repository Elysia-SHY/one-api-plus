// Package memory 为 Agent 提供短期 / 长期记忆存储。
//
// 短期（short）：绑定 session，滑动窗口保留最近 N 条，超量自动裁剪。
// 长期（long）：跨 session 保留，带关键词索引，供 Agent 在开场时进行检索注入。
// 画像（profile）：用户 / Agent 的固定事实（偏好、项目约定），可被覆盖更新。
//
// 记忆只做「存与取」，不做向量召回——网关场景下关键词 + 时间排序已经够用，
// 并且避免引入额外的嵌入依赖和延迟。需要语义检索时可以把 content 交给
// 上游 embedding 模型自行处理。
package memory

import (
	"strings"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
)

// Scope 取值
const (
	ScopeShort   = "short"
	ScopeLong    = "long"
	ScopeProfile = "profile"
)

// Item 是外部可见的一条记忆
type Item struct {
	Id        int    `json:"id"`
	SessionId string `json:"session_id"`
	Scope     string `json:"scope"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Keywords  string `json:"keywords"`
	CreatedAt int64  `json:"created_at"`
}

func Enabled() bool {
	return config.AgentGatewayEnabled && config.MemoryEnabled
}

// Save 写入一条记忆，写完后按上限裁剪；返回裁剪掉的数量
func Save(userId int, sessionId string, scope string, role string, content string) (int64, error) {
	if !Enabled() {
		return 0, nil
	}
	if strings.TrimSpace(content) == "" {
		return 0, nil
	}
	if sessionId == "" {
		sessionId = "default"
	}
	scope = normalizeScope(scope)
	if len(content) > config.MemoryMaxChars {
		content = content[:config.MemoryMaxChars]
	}
	record := &model.MemoryRecord{
		UserId:      userId,
		SessionId:   sessionId,
		Scope:       scope,
		Role:        roleOrUser(role),
		Content:     content,
		Keywords:    ExtractKeywords(content),
		Tokens:      EstimateTokens(content),
		CreatedTime: time.Now().Unix(),
	}
	if err := model.AddMemory(record); err != nil {
		return 0, err
	}
	// 短期记忆按 max items 滚动裁剪；长期与画像不参与滚动
	if scope != ScopeShort {
		return 0, nil
	}
	if err := model.TrimMemory(userId, sessionId, config.MemoryMaxItems); err != nil {
		logger.SysError("failed to trim short memory: " + err.Error())
		return 0, nil
	}
	current, err := model.CountMemory(userId, sessionId)
	if err != nil {
		return 0, nil
	}
	if current > int64(config.MemoryMaxItems) {
		return current - int64(config.MemoryMaxItems), nil
	}
	return 0, nil
}

// List 读取记忆，返回按时间正序（便于直接拼进 prompt）
func List(userId int, sessionId string, scope string, sinceSec int64, limit int) ([]Item, error) {
	if !Enabled() {
		return nil, nil
	}
	var after int64
	if sinceSec > 0 {
		after = time.Now().Unix() - sinceSec
	}
	records, err := model.ListMemory(userId, sessionId, scope, after, limit)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(records))
	// 查询返回的是 id desc，这里翻转成时间正序
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		items = append(items, Item{
			Id:        r.Id,
			SessionId: r.SessionId,
			Scope:     r.Scope,
			Role:      r.Role,
			Content:   r.Content,
			Keywords:  r.Keywords,
			CreatedAt: r.CreatedTime,
		})
	}
	return items, nil
}

// Search 在长期记忆里做关键词检索
func Search(userId int, query string, limit int) ([]Item, error) {
	if !Enabled() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	records, err := model.ListMemory(userId, "", ScopeLong, 0, 500)
	if err != nil {
		return nil, err
	}
	terms := strings.Fields(strings.ToLower(query))
	scored := make([]struct {
		item  Item
		score int
	}, 0, len(records))
	for _, r := range records {
		haystack := strings.ToLower(r.Content + " " + r.Keywords)
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		scored = append(scored, struct {
			item  Item
			score int
		}{Item{
			Id:        r.Id,
			SessionId: r.SessionId,
			Scope:     r.Scope,
			Role:      r.Role,
			Content:   r.Content,
			Keywords:  r.Keywords,
			CreatedAt: r.CreatedTime,
		}, score})
	}
	// 命中词数多的排前面；同分则新的优先
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0; j-- {
			if scored[j].score > scored[j-1].score ||
				(scored[j].score == scored[j-1].score && scored[j].item.CreatedAt > scored[j-1].item.CreatedAt) {
				scored[j], scored[j-1] = scored[j-1], scored[j]
			}
		}
	}
	out := make([]Item, 0, len(scored))
	for i, s := range scored {
		if i >= limit {
			break
		}
		out = append(out, s.item)
	}
	return out, nil
}

// Clear 清空指定 session（传空则清空该用户全部记忆）
func Clear(userId int, sessionId string) error {
	return model.ClearMemory(userId, sessionId)
}

// Delete 删除单条
func Delete(userId int, id int) error {
	return model.DeleteMemory(id, userId)
}

// Digest 把记忆压成一段可注入 system prompt 的文本
func Digest(items []Item, maxChars int) string {
	if len(items) == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 4096
	}
	lines := make([]string, 0, len(items))
	used := 0
	for _, item := range items {
		line := item.Role + ": " + item.Content
		if used+len(line) > maxChars {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	return strings.Join(lines, "\n")
}

// ExtractKeywords 抽取简易关键词：过滤停用词后取出现频次最高的若干词
func ExtractKeywords(content string) string {
	lower := strings.ToLower(content)
	var tokens []string
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			tokens = append(tokens, string(buf))
			buf = buf[:0]
		}
	}
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 0x2E80 {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()
	freq := make(map[string]int)
	for _, t := range tokens {
		if len(t) < 2 || isStopWord(t) {
			continue
		}
		freq[t]++
	}
	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(freq))
	for k, v := range freq {
		list = append(list, kv{k, v})
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].v > list[j-1].v; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	out := make([]string, 0, 8)
	for i, k := range list {
		if i >= 8 {
			break
		}
		out = append(out, k.k)
	}
	return strings.Join(out, ",")
}

// EstimateTokens 粗估 token 数
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var cjk, ascii int
	for _, r := range text {
		if r > 0x2E80 {
			cjk++
		} else {
			ascii++
		}
	}
	return int(float64(cjk)*0.6) + ascii/4
}

func normalizeScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case ScopeLong, ScopeProfile:
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return ScopeShort
	}
}

func roleOrUser(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "user", "assistant", "system", "tool":
		return role
	default:
		return "user"
	}
}

func isStopWord(word string) bool {
	stop := map[string]bool{
		"the": true, "is": true, "a": true, "an": true, "to": true, "of": true, "and": true,
		"in": true, "for": true, "on": true, "with": true, "that": true, "this": true,
		"it": true, "be": true, "as": true, "at": true, "by": true, "or": true, "are": true,
		"的": true, "了": true, "是": true, "在": true, "和": true, "有": true, "就": true,
		"不": true, "我": true, "你": true, "他": true, "这": true, "那": true,
	}
	return stop[word]
}
