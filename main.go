package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"

	"github.com/Elysia-SHY/one-api-plus/common"
	"github.com/Elysia-SHY/one-api-plus/common/client"
	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/i18n"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/controller"
	"github.com/Elysia-SHY/one-api-plus/middleware"
	"github.com/Elysia-SHY/one-api-plus/model"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/openai"
	"github.com/Elysia-SHY/one-api-plus/router"
	"github.com/Elysia-SHY/one-api-plus/service/alias"
	"github.com/Elysia-SHY/one-api-plus/service/capability"
	"github.com/Elysia-SHY/one-api-plus/service/health"
	"github.com/Elysia-SHY/one-api-plus/service/mcp"
	"github.com/Elysia-SHY/one-api-plus/service/modelgroup"
	"github.com/Elysia-SHY/one-api-plus/service/modelsync"
	"github.com/Elysia-SHY/one-api-plus/service/ratelimit"
	"github.com/Elysia-SHY/one-api-plus/service/routing"
)

func main() {
	common.Init()
	logger.SetupLogger()
	logger.SysLogf("One API Plus %s started", common.Version)

	if os.Getenv("GIN_MODE") != gin.DebugMode {
		gin.SetMode(gin.ReleaseMode)
	}
	if config.DebugEnabled {
		logger.SysLog("running in debug mode")
	}

	// Initialize SQL Database
	model.InitDB()
	model.InitLogDB()

	var err error
	err = model.CreateRootAccountIfNeed()
	if err != nil {
		logger.FatalLog("database init error: " + err.Error())
	}
	defer func() {
		err := model.CloseDB()
		if err != nil {
			logger.FatalLog("failed to close database: " + err.Error())
		}
	}()

	// Initialize Redis
	err = common.InitRedisClient()
	if err != nil {
		logger.FatalLog("failed to initialize Redis: " + err.Error())
	}

	// Initialize options
	model.InitOptionMap()
	logger.SysLog(fmt.Sprintf("using theme %s", config.Theme))
	if common.RedisEnabled {
		// for compatibility with old versions
		config.MemoryCacheEnabled = true
	}
	if config.MemoryCacheEnabled {
		logger.SysLog("memory cache enabled")
		logger.SysLog(fmt.Sprintf("sync frequency: %d seconds", config.SyncFrequency))
		model.InitChannelCache()
	}
	if config.MemoryCacheEnabled {
		go model.SyncOptions(config.SyncFrequency)
		go model.SyncChannelCache(config.SyncFrequency)
	}
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err != nil {
			logger.FatalLog("failed to parse CHANNEL_TEST_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyTestChannels(frequency)
	}
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		config.BatchUpdateEnabled = true
		logger.SysLog("batch update enabled with interval " + strconv.Itoa(config.BatchUpdateInterval) + "s")
		model.InitBatchUpdater()
	}
	if config.EnableMetric {
		logger.SysLog("metric enabled, will disable channel if too much request failed")
	}
	openai.InitTokenEncoders()
	client.Init()

	// Initialize i18n
	if err := i18n.Init(); err != nil {
		logger.FatalLog("failed to initialize i18n: " + err.Error())
	}

	// One API Plus：模型别名、智能路由、自动同步与健康检测
	if err := alias.SeedIfEmpty(config.DefaultAliases); err != nil {
		logger.SysError("failed to seed model aliases: " + err.Error())
	}
	if err := alias.Refresh(); err != nil {
		logger.SysError("failed to load model aliases: " + err.Error())
	}
	routing.LogStrategy()
	// 第二阶段：能力库、模型组、Agent Gateway 的初始化
	if err := capability.Refresh(); err != nil {
		logger.SysError("failed to load model capabilities: " + err.Error())
	}
	if err := modelgroup.Refresh(); err != nil {
		logger.SysError("failed to load model groups: " + err.Error())
	}
	if config.MCPEnabled {
		if err := mcp.Load(); err != nil {
			logger.SysError("failed to load mcp servers: " + err.Error())
		}
	}
	if !config.LiteMode {
		// 负载计量的残留清理：进程重启前后计数漂移的兜底
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				routing.Cleanup()
				ratelimit.CleanupMemory()
			}
		}()
	}
	if config.LiteMode {
		logger.SysLog("lite mode enabled, background sync & health check are disabled")
	} else {
		if config.ModelSyncEnabled {
			logger.SysLogf("model sync enabled, interval: %d minutes", config.ModelSyncInterval)
		}
		if config.HealthCheckEnabled {
			logger.SysLogf("channel health check enabled, interval: %d minutes", config.HealthCheckInterval)
		}
		modelsync.Start(config.ModelSyncInterval)
		health.Start(config.HealthCheckInterval)
	}

	// Initialize HTTP server
	server := gin.New()
	server.Use(gin.Recovery())
	// This will cause SSE not to work!!!
	//server.Use(gzip.Gzip(gzip.DefaultCompression))
	server.Use(middleware.RequestId())
	server.Use(middleware.Language())
	middleware.SetUpLogger(server)
	// Initialize session store
	store := cookie.NewStore([]byte(config.SessionSecret))
	server.Use(sessions.Sessions("session", store))

	router.SetRouter(server, buildFS)
	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}
	logger.SysLogf("server started on http://localhost:%s", port)
	err = server.Run(":" + port)
	if err != nil {
		logger.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
