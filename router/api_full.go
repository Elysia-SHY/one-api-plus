//go:build !lite

package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/controller"
	"github.com/Elysia-SHY/one-api-plus/middleware"
)

// registerPlus2Routes 注册第二阶段（AI Gateway）的管理接口。
//
// Lite 构建下这组接口整体不编译进来：它们是「管理向」能力，
// 在内存吃紧的设备上属于纯粹的额外开销，中继链路本身不受影响。
func registerPlus2Routes(plusRoute *gin.RouterGroup) {
	// ---- 第二阶段（AI Gateway）----
	// Dashboard 与实时负载
	plusRoute.GET("/dashboard", middleware.UserAuth(), controller.GetDashboard)
	plusRoute.GET("/load", middleware.AdminAuth(), controller.GetLoadSnapshot)
	plusRoute.GET("/routing", middleware.AdminAuth(), controller.GetRoutingWeights)
	plusRoute.PUT("/routing", middleware.AdminAuth(), controller.UpdateRoutingWeights)
	plusRoute.GET("/ratelimit", middleware.UserAuth(), controller.GetRateLimitStatus)
	// 模型能力数据库
	plusRoute.GET("/capability", middleware.UserAuth(), controller.GetCapabilities)
	plusRoute.GET("/capability/search", middleware.UserAuth(), controller.SearchCapabilities)
	plusRoute.GET("/capability/:name", middleware.UserAuth(), controller.GetCapability)
	plusRoute.POST("/capability/:name/refresh", middleware.AdminAuth(), controller.RefreshCapability)
	plusRoute.PUT("/capability", middleware.AdminAuth(), controller.UpdateCapability)
	// 模型组
	plusRoute.GET("/group/model", middleware.UserAuth(), controller.GetModelGroups)
	plusRoute.POST("/group/model", middleware.AdminAuth(), controller.AddModelGroup)
	plusRoute.PUT("/group/model", middleware.AdminAuth(), controller.UpdateModelGroup)
	plusRoute.DELETE("/group/model/:id", middleware.AdminAuth(), controller.RemoveModelGroup)
	plusRoute.POST("/group/model/member", middleware.AdminAuth(), controller.AddGroupMember)
	plusRoute.DELETE("/group/model/member/:id", middleware.AdminAuth(), controller.RemoveGroupMember)
	// MCP 网关
	plusRoute.GET("/mcp", middleware.AdminAuth(), controller.GetMCPServers)
	plusRoute.POST("/mcp", middleware.AdminAuth(), controller.UpsertMCPServer)
	plusRoute.DELETE("/mcp/:id", middleware.AdminAuth(), controller.RemoveMCPServer)
	plusRoute.POST("/mcp/discover", middleware.AdminAuth(), controller.DiscoverMCPTools)
	plusRoute.POST("/mcp/call", middleware.AdminAuth(), controller.CallMCPTool)
	// Agent Memory
	plusRoute.GET("/memory", middleware.UserAuth(), controller.GetMemory)
	plusRoute.POST("/memory", middleware.UserAuth(), controller.AddMemory)
	plusRoute.DELETE("/memory/:id", middleware.UserAuth(), controller.DeleteMemory)
	plusRoute.DELETE("/memory", middleware.UserAuth(), controller.ClearMemory)
	plusRoute.GET("/memory/search", middleware.UserAuth(), controller.SearchMemory)
	// Responses API
	plusRoute.GET("/responses", middleware.UserAuth(), controller.GetResponsesStatus)
}
