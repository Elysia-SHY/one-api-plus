package router

import (
	"embed"
	"fmt"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"strings"
)

// webConsoleAvailable 判断是否真的嵌入了前端产物
//
// Lite 构建（-tags lite）不 embed web/build，此时如果照常注册 Web 路由，
// 每个静态请求都会打到空 FS 上，白白占用内存与日志。
func webConsoleAvailable(buildFS embed.FS) bool {
	entries, err := buildFS.ReadDir("web/build")
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// serveHeadlessNotice 是 Lite 变体的兜底响应
func serveHeadlessNotice(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{
		"success": false,
		"message": "这是 One API Plus Lite 版本，未包含 Web 控制台；请通过 API 调用，或部署完整版本以获得管理界面",
	})
}

func SetRouter(router *gin.Engine, buildFS embed.FS) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if config.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		logger.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		if webConsoleAvailable(buildFS) {
			SetWebRouter(router, buildFS)
		} else {
			// Lite 变体：没有嵌入前端时给一个最小提示页，不尝试 serve 静态资源
			router.NoRoute(serveHeadlessNotice)
		}
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}
