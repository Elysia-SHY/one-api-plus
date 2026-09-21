package router

import (
	"embed"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/controller"
	"github.com/Elysia-SHY/one-api-plus/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

func SetWebRouter(router *gin.Engine, buildFS embed.FS) {
	themeDir := fmt.Sprintf("web/build/%s", config.Theme)
	var indexPageData []byte

	// 优先检查本地磁盘上是否存在前端静态资源
	if fi, err := os.Stat(themeDir + "/index.html"); err == nil && !fi.IsDir() {
		indexPageData, _ = os.ReadFile(themeDir + "/index.html")
		router.Use(gzip.Gzip(gzip.DefaultCompression))
		router.Use(middleware.GlobalWebRateLimit())
		router.Use(middleware.Cache())
		router.Use(static.Serve("/", static.LocalFile(themeDir, true)))
	} else {
		indexPageData, _ = buildFS.ReadFile(themeDir + "/index.html")
		router.Use(gzip.Gzip(gzip.DefaultCompression))
		router.Use(middleware.GlobalWebRateLimit())
		router.Use(middleware.Cache())
		router.Use(static.Serve("/", common.EmbedFolder(buildFS, themeDir)))
	}

	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexPageData)
	})
}
