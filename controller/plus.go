package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/common/helper"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/service/alias"
	"github.com/Elysia-SHY/one-api-plus/service/cost"
	"github.com/Elysia-SHY/one-api-plus/service/health"
	"github.com/Elysia-SHY/one-api-plus/service/modelsync"
	"github.com/Elysia-SHY/one-api-plus/service/routing"
)

// ---------------------------------------------------------------------------
// 总览
// ---------------------------------------------------------------------------

// GetPlusStatus 返回 One API Plus 增强特性的运行状态
func startOfDayUnix() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
}

func startOfMonthUnix() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
}

func GetPlusStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"system_name":      config.SystemName,
			"lite_mode":        config.LiteMode,
			"routing_strategy": routing.Strategy(),
			"fallback_enabled": config.FallbackEnabled,
			"fallback_models":  config.AllFallbackModels(),
			"alias_enabled":    config.AliasEnabled,
			"alias_count":      len(alias.All()),
			"model_sync": gin.H{
				"enabled":     config.ModelSyncEnabled,
				"interval":    config.ModelSyncInterval,
				"auto_enable": config.ModelSyncAutoEnable,
			},
			"health_check": gin.H{
				"enabled":    config.HealthCheckEnabled,
				"interval":   config.HealthCheckInterval,
				"auto_pause": config.HealthCheckAutoPause,
			},
			"budget_enabled": config.BudgetEnabled,
		},
	})
}

// ---------------------------------------------------------------------------
// 模型目录 & 自动同步
// ---------------------------------------------------------------------------

func GetModelCatalog(c *gin.Context) {
	startIdx, _ := strconv.Atoi(c.Query("p"))
	num := config.ItemsPerPage
	if n, err := strconv.Atoi(c.Query("page_size")); err == nil && n > 0 && n <= 100 {
		num = n
	}
	startIdx = startIdx * num
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	status := c.Query("status")
	keyword := c.Query("keyword")
	items, err := model.SearchCatalog(keyword, channelId, status, startIdx, num)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	total, _ := model.CountCatalog(keyword, channelId, status)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items": items,
			"total": total,
		},
	})
}

// SyncModels 立即触发一次模型同步
func SyncModels(c *gin.Context) {
	results, err := modelsync.SyncAll(c.Query("scope"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	added := 0
	for _, r := range results {
		added += len(r.NewModels)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"channels":   results,
			"new_models": added,
		},
	})
}

// SyncChannelModels 同步指定渠道
func SyncChannelModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的渠道 Id"})
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	result := modelsync.SyncChannel(channel)
	c.JSON(http.StatusOK, gin.H{
		"success": result.Error == "",
		"message": result.Error,
		"data":    result,
	})
}

