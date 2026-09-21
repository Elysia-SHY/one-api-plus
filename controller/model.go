package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/model"
	relay "github.com/Elysia-SHY/one-api-plus/relay"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/openai"
	"github.com/Elysia-SHY/one-api-plus/relay/apitype"
	"github.com/Elysia-SHY/one-api-plus/relay/channeltype"
	"github.com/Elysia-SHY/one-api-plus/relay/meta"
	relaymodel "github.com/Elysia-SHY/one-api-plus/relay/model"
	"github.com/Elysia-SHY/one-api-plus/service/modelgroup"
	"net/http"
	"strings"
)

// https://platform.openai.com/docs/api-reference/models/list

type OpenAIModelPermission struct {
	Id                 string  `json:"id"`
	Object             string  `json:"object"`
	Created            int     `json:"created"`
	AllowCreateEngine  bool    `json:"allow_create_engine"`
	AllowSampling      bool    `json:"allow_sampling"`
	AllowLogprobs      bool    `json:"allow_logprobs"`
	AllowSearchIndices bool    `json:"allow_search_indices"`
	AllowView          bool    `json:"allow_view"`
	AllowFineTuning    bool    `json:"allow_fine_tuning"`
	Organization       string  `json:"organization"`
	Group              *string `json:"group"`
	IsBlocking         bool    `json:"is_blocking"`
}

type OpenAIModels struct {
	Id         string                  `json:"id"`
	Object     string                  `json:"object"`
	Created    int                     `json:"created"`
	OwnedBy    string                  `json:"owned_by"`
	Permission []OpenAIModelPermission `json:"permission"`
	Root       string                  `json:"root"`
	Parent     *string                 `json:"parent"`
}

var models []OpenAIModels
var modelsMap map[string]OpenAIModels
var channelId2Models map[int][]string

func init() {
	var permission []OpenAIModelPermission
	permission = append(permission, OpenAIModelPermission{
		Id:                 "modelperm-LwHkVFn8AcMItP432fKKDIKJ",
		Object:             "model_permission",
		Created:            1626777600,
		AllowCreateEngine:  true,
		AllowSampling:      true,
		AllowLogprobs:      true,
		AllowSearchIndices: false,
		AllowView:          true,
		AllowFineTuning:    false,
		Organization:       "*",
		Group:              nil,
		IsBlocking:         false,
	})
	// https://platform.openai.com/docs/models/model-endpoint-compatibility
	for i := 0; i < apitype.Dummy; i++ {
		if i == apitype.AIProxyLibrary {
			continue
		}
		adaptor := relay.GetAdaptor(i)
		channelName := adaptor.GetChannelName()
		modelNames := adaptor.GetModelList()
		for _, modelName := range modelNames {
			models = append(models, OpenAIModels{
				Id:         modelName,
				Object:     "model",
				Created:    1626777600,
				OwnedBy:    channelName,
				Permission: permission,
				Root:       modelName,
				Parent:     nil,
			})
		}
	}
	for _, channelType := range openai.CompatibleChannels {
		if channelType == channeltype.Azure {
			continue
		}
		channelName, channelModelList := openai.GetCompatibleChannelMeta(channelType)
		for _, modelName := range channelModelList {
			models = append(models, OpenAIModels{
				Id:         modelName,
				Object:     "model",
				Created:    1626777600,
				OwnedBy:    channelName,
				Permission: permission,
				Root:       modelName,
				Parent:     nil,
			})
		}
	}
	modelsMap = make(map[string]OpenAIModels)
	for _, model := range models {
		modelsMap[model.Id] = model
	}
	channelId2Models = make(map[int][]string)
	for i := 1; i < channeltype.Dummy; i++ {
		adaptor := relay.GetAdaptor(channeltype.ToAPIType(i))
		meta := &meta.Meta{
			ChannelType: i,
		}
		adaptor.Init(meta)
		channelId2Models[i] = adaptor.GetModelList()
	}
}

func DashboardListModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelId2Models,
	})
}

func ListAllModels(c *gin.Context) {
	c.JSON(200, gin.H{
		"object": "list",
		"data":   models,
	})
}

