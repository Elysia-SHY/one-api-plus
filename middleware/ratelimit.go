package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Elysia-SHY/one-api-plus/common/config"
	"github.com/Elysia-SHY/one-api-plus/common/ctxkey"
	"github.com/Elysia-SHY/one-api-plus/common/logger"
	"github.com/Elysia-SHY/one-api-plus/service/ratelimit"
)

// RateLimit 对中继请求做令牌桶限流 + 并发闸门。
//
// 挂在 TokenAuth 之后：需要 ctxkey.Id 才能按用户分桶。
// 被拒绝时返回 429，并带上标准的 RateLimit-* 响应头，让客户端可以退避重试。
func RateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if !config.RateLimitEnabled {
			c.Next()
			return
		}
		userId := c.GetInt(ctxkey.Id)
		modelName := c.GetString(ctxkey.RequestModel)
		key := fmt.Sprintf("user:%d", userId)
		if tokenId := c.GetInt(ctxkey.TokenId); tokenId > 0 {
			key = fmt.Sprintf("token:%d", tokenId)
		}
		result := ratelimit.Allow(key, modelName)
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
		if !result.Allowed {
			c.Header("Retry-After", fmt.Sprintf("%d", maxInt(result.RetryAfter, 1)))
			logger.Warnf(ctx, "rate limit exceeded for %s (model %s)", key, modelName)
			abortWithOpenAIError(c, http.StatusTooManyRequests,
				fmt.Sprintf("请求过于频繁，请稍后再试（每分钟上限 %d）", result.Limit), "rate_limit_exceeded")
			return
		}
		// 并发闸门
		if config.RateLimitConcurrent > 0 {
			concKey := fmt.Sprintf("conc:%s", key)
			if !ratelimit.AcquireConcurrency(concKey, config.RateLimitConcurrent) {
				c.Header("Retry-After", "1")
				logger.Warnf(ctx, "concurrency limit exceeded for %s", key)
				abortWithOpenAIError(c, http.StatusTooManyRequests,
					fmt.Sprintf("并发请求数超过上限 %d", config.RateLimitConcurrent), "concurrency_limit_exceeded")
				return
			}
			defer ratelimit.ReleaseConcurrency(concKey)
			c.Next()
			return
		}
		c.Next()
	}
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// abortWithOpenAIError 以 OpenAI 的错误形态返回，兼容所有 SDK
func abortWithOpenAIError(c *gin.Context, status int, message string, code string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "one_api_plus_error",
			"code":    code,
		},
	})
	c.Abort()
}
