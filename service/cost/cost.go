// Package cost 提供模型定价、请求前成本预测与用户预算控制。
package cost

import (
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
	billingratio "github.com/Elysia-SHY/one-api-plus/relay/billing/ratio"
)

const cacheTTL = 60 * time.Second

var (
	priceCache   map[string]model.ModelPrice
	priceCacheAt time.Time
	priceLock    sync.RWMutex
)

func loadPrices() map[string]model.ModelPrice {
	priceLock.RLock()
	if priceCache != nil && time.Since(priceCacheAt) < cacheTTL {
		defer priceLock.RUnlock()
		return priceCache
	}
	priceLock.RUnlock()

	items, err := model.GetAllPrices()
	if err != nil {
		logger.SysError("failed to load model prices: " + err.Error())
		items = nil
	}
	next := make(map[string]model.ModelPrice, len(items))
	for _, item := range items {
		next[item.ModelName] = *item
	}
	priceLock.Lock()
	priceCache = next
	priceCacheAt = time.Now()
	priceLock.Unlock()
	return next
}

// InvalidatePriceCache 让定价缓存立即失效（后台改价后调用）
func InvalidatePriceCache() {
	priceLock.Lock()
	priceCache = nil
	priceCacheAt = time.Time{}
	priceLock.Unlock()
}

// GetPrice 返回模型每 1M tokens 的输入/输出价格（USD）。
// 优先使用数据库中的自定义定价，其次回落到系统内置的倍率表换算。
func GetPrice(modelName string, channelType int) (inputPrice float64, outputPrice float64, fromDB bool) {
	prices := loadPrices()
	if p, ok := prices[modelName]; ok {
		return p.InputPrice, p.OutputPrice, true
	}
	ratio := billingratio.GetModelRatio(modelName, channelType)
	perMillion := ratio * 1e6 / config.QuotaPerUnit
	return perMillion, perMillion * billingratio.GetCompletionRatio(modelName, channelType), false
}

// Estimate 请求前的成本预测，返回 USD
func Estimate(modelName string, promptTokens int, completionTokens int, channelType int) float64 {
	input, output, _ := GetPrice(modelName, channelType)
	cost := float64(promptTokens)/1e6*input + float64(completionTokens)/1e6*output
	return cost
}

// EstimateQuota 请求前的成本预测，返回额度点（quota）
func EstimateQuota(modelName string, promptTokens int, completionTokens int, channelType int) int64 {
	usd := Estimate(modelName, promptTokens, completionTokens, channelType)
	return int64(usd * config.QuotaPerUnit)
}

// CheckBudget 判断用户是否仍有预算，返回是否放行与原因
func CheckBudget(userId int, predictedQuota int64) (bool, string) {
	if !config.BudgetEnabled {
		return true, ""
	}
	budget, err := model.GetUserBudget(userId)
	if err != nil {
		logger.SysError("failed to load user budget: " + err.Error())
		return true, ""
	}
	now := time.Now()
	if budget.DailyLimit > 0 {
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		used, err := model.GetUserPeriodQuota(userId, startOfDay)
		if err == nil && used+predictedQuota > budget.DailyLimit {
			return false, "已达到今日预算上限"
		}
	}
	if budget.MonthlyLimit > 0 {
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
		used, err := model.GetUserPeriodQuota(userId, startOfMonth)
		if err == nil && used+predictedQuota > budget.MonthlyLimit {
			return false, "已达到本月预算上限"
		}
	}
	return true, ""
}

// UserStat 返回用户最近 N 天的成本统计
func UserStat(userId int, days int) ([]*model.ModelCostStat, int64, error) {
	return model.GetUserCostStat(userId, days)
}