func ListModels(c *gin.Context) {
	ctx := c.Request.Context()
	var availableModels []string
	var userGroup string
	if c.GetString(ctxkey.AvailableModels) != "" {
		availableModels = strings.Split(c.GetString(ctxkey.AvailableModels), ",")
	} else {
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ = model.CacheGetUserGroup(userId)
		availableModels, _ = model.CacheGetGroupModels(ctx, userGroup)
	}
	if userGroup == "" {
		userGroup = c.GetString(ctxkey.Group)
	}
	if userGroup == "" {
		userGroup = "default"
	}
	// One API Plus: /v1/models 也要把「模型组逻辑名」暴露给客户端，
	// 这样 Cherry Studio / NextChat / Codex 等的模型下拉里能直接看到逻辑名。
	availableModels = appendModelGroupNames(userGroup, availableModels)
	modelSet := make(map[string]bool)
	for _, availableModel := range availableModels {
		modelSet[strings.TrimSpace(availableModel)] = true
	}
	availableOpenAIModels := make([]OpenAIModels, 0)
	for _, model := range models {
		if _, ok := modelSet[model.Id]; ok {
			modelSet[model.Id] = false
			availableOpenAIModels = append(availableOpenAIModels, model)
		}
	}
	for modelName, ok := range modelSet {
		if ok {
			availableOpenAIModels = append(availableOpenAIModels, OpenAIModels{
				Id:      modelName,
				Object:  "model",
				Created: 1626777600,
				OwnedBy: "custom",
				Root:    modelName,
				Parent:  nil,
			})
		}
	}
	c.JSON(200, gin.H{
		"object": "list",
		"data":   availableOpenAIModels,
	})
}

func RetrieveModel(c *gin.Context) {
	modelId := c.Param("model")
	if model, ok := modelsMap[modelId]; ok {
		c.JSON(200, model)
	} else {
		Error := relaymodel.Error{
			Message: fmt.Sprintf("The model '%s' does not exist", modelId),
			Type:    "invalid_request_error",
			Param:   "model",
			Code:    "model_not_found",
		}
		c.JSON(200, gin.H{
			"error": Error,
		})
	}
}

func GetUserAvailableModels(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.GetInt(ctxkey.Id)
	userGroup, err := model.CacheGetUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	models, err := model.CacheGetGroupModels(ctx, userGroup)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// One API Plus: 把「模型组」逻辑名也纳入可选列表，用户令牌里可直接勾选逻辑名，
	// 由 distributor 在请求时自动解析到当前可用的真实模型。
	// 仅包含在当前分组下至少有一个成员模型可用的组。
	models = appendModelGroupNames(userGroup, models)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
	return
}

// appendModelGroupNames 把当前分组下可用的模型组逻辑名并入模型列表（去重）。
//
// userGroup 为空或取不到时退化为「不做分组可用性过滤」，只按组自身的开关判断，
// 避免管理员/根令牌场景下逻辑名凭空消失。
func appendModelGroupNames(userGroup string, models []string) []string {
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	for _, g := range modelgroup.All() {
		if g == nil || g.GroupName == "" || !g.Enabled {
			continue
		}
		if seen[g.GroupName] {
			continue
		}
		if groupUsable(userGroup, g) {
			out = append(out, g.GroupName)
			seen[g.GroupName] = true
		}
	}
	return out
}

// groupUsable 判断一个模型组在当前分组下是否可用。
//
// 组名本身不会出现在 abilities 表里，无法直接查渠道，因此判定口径是
// 「组内至少有一个启用的成员模型，在当前分组下有启用渠道」。
func groupUsable(userGroup string, g *model.ModelGroup) bool {
	if len(g.Members) == 0 {
		return false
	}
	for _, m := range g.Members {
		if m == nil || !m.Enabled || strings.TrimSpace(m.ModelName) == "" {
			continue
		}
		if userGroup == "" {
			// 无法确定分组时不做过严过滤：有启用的成员即视为可用
			return true
		}
		if channels, err := model.GetChannelsForModel(userGroup, m.ModelName); err == nil && len(channels) > 0 {
			return true
		}
	}
	return false
}