// EnableModel 启用目录中的某个模型（写回渠道 Models 字段）
func EnableModel(c *gin.Context) {
	var req struct {
		ChannelId int    `json:"channel_id"`
		Model     string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if req.ChannelId == 0 || req.Model == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel_id 与 model 不能为空"})
		return
	}
	if err := model.EnableCatalogModel(req.ChannelId, req.Model); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// UpdateCatalogModel 修改目录里模型的状态
func UpdateCatalogModel(c *gin.Context) {
	var req struct {
		ChannelId int    `json:"channel_id"`
		Model     string `json:"model"`
		Status    string `json:"status"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	switch req.Status {
	case config.ModelStatusNormal, config.ModelStatusDegraded, config.ModelStatusPaused, config.ModelStatusInvalid, config.ModelStatusNew:
	default:
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "非法的模型状态"})
		return
	}
	if err := model.UpdateCatalogStatus(req.ChannelId, req.Model, req.Status, req.Enabled); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// ---------------------------------------------------------------------------
// 渠道健康检测
// ---------------------------------------------------------------------------

func GetHealthList(c *gin.Context) {
	items, err := model.GetAllHealth()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

func GetChannelHealth(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的渠道 Id"})
		return
	}
	item, err := model.GetChannelHealth(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": item})
}

// CheckChannels 立即触发一次全量健康检查
func CheckChannels(c *gin.Context) {
	results, err := health.CheckAll()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": results})
}

// ---------------------------------------------------------------------------
// 模型别名
// ---------------------------------------------------------------------------

func GetAliasList(c *gin.Context) {
	items, err := model.GetAllAlias()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

func AddAlias(c *gin.Context) {
	var item model.ModelAlias
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if item.Alias == "" || item.Target == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "别名与目标模型都不能为空"})
		return
	}
	item.Alias = strings.TrimSpace(item.Alias)
	item.Target = strings.TrimSpace(item.Target)
	item.Enabled = true
	item.CreatedTime = helper.GetTimestamp()
	if err := model.CreateAlias(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = alias.Refresh()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": item})
}

func UpdateAlias(c *gin.Context) {
	var item model.ModelAlias
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if item.Id == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少别名 Id"})
		return
	}
	if err := model.UpdateAlias(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = alias.Refresh()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteAlias(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的别名 Id"})
		return
	}
	if err = model.DeleteAlias(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = alias.Refresh()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// ---------------------------------------------------------------------------
// 模型定价
// ---------------------------------------------------------------------------

func GetPriceList(c *gin.Context) {
	items, err := model.GetAllPrices()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

func UpsertPrice(c *gin.Context) {
	var item model.ModelPrice
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if item.ModelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "模型名不能为空"})
		return
	}
	if err := model.UpsertPrice(&item); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	cost.InvalidatePriceCache()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": item})
}

func DeletePrice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的 Id"})
		return
	}
	if err = model.DeletePrice(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	cost.InvalidatePriceCache()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// ---------------------------------------------------------------------------
// 成本统计与预算
// ---------------------------------------------------------------------------

// GetCostStat 返回按模型聚合的成本统计
func GetCostStat(c *gin.Context) {
	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = 7
	}
	userId := c.GetInt(ctxkey.Id)
	if target := c.Query("user_id"); target != "" && c.GetInt(ctxkey.Role) >= model.RoleAdminUser {
		userId, _ = strconv.Atoi(target)
	}
	stats, total, err := cost.UserStat(userId, days)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"days":  days,
			"items": stats,
			"total": total,
			"usd":   float64(total) / config.QuotaPerUnit,
		},
	})
}

// PredictCost 请求前的成本预测
func PredictCost(c *gin.Context) {
	modelName := c.Query("model")
	promptTokens, _ := strconv.Atoi(c.Query("prompt_tokens"))
	completionTokens, _ := strconv.Atoi(c.Query("completion_tokens"))
	if modelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "model 不能为空"})
		return
	}
	if promptTokens <= 0 {
		promptTokens = 1000
	}
	if completionTokens <= 0 {
		completionTokens = 500
	}
	usd := cost.Estimate(modelName, promptTokens, completionTokens, 0)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"model":             modelName,
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"usd":               usd,
			"quota":             cost.EstimateQuota(modelName, promptTokens, completionTokens, 0),
		},
	})
}

// GetBudget 查询预算
func GetBudget(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	if target := c.Query("user_id"); target != "" {
		id, err := strconv.Atoi(target)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的用户 Id"})
			return
		}
		if id != userId && c.GetInt(ctxkey.Role) < model.RoleAdminUser {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "无权查看他人预算"})
			return
		}
		userId = id
	}
	budget, err := model.GetUserBudget(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	now := helper.GetTimestamp()
	dailyUsed, _ := model.GetUserPeriodQuota(userId, startOfDayUnix())
	monthlyUsed, _ := model.GetUserPeriodQuota(userId, startOfMonthUnix())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"budget":       budget,
			"daily_used":   dailyUsed,
			"monthly_used": monthlyUsed,
			"checked_at":   now,
		},
	})
}

// UpdateBudget 设置预算（管理员）
func UpdateBudget(c *gin.Context) {
	var budget model.UserBudget
	if err := c.ShouldBindJSON(&budget); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if budget.UserId == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "user_id 不能为空"})
		return
	}
	if err := model.UpsertUserBudget(&budget); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": budget})
}
