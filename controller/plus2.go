package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/service/capability"
	"github.com/Elysia-SHY/one-api-plus/service/dashboard"
	"github.com/Elysia-SHY/one-api-plus/service/mcp"
	"github.com/Elysia-SHY/one-api-plus/service/memory"
	"github.com/Elysia-SHY/one-api-plus/service/modelgroup"
	"github.com/Elysia-SHY/one-api-plus/service/ratelimit"
	"github.com/Elysia-SHY/one-api-plus/service/routing"
)

// mustMarshal 序列化配置对象；Dashboard 接口里仅用于回显，失败时给出空对象
func mustMarshal(v interface{}) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return out
}

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

// GetDashboard 返回面板聚合数据
func GetDashboard(c *gin.Context) {
	if !config.DashboardEnabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "仪表盘未启用"})
		return
	}
	userId := c.GetInt(ctxkey.Id)
	if c.GetInt(ctxkey.Role) >= model.RoleAdminUser {
		// 管理员可以看全站
		if raw := c.Query("user_id"); raw != "" {
			userId, _ = strconv.Atoi(raw)
		} else {
			userId = 0
		}
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	data, err := dashboard.Build(userId, days)
	if err != nil {
		logger.Errorf(c.Request.Context(), "failed to build dashboard: %s", err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data, "message": ""})
}

// GetLoadSnapshot 返回各渠道实时并发负载
func GetLoadSnapshot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"channels": routing.Snapshot(),
			"total":    routing.Total(),
		},
	})
}

// GetRoutingWeights 返回当前路由评分四维权重
func GetRoutingWeights(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"strategy":  config.RoutingStrategy,
			"weights":   config.GetRoutingWeights(),
			"loadAware": config.RoutingLoadAware,
			"maxLoad":   config.RoutingMaxLoad,
		},
	})
}

// UpdateRoutingWeights 在线更新路由权重
func UpdateRoutingWeights(c *gin.Context) {
	var payload struct {
		Weights  config.RoutingScoreWeights `json:"weights"`
		Raw      string                     `json:"raw"`
		Strategy string                     `json:"strategy"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(payload.Raw) != "" {
		if err := config.SetRoutingWeights(payload.Raw); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "ROUTING_WEIGHTS 解析失败：" + err.Error()})
			return
		}
	} else {
		w := payload.Weights
		if w.Cost == 0 && w.Latency == 0 && w.Load == 0 && w.Stability == 0 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "请至少提供一个非零权重"})
			return
		}
		total := w.Cost + w.Latency + w.Load + w.Stability
		w.Cost /= total
		w.Latency /= total
		w.Load /= total
		w.Stability /= total
		if err := config.SetRoutingWeights(string(mustMarshal(w))); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	if payload.Strategy != "" {
		config.RoutingStrategy = payload.Strategy
		model.UpdateOption("RoutingStrategy", payload.Strategy)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已更新",
		"data": gin.H{
			"strategy": routing.Strategy(),
			"weights":  config.GetRoutingWeights(),
		},
	})
}

// ---------------------------------------------------------------------------
// 模型能力数据库
// ---------------------------------------------------------------------------

// GetCapabilities 列出全部已登记的模型能力
func GetCapabilities(c *gin.Context) {
	items, err := model.GetAllCapabilities()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// SearchCapabilities 按能力筛选模型
func SearchCapabilities(c *gin.Context) {
	ctxLen, _ := strconv.Atoi(c.DefaultQuery("context", "0"))
	vision := c.Query("vision") == "true"
	toolCall := c.Query("tool_call") == "true"
	reasoning := c.Query("reasoning") == "true"
	embedding := c.Query("embedding") == "true"
	items, err := model.SearchCapabilities(ctxLen, vision, toolCall, reasoning, embedding)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// GetCapability 查询单个模型的能力画像（未登记时用规则推断返回，但不落库）
func GetCapability(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "模型名不能为空"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": capability.Get(name)})
}

// RefreshCapability 对模型重建能力记录 force=1 时用规则结论覆盖已有值
func RefreshCapability(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "模型名不能为空"})
		return
	}
	item, err := model.GetCapability(name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	inferred := capability.Infer(name)
	if inferred == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无法推断该模型能力"})
		return
	}
	if c.Query("force") != "1" && item != nil && item.ContextLength > 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "已存在能力记录，未覆盖", "data": item})
		return
	}
	if err = model.UpsertCapability(inferred, false); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err = capability.Refresh(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已刷新", "data": inferred})
}

// UpdateCapability 人工写入/覆盖一条能力记录
func UpdateCapability(c *gin.Context) {
	var item model.ModelCapability
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if item.ModelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "model_name 不能为空"})
		return
	}
	if err := capability.Set(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已保存"})
}

// ---------------------------------------------------------------------------
// 模型组
// ---------------------------------------------------------------------------

// GetModelGroups 列出全部模型组（含成员）
func GetModelGroups(c *gin.Context) {
	groups, err := model.GetAllModelGroups()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": groups})
}

// AddModelGroup 新建模型组
func AddModelGroup(c *gin.Context) {
	var group model.ModelGroup
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.CreateModelGroup(&group); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := modelgroup.Refresh(); err != nil {
		logger.SysError("failed to refresh model group cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已创建", "data": group})
}

// UpdateModelGroup 修改模型组（名称/策略/启用）
func UpdateModelGroup(c *gin.Context) {
	var group model.ModelGroup
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if group.Id == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少 id"})
		return
	}
	if err := model.UpdateModelGroup(&group); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := modelgroup.Refresh(); err != nil {
		logger.SysError("failed to refresh model group cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已更新"})
}

// RemoveModelGroup 删除模型组及其成员
func RemoveModelGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的 id"})
		return
	}
	if err = model.DeleteModelGroup(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err = modelgroup.Refresh(); err != nil {
		logger.SysError("failed to refresh model group cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}

// AddGroupMember 添加或更新组成员
func AddGroupMember(c *gin.Context) {
	var member model.ModelGroupMember
	if err := c.ShouldBindJSON(&member); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.AddGroupMember(&member); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := modelgroup.Refresh(); err != nil {
		logger.SysError("failed to refresh model group cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已保存"})
}

// RemoveGroupMember 删除组成员
func RemoveGroupMember(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的 id"})
		return
	}
	if err = model.DeleteGroupMember(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err = modelgroup.Refresh(); err != nil {
		logger.SysError("failed to refresh model group cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}

// ---------------------------------------------------------------------------
// MCP 网关
// ---------------------------------------------------------------------------

// GetMCPServers 列出 MCP 服务器及其工具
func GetMCPServers(c *gin.Context) {
	if err := mcp.Load(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	items, err := model.GetAllMCPServers()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"servers": mcp.ListServers(),
			"db":      items,
			"tools":   mcp.Tools(),
		},
	})
}

// UpsertMCPServer 新增或更新一个 MCP 服务器
func UpsertMCPServer(c *gin.Context) {
	var server model.MCPServer
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if server.Name == "" || server.URL == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "name 与 url 不能为空"})
		return
	}
	if err := model.UpsertMCPServer(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := mcp.Load(); err != nil {
		logger.SysError("failed to reload mcp servers: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已保存"})
}

// RemoveMCPServer 删除 MCP 服务器
func RemoveMCPServer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的 id"})
		return
	}
	if err = model.DeleteMCPServer(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err = mcp.Load(); err != nil {
		logger.SysError("failed to reload mcp servers: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}

// DiscoverMCPTools 触发一次工具发现
func DiscoverMCPTools(c *gin.Context) {
	if !mcp.Enabled() {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "MCP 网关未启用，请设置 AGENT_GATEWAY_ENABLED 与 MCP_ENABLED"})
		return
	}
	total := mcp.Discover()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("已发现 %d 个工具", total),
		"data":    mcp.Tools(),
	})
}

// CallMCPTool 直接调用一个 MCP 工具
func CallMCPTool(c *gin.Context) {
	var payload struct {
		Tool      string `json:"tool"`
		Arguments string `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.Tool == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "tool 不能为空"})
		return
	}
	result, err := mcp.Call(payload.Tool, payload.Arguments)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

