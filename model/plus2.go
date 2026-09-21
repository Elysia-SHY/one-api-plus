package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Elysia-SHY/one-api-plus/common/helper"
)

// ---------------------------------------------------------------------------
// 模型能力数据库
//
// ModelCapability 以 model_name 为唯一键，记录一个模型的静态能力画像：
// 上下文窗口、输出上限、是否支持视觉 / 工具调用 / 推理 / 嵌入等。
// 能力来源有两种：内置知识库推断（rule）与在线探测（probe）。
// ---------------------------------------------------------------------------

type ModelCapability struct {
	Id            int    `json:"id"`
	ModelName     string `json:"model_name" gorm:"uniqueIndex;type:varchar(128)"`
	ContextLength int    `json:"context_length" gorm:"default:0"`
	MaxOutput     int    `json:"max_output" gorm:"default:0"`
	Vision        int    `json:"vision" gorm:"default:0"`              // 0 未知 / 1 支持 / 2 不支持
	ToolCall      int    `json:"tool_call" gorm:"default:0"`           // 同上
	Reasoning     int    `json:"reasoning" gorm:"default:0"`           // 同上
	Embedding     int    `json:"embedding" gorm:"default:0"`           // 同上
	Streaming     int    `json:"streaming" gorm:"default:0"`           // 同上
	JSONMode      int    `json:"json_mode" gorm:"default:0"`           // 同上
	InputTypes    string `json:"input_types" gorm:"type:varchar(64)"`  // text,image,audio
	OutputTypes   string `json:"output_types" gorm:"type:varchar(64)"` // text,json
	Source        string `json:"source" gorm:"type:varchar(16)"`       // rule / probe / manual
	Remark        string `json:"remark"`
	UpdatedTime   int64  `json:"updated_time" gorm:"bigint"`
}

// 能力三态
const (
	CapUnknown     = 0
	CapSupported   = 1
	CapUnsupported = 2
)

