package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/helper"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

// ---------------------------------------------------------------------------
// 模型目录（Model Sync 的产物）
// ---------------------------------------------------------------------------

type ModelCatalog struct {
	Id         int    `json:"id"`
	ChannelId  int    `json:"channel_id" gorm:"index:idx_catalog_channel_model"`
	ModelName  string `json:"model_name" gorm:"index:idx_catalog_channel_model;type:varchar(128)"`
	OwnedBy    string `json:"owned_by"`
	Source     string `json:"source"` // openai / anthropic / gemini / azure ...
	Status     string `json:"status" gorm:"type:varchar(16);default:'new'"`
	Enabled    bool   `json:"enabled" gorm:"default:false"`
	FirstSeen  int64  `json:"first_seen" gorm:"bigint"`
	LastSeen   int64  `json:"last_seen" gorm:"bigint"`
	SyncTime   int64  `json:"sync_time" gorm:"bigint"`
	SyncStatus string `json:"sync_status" gorm:"type:varchar(32);default:''"`

	// 第二阶段：模型能力画像（冗余自 model_capabilities，便于按渠道查看）
	ContextLength int `json:"context_length" gorm:"default:0"`
	MaxOutput     int `json:"max_output" gorm:"default:0"`
	Vision        int `json:"vision" gorm:"default:0"`
	ToolCall      int `json:"tool_call" gorm:"default:0"`
	Reasoning     int `json:"reasoning" gorm:"default:0"`
	Embedding     int `json:"embedding" gorm:"default:0"`
}

// UpsertCatalogModels 把一次同步拿到的模型列表写回目录，返回新增的模型名
func UpsertCatalogModels(channelId int, source string, modelNames []string, defaultStatus string) ([]string, error) {
	now := helper.GetTimestamp()
	existing, err := GetCatalogModels(channelId)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		known[name] = struct{}{}
	}
	newModels := make([]string, 0)
	for _, name := range modelNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		item := ModelCatalog{
			ChannelId:  channelId,
			ModelName:  name,
			Source:     source,
			Status:     defaultStatus,
			Enabled:    true,
			FirstSeen:  now,
			LastSeen:   now,
			SyncTime:   now,
			SyncStatus: "ok",
		}
		if _, ok := known[name]; ok {
			err = DB.Model(&ModelCatalog{}).
				Where("channel_id = ? and model_name = ?", channelId, name).
				Select("last_seen", "sync_time", "sync_status", "source").
				Updates(ModelCatalog{LastSeen: now, SyncTime: now, SyncStatus: "ok", Source: source}).Error
			if err != nil {
				logger.SysError(fmt.Sprintf("failed to refresh catalog model %s: %s", name, err.Error()))
			}
			continue
		}
		if err = DB.Create(&item).Error; err != nil {
			logger.SysError(fmt.Sprintf("failed to insert catalog model %s: %s", name, err.Error()))
			continue
		}
		newModels = append(newModels, name)
	}
	return newModels, nil
}

func GetCatalogModels(channelId int) ([]string, error) {
	var names []string
	err := DB.Model(&ModelCatalog{}).Where("channel_id = ?", channelId).Pluck("model_name", &names).Error
	return names, err
}

func SearchCatalog(keyword string, channelId int, status string, startIdx int, num int) ([]*ModelCatalog, error) {
	var items []*ModelCatalog
	query := DB.Model(&ModelCatalog{})
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("model_name LIKE ?", "%"+keyword+"%")
	}
	err := query.Order("channel_id asc, model_name asc").Limit(num).Offset(startIdx).Find(&items).Error
	return items, err
}

func CountCatalog(keyword string, channelId int, status string) (int64, error) {
	var total int64
	query := DB.Model(&ModelCatalog{})
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("model_name LIKE ?", "%"+keyword+"%")
	}
	err := query.Count(&total).Error
	return total, err
}

func UpdateCatalogStatus(channelId int, modelName string, status string, enabled *bool) error {
	updates := map[string]interface{}{"status": status}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	return DB.Model(&ModelCatalog{}).Where("channel_id = ? and model_name = ?", channelId, modelName).Updates(updates).Error
}

func DeleteCatalogByChannel(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ModelCatalog{}).Error
}

// EnableCatalogModel 把目录里的模型真正追加到渠道的 Models 字段上
func EnableCatalogModel(channelId int, modelName string) error {
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return err
	}
	models := strings.Split(channel.Models, ",")
	for _, m := range models {
		if strings.TrimSpace(m) == modelName {
			return nil
		}
	}
	models = append(models, modelName)
	sort.Strings(models)
	channel.Models = strings.Join(models, ",")
	if err = channel.Update(); err != nil {
		return err
	}
	return UpdateCatalogStatus(channelId, modelName, config.ModelStatusNormal, boolPtr(true))
}

func boolPtr(b bool) *bool { return &b }

// ---------------------------------------------------------------------------
// 渠道健康度
// ---------------------------------------------------------------------------