// ---------------------------------------------------------------------------
// Agent Memory
// ---------------------------------------------------------------------------

// GetMemory 读取当前用户的记忆
func GetMemory(c *gin.Context) {
	if !memory.Enabled() {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "记忆功能未启用"})
		return
	}
	userId := c.GetInt(ctxkey.Id)
	sessionId := c.Query("session_id")
	scope := c.Query("scope")
	withinSec, _ := strconv.ParseInt(c.DefaultQuery("within", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := memory.List(userId, sessionId, scope, withinSec, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// AddMemory 写入一条记忆
func AddMemory(c *gin.Context) {
	if !memory.Enabled() {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "记忆功能未启用"})
		return
	}
	var payload struct {
		SessionId string `json:"session_id"`
		Scope     string `json:"scope"`
		Role      string `json:"role"`
		Content   string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	trimmed, err := memory.Save(c.GetInt(ctxkey.Id), payload.SessionId, payload.Scope, payload.Role, payload.Content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已保存，裁剪 %d 条", trimmed)})
}

// DeleteMemory 删除一条记忆
func DeleteMemory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的 id"})
		return
	}
	if err = memory.Delete(c.GetInt(ctxkey.Id), id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}

// ClearMemory 清空记忆
func ClearMemory(c *gin.Context) {
	sessionId := c.Query("session_id")
	if err := memory.Clear(c.GetInt(ctxkey.Id), sessionId); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已清空"})
}

// SearchMemory 在长期记忆里做关键词检索
func SearchMemory(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少查询词"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := memory.Search(c.GetInt(ctxkey.Id), query, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// ---------------------------------------------------------------------------
// Responses API 管理
// ---------------------------------------------------------------------------

// GetResponsesStatus 返回 Responses 兼容层的状态
func GetResponsesStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"enabled":     config.ResponsesAPIEnabled,
			"store_items": config.ResponsesStoreItems,
			"endpoint":    "/v1/responses",
		},
	})
}

// ---------------------------------------------------------------------------
// 限流状态
// ---------------------------------------------------------------------------

// GetRateLimitStatus 返回限流配置与当前占用
func GetRateLimitStatus(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	key := fmt.Sprintf("conc:user:%d", userId)
	if tokenId := c.GetInt(ctxkey.TokenId); tokenId > 0 {
		key = fmt.Sprintf("conc:token:%d", tokenId)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"enabled":    config.RateLimitEnabled,
			"qpm":        config.RateLimitQPM,
			"burst":      config.RateLimitBurst,
			"per_model":  config.RateLimitPerModel,
			"concurrent": config.RateLimitConcurrent,
			"in_flight":  ratelimit.ConcurrencyOf(key),
		},
	})
}
