// Package dashboard 为 One API Plus 提供可观测面板的数据聚合层。
//
// 三条主线：
//  1. 概览 —— 今日/近 7 天请求数、用量、 token、平均延迟、失败率
//  2. 分布 —— Top N 模型、Top N 渠道、用户贡献、24 小时趋势
//  3. 健康 —— 渠道健康度分布、失效渠道清单
//
// 所有查询走已有的 Log 表，不新增写入路径，因此对中继链路零侵入。
package dashboard

import (
	"sort"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/model"
)

// Overview 是概览区的数据
type Overview struct {
	Requests         int64   `json:"requests"`
	Quota            int64   `json:"quota"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	AvgElapsedMs     int64   `json:"avg_elapsed_ms"`
	FailRate         float64 `json:"fail_rate"`
	ActiveUsers      int64   `json:"active_users"`
	UpdatedAt        int64   `json:"updated_at"`
}

// ChannelStat 单个渠道的用量统计
type ChannelStat struct {
	ChannelId    int     `json:"channel_id"`
	ChannelName  string  `json:"channel_name"`
	Requests     int64   `json:"requests"`
	Quota        int64   `json:"quota"`
	Tokens       int64   `json:"tokens"`
	AvgElapsedMs int64   `json:"avg_elapsed_ms"`
	Status       string  `json:"status"`
	WeightFactor float64 `json:"weight_factor"`
	Load         int64   `json:"load"`
	LatencyMs    int     `json:"latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
}

// Dashboard 是完整的面板数据
type Dashboard struct {
	Overview    Overview               `json:"overview"`
	Trend       []*model.TrendPoint    `json:"trend"`
	TopModels   []*model.ModelCostStat `json:"top_models"`
	Channels    []*ChannelStat         `json:"channels"`
	Health      map[string]int         `json:"health_distribution"`
	GeneratedAt int64                  `json:"generated_at"`
	Enabled     bool                   `json:"enabled"`
}

// Build 聚合出完整面板数据；userId > 0 时只统计该用户
func Build(userId int, days int) (*Dashboard, error) {
	if days <= 0 {
		days = 7
	}
	if config.DashboardTopN <= 0 {
		config.DashboardTopN = 10
	}
	now := time.Now()
	start := now.AddDate(0, 0, -days+1)
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	result := &Dashboard{
		Overview:    Overview{},
		Trend:       make([]*model.TrendPoint, 0),
		TopModels:   make([]*model.ModelCostStat, 0),
		Channels:    make([]*ChannelStat, 0),
		Health:      map[string]int{},
		GeneratedAt: now.Unix(),
		Enabled:     true,
	}
	// 1) 今日概览
	todayStats, err := querySummary(userId, startOfToday.Unix(), now.Unix()+1)
	if err != nil {
		return nil, err
	}
	result.Overview = *todayStats
	// 2) 趋势：7 天以内按小时，以上按天
	trendStart := start.Unix()
	if days > 1 {
		trendStart = now.AddDate(0, 0, -days+1).Unix()
	}
	trend, err := model.GetRequestTrend(userId, trendStart, now.Unix()+1, days <= 2)
	if err != nil {
		return nil, err
	}
	result.Trend = trend
	// 3) Top N 模型
	models, totalQuota, err := model.GetUserCostStat(userId, days)
	if err != nil {
		return nil, err
	}
	for i, m := range models {
		if i >= config.DashboardTopN {
			break
		}
		result.TopModels = append(result.TopModels, m)
	}
	_ = totalQuota
	// 4) 渠道用量 + 健康度
	channelStats, err := queryChannelStats(userId, now.AddDate(0, 0, -days).Unix(), now.Unix()+1)
	if err != nil {
		return nil, err
	}
	healthList, err := model.GetAllHealth()
	if err != nil {
		return nil, err
	}
	healthByChannel := make(map[int]*model.ChannelHealth, len(healthList))
	distribution := map[string]int{}
	for _, h := range healthList {
		healthByChannel[h.ChannelId] = h
		distribution[h.Status]++
	}
	result.Health = distribution
	result.Channels = decorateChannels(channelStats, healthByChannel)
	sort.SliceStable(result.Channels, func(i, j int) bool {
		return result.Channels[i].Quota > result.Channels[j].Quota
	})
	if len(result.Channels) > config.DashboardTopN {
		result.Channels = result.Channels[:config.DashboardTopN]
	}
	return result, nil
}

// querySummary 汇总一段时间内的核心指标
func querySummary(userId int, start int64, end int64) (*Overview, error) {
	var row struct {
		Requests   int64   `gorm:"column:requests"`
		Quota      int64   `gorm:"column:quota"`
		Prompt     int64   `gorm:"column:prompt_tokens"`
		Completion int64   `gorm:"column:completion_tokens"`
		Elapsed    float64 `gorm:"column:avg_elapsed"`
		Users      int64   `gorm:"column:active_users"`
	}
	query := model.LOG_DB.Model(&model.Log{}).Select(
		"count(*) as requests, coalesce(sum(quota),0) as quota, "+
			"coalesce(sum(prompt_tokens),0) as prompt_tokens, "+
			"coalesce(sum(completion_tokens),0) as completion_tokens, "+
			"coalesce(avg(elapsed_time),0) as avg_elapsed, "+
			"count(distinct user_id) as active_users").
		Where("type = ? and created_at between ? and ?", model.LogTypeConsume, start, end)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if err := query.Scan(&row).Error; err != nil {
		return nil, err
	}
	return &Overview{
		Requests:         row.Requests,
		Quota:            row.Quota,
		PromptTokens:     row.Prompt,
		CompletionTokens: row.Completion,
		AvgElapsedMs:     int64(row.Elapsed),
		ActiveUsers:      row.Users,
		UpdatedAt:        time.Now().Unix(),
	}, nil
}

// queryChannelStats 按渠道聚合用量
func queryChannelStats(userId int, start int64, end int64) ([]*ChannelStat, error) {
	rows := make([]*ChannelStat, 0)
	query := model.LOG_DB.Model(&model.Log{}).Select(
		"channel_id, count(*) as requests, coalesce(sum(quota),0) as quota, "+
			"coalesce(sum(prompt_tokens+completion_tokens),0) as tokens, "+
			"coalesce(avg(elapsed_time),0) as avg_elapsed_ms").
		Where("type = ? and created_at between ? and ?", model.LogTypeConsume, start, end)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	err := query.Group("channel_id").Scan(&rows).Error
	return rows, err
}

// decorateChannels 把渠道用量与健康度合并，并补上渠道名
func decorateChannels(stats []*ChannelStat, healthByChannel map[int]*model.ChannelHealth) []*ChannelStat {
	for _, s := range stats {
		if h, ok := healthByChannel[s.ChannelId]; ok {
			s.Status = h.Status
			s.WeightFactor = h.WeightFactor
			s.LatencyMs = h.LatencyMs
			s.ErrorRate = h.ErrorRate
			s.ChannelName = h.ChannelName
		} else {
			s.Status = config.HealthStatusUnknown
			s.WeightFactor = 0.8
		}
		if s.ChannelName == "" {
			if ch, err := model.GetChannelById(s.ChannelId, true); err == nil && ch != nil {
				s.ChannelName = ch.Name
			}
		}
	}
	return stats
}