type ChannelHealth struct {
	ChannelId        int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	ChannelName      string  `json:"channel_name"`
	Status           string  `json:"status" gorm:"type:varchar(16);default:'unknown'"`
	LatencyMs        int     `json:"latency_ms"`
	SuccessCount     int64   `json:"success_count" gorm:"bigint"`
	FailCount        int64   `json:"fail_count" gorm:"bigint"`
	ErrorRate        float64 `json:"error_rate"`
	ConsecutiveFails int     `json:"consecutive_fails"`
	WeightFactor     float64 `json:"weight_factor"`
	LastCheck        int64   `json:"last_check" gorm:"bigint"`
	LastError        string  `json:"last_error" gorm:"type:text"`
}

// UpdateHealth 用一次探测结果刷新健康度，采用指数滑动平均
func UpdateHealth(channelId int, channelName string, ok bool, latencyMs int, errMsg string) error {
	now := helper.GetTimestamp()
	var record ChannelHealth
	err := DB.Where("channel_id = ?", channelId).First(&record).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record = ChannelHealth{
			ChannelId:    channelId,
			ChannelName:  channelName,
			Status:       config.HealthStatusUnknown,
			WeightFactor: 1,
		}
	}
	if ok {
		record.SuccessCount++
		record.ConsecutiveFails = 0
		if record.LatencyMs == 0 {
			record.LatencyMs = latencyMs
		} else {
			// 0.7 历史 + 0.3 本次
			record.LatencyMs = int(0.7*float64(record.LatencyMs) + 0.3*float64(latencyMs))
		}
		record.LastError = ""
	} else {
		record.FailCount++
		record.ConsecutiveFails++
		record.LastError = truncate(errMsg, 512)
		if latencyMs > 0 {
			if record.LatencyMs == 0 {
				record.LatencyMs = latencyMs
			} else {
				record.LatencyMs = int(0.7*float64(record.LatencyMs) + 0.3*float64(latencyMs))
			}
		}
	}
	newTotal := record.SuccessCount + record.FailCount
	if newTotal > 0 {
		record.ErrorRate = float64(record.FailCount) / float64(newTotal)
	}
	record.LastCheck = now
	record.Status = evaluateHealth(&record)
	record.WeightFactor = weightOf(&record)
	return DB.Save(&record).Error
}

func evaluateHealth(h *ChannelHealth) string {
	switch {
	case h.ConsecutiveFails >= config.HealthCheckFailPause && config.HealthCheckFailPause > 0:
		return config.HealthStatusDead
	case h.ErrorRate >= config.HealthCheckErrorRatePause && h.SuccessCount+h.FailCount >= 5:
		return config.HealthStatusPaused
	case h.LatencyMs >= config.HealthCheckDeadLatency && config.HealthCheckDeadLatency > 0:
		return config.HealthStatusDead
	case h.LatencyMs >= config.HealthCheckDegradedLatency && config.HealthCheckDegradedLatency > 0:
		return config.HealthStatusDegraded
	case h.SuccessCount+h.FailCount == 0:
		return config.HealthStatusUnknown
	default:
		return config.HealthStatusHealthy
	}
}

// weightOf 把健康度折算成一个 0~1 的权重系数，供路由打分使用
func weightOf(h *ChannelHealth) float64 {
	base := 1.0
	switch h.Status {
	case config.HealthStatusHealthy:
		base = 1.0
	case config.HealthStatusDegraded:
		base = 0.6
	case config.HealthStatusPaused:
		base = 0.3
	case config.HealthStatusDead:
		base = 0.05
	case config.HealthStatusUnknown:
		base = 0.8
	}
	base *= 1 - h.ErrorRate*0.5
	if base < 0.01 {
		base = 0.01
	}
	return base
}

func GetChannelHealth(channelId int) (*ChannelHealth, error) {
	var record ChannelHealth
	err := DB.Where("channel_id = ?", channelId).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &ChannelHealth{
				ChannelId:    channelId,
				Status:       config.HealthStatusUnknown,
				WeightFactor: 0.8,
			}, nil
		}
		return nil, err
	}
	return &record, nil
}

func GetAllHealth() ([]*ChannelHealth, error) {
	var records []*ChannelHealth
	err := DB.Order("channel_id asc").Find(&records).Error
	return records, err
}