func GetCapability(modelName string) (*ModelCapability, error) {
	var item ModelCapability
	err := DB.Where("model_name = ?", modelName).First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func GetAllCapabilities() ([]*ModelCapability, error) {
	var items []*ModelCapability
	err := DB.Order("model_name asc").Find(&items).Error
	return items, err
}

// UpsertCapability 写入或合并一条能力记录；merge=true 时只覆盖当前未知(0)的字段
func UpsertCapability(item *ModelCapability, merge bool) error {
	item.UpdatedTime = helper.GetTimestamp()
	existing, err := GetCapability(item.ModelName)
	if err != nil {
		return err
	}
	if existing == nil {
		item.Id = 0
		return DB.Create(item).Error
	}
	if merge {
		item.Id = existing.Id
		patch := map[string]interface{}{"updated_time": item.UpdatedTime, "source": item.Source}
		if existing.ContextLength == 0 && item.ContextLength > 0 {
			patch["context_length"] = item.ContextLength
		}
		if existing.MaxOutput == 0 && item.MaxOutput > 0 {
			patch["max_output"] = item.MaxOutput
		}
		for field, val := range map[string]int{
			"vision": item.Vision, "tool_call": item.ToolCall, "reasoning": item.Reasoning,
			"embedding": item.Embedding, "streaming": item.Streaming, "json_mode": item.JSONMode,
		} {
			if val != CapUnknown {
				patch[field] = val
			}
		}
		if existing.InputTypes == "" && item.InputTypes != "" {
			patch["input_types"] = item.InputTypes
		}
		if existing.OutputTypes == "" && item.OutputTypes != "" {
			patch["output_types"] = item.OutputTypes
		}
		if item.Remark != "" {
			patch["remark"] = item.Remark
		}
		return DB.Model(&ModelCapability{}).Where("id = ?", existing.Id).Updates(patch).Error
	}
	item.Id = existing.Id
	fields := []string{"context_length", "max_output", "vision", "tool_call", "reasoning",
		"embedding", "streaming", "json_mode", "input_types", "output_types", "source", "remark", "updated_time"}
	return DB.Model(&ModelCapability{}).Where("id = ?", existing.Id).Select(fields).Updates(item).Error
}

// SearchCapabilities 按能力过滤模型：任何传 true 的条件都必须满足
func SearchCapabilities(ctxLen int, vision bool, toolCall bool, reasoning bool, embedding bool) ([]*ModelCapability, error) {
	query := DB.Model(&ModelCapability{})
	if ctxLen > 0 {
		query = query.Where("context_length >= ?", ctxLen)
	}
	if vision {
		query = query.Where("vision = ?", CapSupported)
	}
	if toolCall {
		query = query.Where("tool_call = ?", CapSupported)
	}
	if reasoning {
		query = query.Where("reasoning = ?", CapSupported)
	}
	if embedding {
		query = query.Where("embedding = ?", CapSupported)
	}
	var items []*ModelCapability
	err := query.Order("model_name asc").Find(&items).Error
	return items, err
}

// SyncCapabilityToChannel 把能力结论回填到某一渠道下该模型的目录记录上
func SyncCapabilityToChannel(channelId int, modelName string, cap *ModelCapability) error {
	if cap == nil {
		return nil
	}
	return DB.Model(&ModelCatalog{}).Where("channel_id = ? and model_name = ?", channelId, modelName).
		Updates(map[string]interface{}{
			"context_length": cap.ContextLength,
			"max_output":     cap.MaxOutput,
			"vision":         cap.Vision,
			"tool_call":      cap.ToolCall,
			"reasoning":      cap.Reasoning,
			"embedding":      cap.Embedding,
		}).Error
}

// ---------------------------------------------------------------------------
// 模型组（逻辑模型层）
//
// 用户请求逻辑名（如 smart-code），网关从成员模型里挑一个真实可用模型转发。
// ---------------------------------------------------------------------------

type ModelGroup struct {
	Id          int                 `json:"id"`
	GroupName   string              `json:"group_name" gorm:"uniqueIndex;type:varchar(128)"`
	Strategy    string              `json:"strategy" gorm:"type:varchar(32);default:'capability'"`
	Enabled     bool                `json:"enabled" gorm:"default:true"`
	Remark      string              `json:"remark"`
	CreatedTime int64               `json:"created_time" gorm:"bigint"`
	Members     []*ModelGroupMember `json:"members" gorm:"-"`
}

type ModelGroupMember struct {
	Id          int    `json:"id"`
	GroupName   string `json:"group_name" gorm:"index:idx_group_member;type:varchar(128)"`
	ModelName   string `json:"model_name" gorm:"index:idx_group_member;type:varchar(128)"`
	Weight      int    `json:"weight" gorm:"default:1"`
	Priority    int    `json:"priority" gorm:"default:0"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

func GetAllModelGroups() ([]*ModelGroup, error) {
	var groups []*ModelGroup
	if err := DB.Order("id desc").Find(&groups).Error; err != nil {
		return nil, err
	}
	members, err := GetAllGroupMembers("")
	if err != nil {
		return groups, err
	}
	byGroup := make(map[string][]*ModelGroupMember, len(groups))
	for _, m := range members {
		byGroup[m.GroupName] = append(byGroup[m.GroupName], m)
	}
	for _, g := range groups {
		g.Members = byGroup[g.GroupName]
	}
	return groups, nil
}

func GetModelGroup(name string) (*ModelGroup, error) {
	var group ModelGroup
	if err := DB.Where("group_name = ?", name).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	members, err := GetAllGroupMembers(name)
	if err != nil {
		return nil, err
	}
	group.Members = members
	return &group, nil
}

func GetAllGroupMembers(groupName string) ([]*ModelGroupMember, error) {
	var items []*ModelGroupMember
	query := DB.Model(&ModelGroupMember{})
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	err := query.Order("priority desc, id asc").Find(&items).Error
	return items, err
}

func CreateModelGroup(group *ModelGroup) error {
	group.GroupName = strings.TrimSpace(group.GroupName)
	if group.GroupName == "" {
		return errors.New("group name is required")
	}
	if group.Strategy == "" {
		group.Strategy = "capability"
	}
	group.CreatedTime = helper.GetTimestamp()
	return DB.Create(group).Error
}

func UpdateModelGroup(group *ModelGroup) error {
	return DB.Model(&ModelGroup{}).Where("id = ?", group.Id).
		Select("group_name", "strategy", "enabled", "remark").Updates(group).Error
}

func DeleteModelGroup(id int) error {
	group, err := GetModelGroupById(id)
	if err != nil {
		return err
	}
	if group == nil {
		return nil
	}
	if err = DB.Where("group_name = ?", group.GroupName).Delete(&ModelGroupMember{}).Error; err != nil {
		return err
	}
	return DB.Where("id = ?", id).Delete(&ModelGroup{}).Error
}

func GetModelGroupById(id int) (*ModelGroup, error) {
	var group ModelGroup
	err := DB.Where("id = ?", id).First(&group).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &group, nil
}

func AddGroupMember(member *ModelGroupMember) error {
	member.ModelName = strings.TrimSpace(member.ModelName)
	if member.ModelName == "" || member.GroupName == "" {
		return errors.New("group name and model name are required")
	}
	if member.Weight <= 0 {
		member.Weight = 1
	}
	member.CreatedTime = helper.GetTimestamp()
	var existing ModelGroupMember
	err := DB.Where("group_name = ? and model_name = ?", member.GroupName, member.ModelName).First(&existing).Error
	if err == nil {
		member.Id = existing.Id
		return DB.Model(&ModelGroupMember{}).Where("id = ?", existing.Id).
			Select("weight", "priority", "enabled").Updates(member).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Create(member).Error
}

func UpdateGroupMember(member *ModelGroupMember) error {
	return DB.Model(&ModelGroupMember{}).Where("id = ?", member.Id).
		Select("weight", "priority", "enabled").Updates(member).Error
}

func DeleteGroupMember(id int) error {
	return DB.Where("id = ?", id).Delete(&ModelGroupMember{}).Error
}

// ---------------------------------------------------------------------------
// MCP 服务器注册
// ---------------------------------------------------------------------------

type MCPServer struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"uniqueIndex;type:varchar(64)"`
	URL         string `json:"url" gorm:"type:varchar(512)"`
	Protocol    string `json:"protocol" gorm:"type:varchar(16);default:'http'"` // http / sse / stdio
	AuthToken   string `json:"auth_token" gorm:"type:varchar(256)"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	Remark      string `json:"remark"`
	LastError   string `json:"last_error" gorm:"type:text"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

func GetAllMCPServers() ([]*MCPServer, error) {
	var items []*MCPServer
	err := DB.Order("id desc").Find(&items).Error
	return items, err
}

func UpsertMCPServer(server *MCPServer) error {
	server.UpdatedTime = helper.GetTimestamp()
	var existing MCPServer
	err := DB.Where("name = ?", server.Name).First(&existing).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		server.Id = 0
		return DB.Create(server).Error
	}
	server.Id = existing.Id
	return DB.Model(&MCPServer{}).Where("id = ?", existing.Id).
		Select("url", "protocol", "auth_token", "enabled", "remark", "last_error", "updated_time").Updates(server).Error
}

func DeleteMCPServer(id int) error {
	return DB.Where("id = ?", id).Delete(&MCPServer{}).Error
}

// ---------------------------------------------------------------------------
// Agent Memory
// ---------------------------------------------------------------------------

type MemoryRecord struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index:idx_mem_user_session"`
	SessionId   string `json:"session_id" gorm:"index:idx_mem_user_session;type:varchar(128)"`
	Scope       string `json:"scope" gorm:"type:varchar(16);default:'short'"` // short / long / profile
	Role        string `json:"role" gorm:"type:varchar(16);default:'user'"`
	Content     string `json:"content" gorm:"type:text"`
	Keywords    string `json:"keywords" gorm:"type:varchar(256)"`
	Tokens      int    `json:"tokens" gorm:"default:0"`
	CreatedTime int64  `json:"created_time" gorm:"bigint;index"`
}

