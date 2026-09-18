package middleware

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	RPS         float64
	BucketSize  int
	Enabled     bool
	ExemptPaths []string
}

// DefaultRateLimitConfig 默认限流配置
var DefaultRateLimitConfig = RateLimitConfig{
	RPS:        10,
	BucketSize: 100,
	Enabled:    true,
}

// ClientLimiter 客户端限流器
type ClientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter 全局限流器
type RateLimiter struct {
	mu       sync.RWMutex
	clients  map[string]*ClientLimiter
	config   RateLimitConfig
	cleanup  time.Duration
	stopChan chan struct{}
}

func isValidAppKeyFormat(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		isSafe := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-'
		if !isSafe {
			return false
		}
	}
	return true
}

var globalRateLimiter *RateLimiter

// InitRateLimiter 初始化全局限流器
func InitRateLimiter(config RateLimitConfig) {
	if config.RPS <= 0 {
		config.RPS = DefaultRateLimitConfig.RPS
	}
	if config.BucketSize <= 0 {
		config.BucketSize = DefaultRateLimitConfig.BucketSize
	}

	globalRateLimiter = &RateLimiter{
		clients:  make(map[string]*ClientLimiter),
		config:   config,
		cleanup:  5 * time.Minute,
		stopChan: make(chan struct{}),
	}

	go globalRateLimiter.cleanupLoop()
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanupClients()
		case <-rl.stopChan:
			return
		}
	}
}

func (rl *RateLimiter) cleanupClients() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, client := range rl.clients {
		if now.Sub(client.lastSeen) > 30*time.Minute {
			delete(rl.clients, ip)
		}
	}
}

func (rl *RateLimiter) getLimiter(clientKey string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if client, exists := rl.clients[clientKey]; exists {
		client.lastSeen = time.Now()
		return client.limiter
	}

	limiter := rate.NewLimiter(rate.Limit(rl.config.RPS), rl.config.BucketSize)
	rl.clients[clientKey] = &ClientLimiter{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	return limiter
}

// Stop 停止限流器
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// RateLimitMiddleware 限流中间件
// 基于 IP 地址进行限流，防止 DDoS 和暴力请求
func RateLimitMiddleware(config ...RateLimitConfig) gin.HandlerFunc {
	cfg := DefaultRateLimitConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	if !cfg.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	InitRateLimiter(cfg)

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range cfg.ExemptPaths {
			if p == path || strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		clientKey := c.ClientIP()
		if authVal, exists := c.Get("authenticated"); exists {
			if isAuthenticated, ok := authVal.(bool); ok && isAuthenticated {
				if apiKey := c.GetHeader("X-API-KEY"); apiKey != "" && isValidAppKeyFormat(apiKey) {
					clientKey = "apikey:" + apiKey
				}
			}
		}

		if !globalRateLimiter.Allow(c.Request.Context(), clientKey) {
			c.JSON(429, gin.H{
				"code":        429,
				"msg":         "请求过于频繁，请稍后再试",
				"retry_after": 5,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func (rl *RateLimiter) Allow(ctx context.Context, clientKey string) bool {
	if !cache.GlobalIsRedis() {
		return rl.getLimiter(clientKey).Allow()
	}
	c := cache.GetGlobalCache()
	minute := time.Now().Truncate(time.Minute).Unix()
	key := fmt.Sprintf("mtk:ratelimit:%s:%d", clientKey, minute)
	cur, err := c.Incr(ctx, key, time.Minute)
	if err != nil {
		logger.Warnf("[ratelimit] 计数后端异常，放行 client=%s: %v", clientKey, err)
		return true
	}
	return cur <= int64(rl.config.RPS*60)
}

// GetRateLimitStatus 获取限流状态（用于监控）
// 返回值：
//   - remaining: 剩余可用令牌数（向下取整）。-1 表示限流器未初始化。
//   - resetAfter: 桶填满所需秒数（0 表示已满或限流器未初始化）。
func GetRateLimitStatus(clientKey string) (remaining int, resetAfter float64) {
	if globalRateLimiter == nil {
		return -1, 0
	}

	if !cache.GlobalIsRedis() {
		limiter := globalRateLimiter.getLimiter(clientKey)
		tokens := limiter.Tokens()
		remaining = int(tokens)
		if remaining < 0 {
			remaining = 0
		}
		burst := float64(limiter.Burst())
		limit := float64(limiter.Limit())
		if limit <= 0 {
			return remaining, 0
		}
		deficit := burst - tokens
		if deficit <= 0 {
			return remaining, 0
		}
		resetAfter = deficit / limit
		return remaining, resetAfter
	}

	minute := time.Now().Truncate(time.Minute).Unix()
	key := fmt.Sprintf("mtk:ratelimit:%s:%d", clientKey, minute)
	curStr, err := cache.GetGlobalCache().Get(context.Background(), key)
	if err != nil || curStr == "" {
		return int(globalRateLimiter.config.RPS * 60), 0
	}
	var cur int64
	_, _ = fmt.Sscanf(curStr, "%d", &cur)
	rem := int64(globalRateLimiter.config.RPS*60) - cur
	if rem < 0 {
		rem = 0
	}
	return int(rem), 0
}
