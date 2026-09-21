//go:build lite

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
	// Lite 构建：不注册 Dashboard / MCP / Memory 等重型管理接口
	_ = plusRoute
	_ = controller.GetDashboard
	_ = middleware.AdminAuth
}