func AddMemory(record *MemoryRecord) error {
	if record.CreatedTime == 0 {
		record.CreatedTime = helper.GetTimestamp()
	}
	if record.Scope == "" {
		record.Scope = "short"
	}
	if record.SessionId == "" {
		record.SessionId = "default"
	}
	return DB.Create(record).Error
}

// ListMemory 返回最近 limit 条记忆；createAfter > 0 时按时间下界过滤
func ListMemory(userId int, sessionId string, scope string, createAfter int64, limit int) ([]*MemoryRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := DB.Model(&MemoryRecord{}).Where("user_id = ?", userId)
	if sessionId != "" {
		query = query.Where("session_id = ?", sessionId)
	}
	if scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if createAfter > 0 {
		query = query.Where("created_time >= ?", createAfter)
	}
	var items []*MemoryRecord
	err := query.Order("id desc").Limit(limit).Find(&items).Error
	return items, err
}

func DeleteMemory(id int, userId int) error {
	query := DB.Where("id = ?", id)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	return query.Delete(&MemoryRecord{}).Error
}

func ClearMemory(userId int, sessionId string) error {
	query := DB.Where("user_id = ?", userId)
	if sessionId != "" {
		query = query.Where("session_id = ?", sessionId)
	}
	return query.Delete(&MemoryRecord{}).Error
}