// HealthSnapshot 供路由使用，避免每个请求都查库
type HealthSnapshot struct {
	Status       string
	LatencyMs    int
	ErrorRate    float64
	WeightFactor float64
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---------------------------------------------------------------------------
// 模型别名
// ---------------------------------------------------------------------------

type ModelAlias struct {
	Id          int    `json:"id"`
	Alias       string `json:"alias" gorm:"uniqueIndex;type:varchar(128)"`
	Target      string `json:"target" gorm:"type:varchar(128)"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	Remark      string `json:"remark"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

func GetAllAlias() ([]*ModelAlias, error) {
	var items []*ModelAlias
	err := DB.Order("id desc").Find(&items).Error
	return items, err
}

func CreateAlias(alias *ModelAlias) error {
	alias.CreatedTime = helper.GetTimestamp()
	return DB.Create(alias).Error
}

func UpdateAlias(alias *ModelAlias) error {
	return DB.Model(&ModelAlias{}).Where("id = ?", alias.Id).Select("alias", "target", "enabled", "remark").Updates(alias).Error
}

func DeleteAlias(id int) error {
	return DB.Where("id = ?", id).Delete(&ModelAlias{}).Error
}

// ---------------------------------------------------------------------------
// 模型价格（USD / 1M tokens）
// ---------------------------------------------------------------------------

type ModelPrice struct {
	Id          int     `json:"id"`
	ModelName   string  `json:"model_name" gorm:"uniqueIndex;type:varchar(128)"`
	InputPrice  float64 `json:"input_price"`  // USD per 1M prompt tokens
	OutputPrice float64 `json:"output_price"` // USD per 1M completion tokens
	UpdatedTime int64   `json:"updated_time" gorm:"bigint"`
}

func GetAllPrices() ([]*ModelPrice, error) {
	var items []*ModelPrice
	err := DB.Order("model_name asc").Find(&items).Error
	return items, err
}

func UpsertPrice(price *ModelPrice) error {
	price.UpdatedTime = helper.GetTimestamp()
	var existing ModelPrice
	err := DB.Where("model_name = ?", price.ModelName).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DB.Create(price).Error
		}
		return err
	}
	price.Id = existing.Id
	return DB.Model(&ModelPrice{}).Where("id = ?", existing.Id).
		Select("input_price", "output_price", "updated_time").Updates(price).Error
}

func DeletePrice(id int) error {
	return DB.Where("id = ?", id).Delete(&ModelPrice{}).Error
}

// ---------------------------------------------------------------------------
// 用户预算
// ---------------------------------------------------------------------------

type UserBudget struct {
	UserId       int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	DailyLimit   int64 `json:"daily_limit" gorm:"bigint"`   // 额度点（quota）
	MonthlyLimit int64 `json:"monthly_limit" gorm:"bigint"` // 额度点（quota）
	UpdatedTime  int64 `json:"updated_time" gorm:"bigint"`
}

func GetUserBudget(userId int) (*UserBudget, error) {
	var budget UserBudget
	err := DB.Where("user_id = ?", userId).First(&budget).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &UserBudget{
				UserId:       userId,
				DailyLimit:   int64(config.DefaultDailyBudget),
				MonthlyLimit: int64(config.DefaultMonthlyBudget),
			}, nil
		}
		return nil, err
	}
	return &budget, nil
}

func UpsertUserBudget(budget *UserBudget) error {
	budget.UpdatedTime = helper.GetTimestamp()
	var existing UserBudget
	err := DB.Where("user_id = ?", budget.UserId).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DB.Create(budget).Error
		}
		return err
	}
	return DB.Model(&UserBudget{}).Where("user_id = ?", budget.UserId).
		Select("daily_limit", "monthly_limit", "updated_time").Updates(budget).Error
}

// ---------------------------------------------------------------------------
// 成本统计
// ---------------------------------------------------------------------------

type ModelCostStat struct {
	ModelName        string `json:"model_name"`
	Requests         int64  `json:"requests"`
	Quota            int64  `json:"quota"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	AvgElapsedMs     int64  `json:"avg_elapsed_ms"`
}

func GetUserCostStat(userId int, days int) ([]*ModelCostStat, int64, error) {
	if days <= 0 {
		days = 7
	}
	start := time.Now().AddDate(0, 0, -days).Unix()
	rows := make([]*ModelCostStat, 0)
	query := LOG_DB.Model(&Log{}).Select(
		"model_name, count(*) as requests, sum(quota) as quota, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, avg(elapsed_time) as avg_elapsed_ms").
		Where("type = ? and created_at >= ?", LogTypeConsume, start)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	err := query.Group("model_name").Order("quota desc").Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	var total int64
	for _, r := range rows {
		total += r.Quota
	}
	return rows, total, nil
}

func GetUserPeriodQuota(userId int, since int64) (int64, error) {
	var used int64
	err := LOG_DB.Model(&Log{}).
		Where("type = ? and user_id = ? and created_at >= ?", LogTypeConsume, userId, since).
		Select("coalesce(sum(quota), 0)").Scan(&used).Error
	return used, err
}

// ---------------------------------------------------------------------------
// 路由候选查询
// ---------------------------------------------------------------------------

// GetChannelsForModel 返回分组下所有声明支持该模型且处于启用状态的渠道
func GetChannelsForModel(group string, modelName string) ([]*Channel, error) {
	groupCol := "`group`"
	trueVal := "1"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
		trueVal = "true"
	}
	var abilityList []*Ability
	err := DB.Where(groupCol+" = ? and model = ? and enabled = "+trueVal, group, modelName).Find(&abilityList).Error
	if err != nil {
		return nil, err
	}
	if len(abilityList) == 0 {
		return nil, errors.New("channel not found")
	}
	ids := make([]int, 0, len(abilityList))
	seen := make(map[int]bool)
	for _, ability := range abilityList {
		if seen[ability.ChannelId] {
			continue
		}
		seen[ability.ChannelId] = true
		ids = append(ids, ability.ChannelId)
	}
	var channels []*Channel
	err = DB.Where("id in ? and status = ?", ids, ChannelStatusEnabled).Find(&channels).Error
	return channels, err
}
