// Package alias 提供模型别名映射，例如 gemini-flash -> gemini-2.5-flash-preview，
// 让 Agent、IDE 与用户可以用稳定的短名调用模型。
package alias

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/model"
)

const cacheTTL = 60 * time.Second

var (
	cache    map[string]string
	cacheAt  time.Time
	cacheMux sync.RWMutex
)

// Refresh 重新加载别名表
func Refresh() error {
	items, err := model.GetAllAlias()
	if err != nil {
		return err
	}
	next := make(map[string]string, len(items))
	for _, item := range items {
		if !item.Enabled || item.Alias == "" || item.Target == "" {
			continue
		}
		next[strings.ToLower(item.Alias)] = item.Target
	}
	cacheMux.Lock()
	cache = next
	cacheAt = time.Now()
	cacheMux.Unlock()
	return nil
}

func ensureLoaded() {
	cacheMux.RLock()
	fresh := cache != nil && time.Since(cacheAt) < cacheTTL
	cacheMux.RUnlock()
	if fresh {
		return
	}
	if err := Refresh(); err != nil {
		logger.SysError("failed to refresh model alias: " + err.Error())
	}
}

// Resolve 把别名解析成真实模型名；未命中时原样返回
func Resolve(modelName string) string {
	if !config.AliasEnabled || modelName == "" {
		return modelName
	}
	ensureLoaded()
	cacheMux.RLock()
	target, ok := cache[strings.ToLower(modelName)]
	cacheMux.RUnlock()
	if !ok {
		return modelName
	}
	return target
}

// SeedIfEmpty 在别名表为空时，用 DEFAULT_ALIASES（JSON，如 {"gemini-flash":"gemini-2.5-flash-preview"}）初始化
func SeedIfEmpty(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	items, err := model.GetAllAlias()
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return Refresh()
	}
	var seeds map[string]string
	if err = json.Unmarshal([]byte(raw), &seeds); err != nil {
		return err
	}
	for k, v := range seeds {
		if k == "" || v == "" {
			continue
		}
		if err = model.CreateAlias(&model.ModelAlias{Alias: k, Target: v, Enabled: true}); err != nil {
			logger.SysError(fmt.Sprintf("failed to seed alias %s: %s", k, err.Error()))
		}
	}
	logger.SysLog(fmt.Sprintf("seeded %d model aliases from DEFAULT_ALIASES", len(seeds)))
	return Refresh()
}

// All 返回当前所有别名（拷贝）
func All() map[string]string {
	ensureLoaded()
	cacheMux.RLock()
	defer cacheMux.RUnlock()
	copied := make(map[string]string, len(cache))
	for k, v := range cache {
		copied[k] = v
	}
	return copied
}