// CountMemory 统计某用户当前持有的记忆条数，用于上限控制
func CountMemory(userId int, sessionId string) (int64, error) {
	var total int64
	query := DB.Model(&MemoryRecord{}).Where("user_id = ?", userId)
	if sessionId != "" {
		query = query.Where("session_id = ?", sessionId)
	}
	err := query.Count(&total).Error
	return total, err
}

// TrimMemory 保留最近 keep 条，其余删除
func TrimMemory(userId int, sessionId string, keep int) error {
	if keep <= 0 {
		return nil
	}
	var ids []int
	query := DB.Model(&MemoryRecord{}).Where("user_id = ?", userId)
	if sessionId != "" {
		query = query.Where("session_id = ?", sessionId)
	}
	if err := query.Order("id desc").Limit(100000).Offset(keep).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return DB.Where("id in ?", ids).Delete(&MemoryRecord{}).Error
}

// ---------------------------------------------------------------------------
// Dashboard 聚合查询
// ---------------------------------------------------------------------------

// TrendPoint 是时间序列上的一个点
type TrendPoint struct {
	Bucket  string `json:"bucket"`
	Count   int64  `json:"count"`
	Quota   int64  `json:"quota"`
	Tokens  int64  `json:"tokens"`
	Elapsed int64  `json:"elapsed_ms"`
}

// GetRequestTrend 按小时/天聚合消费日志
//
// 说明：SQLite/MySQL/PostgreSQL 的时间格式化函数各不相同，这里统一在 Go 侧
// 拉取 (created_at, quota, tokens, elapsed) 明细后在内存中分桶，避免方言分支，
// 同时保证三种数据库下 Dashboard 数据一致。
func GetRequestTrend(userId int, start int64, end int64, byHour bool) ([]*TrendPoint, error) {
	var rows []struct {
		CreatedAt int64 `gorm:"column:created_at"`
		Quota     int64 `gorm:"column:quota"`
		Tokens    int64 `gorm:"column:tokens"`
		Elapsed   int64 `gorm:"column:elapsed_time"`
	}
	query := LOG_DB.Model(&Log{}).
		Select("created_at, quota, prompt_tokens + completion_tokens as tokens, elapsed_time").
		Where("type = ? and created_at between ? and ?", LogTypeConsume, start, end)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if err := query.Limit(trendMaxRows).Scan(&rows).Error; err != nil {
		return nil, err
	}
	layout := "2006-01-02"
	if byHour {
		layout = "2006-01-02 15:00"
	}
	index := make(map[string]int)
	points := make([]*TrendPoint, 0, 48)
	for _, r := range rows {
		bucket := time.Unix(r.CreatedAt, 0).Format(layout)
		pos, ok := index[bucket]
		if !ok {
			pos = len(points)
			index[bucket] = pos
			points = append(points, &TrendPoint{Bucket: bucket})
		}
		p := points[pos]
		p.Count++
		p.Quota += r.Quota
		p.Tokens += r.Tokens
		p.Elapsed += r.Elapsed
	}
	for _, p := range points {
		if p.Count > 0 {
			p.Elapsed /= p.Count
		}
	}
	return points, nil
}

// trendMaxRows 限制单次趋势聚合的扫描行数，防止超大数据拖垮 Dashboard
const trendMaxRows = 200000
