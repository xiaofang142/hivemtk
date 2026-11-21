package middleware

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
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

// rateLimitJWT 仅供限流键取租户用（不写 ctx、不改请求语义）。
//
// 为什么限流要自己验签：`RateLimitMiddleware` 挂在**引擎级**，
// 早于各路由组的 `JWTAuthMiddleware`，此刻 `c.Get(TenantContextKey)` 还没有值。
// 若只认 `c.ClientIP()`，SaaS 部署下同一 NAT/反代出口后的**所有租户共用一个桶**：
// 一个租户打出流量，其他租户一起吃 429 —— 隔离缺口。
var rateLimitJWT = utils.NewJWTUtils(utils.DefaultJWTConfig)

// bearerTokenFromRequest 取请求携带的 Bearer 令牌（与 JWTAuthMiddleware 同一取法，
// 含 websocket 的 `?token=` 兜底），取不到返回空串。
func bearerTokenFromRequest(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" && c.GetHeader("Upgrade") == "websocket" {
		if q := c.Query("token"); q != "" {
			authHeader = "Bearer " + q
		}
	}
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

// requestTenantID 从请求令牌里取租户；无令牌/验签失败/令牌无租户维度 ⇒ 0。
//
// 故意不把结果写回 gin/request ctx：本中间件在鉴权之前，往 ctx 里塞租户等于
// 让"未通过鉴权的请求"获得租户绑定，下游 tenancyidx callback 会当真。
func requestTenantID(c *gin.Context) uint {
	tok := bearerTokenFromRequest(c)
	if tok == "" {
		return 0
	}
	claims, err := rateLimitJWT.ParseToken(tok)
	if err != nil || claims == nil {
		return 0
	}
	return claims.TenantID
}

// rateLimitTenantPrefix 返回限流键的租户前缀。
//
//   - off 模式：返回 ""（键形不变，离线单机行为一致；也省掉一次令牌解析）；
//   - on 模式且令牌带租户：返回 "t<id>:"；
//   - on 模式但无令牌/坏令牌/令牌无租户：返回 ""，即沿用 IP 口径。
//
// 最后一档是**显式登记的语义**而不是遗漏：未登录流量本来就没有租户维度，
// 只能按 IP 分桶（和 off 模式一致）。它的后果是"同一出口后的未登录请求共用桶"，
// 这仍需登录才能区分——判据因此是"已登录流量按租户分桶；未登录仍按 IP"。
func rateLimitTenantPrefix(c *gin.Context) string {
	if !tenant.IsMultiTenant() {
		return ""
	}
	tid := requestTenantID(c)
	if tid == 0 {
		return ""
	}
	return fmt.Sprintf("t%d:", tid)
}

// scopedRateLimitKey 限流键 = 租户前缀 + 原有客户端标识。
//
// **唯一**的键构造处：全局限流、路径配额、监控读数三条路都调它，
// 分开各拼一次正是"两处实现、其中一处漏租户"这类缺陷的来源。
func scopedRateLimitKey(c *gin.Context, clientKey string) string {
	return rateLimitTenantPrefix(c) + clientKey
}

// scopedRateLimitKeyFromCtx 同 scopedRateLimitKey，但只有 ctx（监控读数口没有 gin 上下文）。
func scopedRateLimitKeyFromCtx(ctx context.Context, clientKey string) string {
	if !tenant.IsMultiTenant() {
		return clientKey
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		return clientKey
	}
	return fmt.Sprintf("t%d:", tid) + clientKey
}

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
		callback := IsMerchantCallbackPath(path)
		if !callback {
			if c.GetBool("authenticated") {
				if apiKey := c.GetHeader("X-API-KEY"); apiKey != "" && isValidAppKeyFormat(apiKey) {
					clientKey = "apikey:" + apiKey
				}
			}
			// SaaS：已登录流量按租户分桶（off 模式下前缀恒为空串，键形不变）
			clientKey = scopedRateLimitKey(c, clientKey)
		}

		if !globalRateLimiter.allow(c.Request.Context(), clientKey, callback) {
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
	return rl.allow(ctx, clientKey, false)
}

func (rl *RateLimiter) allow(ctx context.Context, clientKey string, metadataOnly bool) bool {
	if !cache.GlobalIsRedis() {
		return rl.getLimiter(clientKey).Allow()
	}
	c := cache.GetGlobalCache()
	minute := time.Now().Truncate(time.Minute).Unix()
	key := fmt.Sprintf("mtk:ratelimit:%s:%d", clientKey, minute)
	cur, err := c.Incr(ctx, key, time.Minute)
	if err != nil {
		if !metadataOnly {
			logger.Warnf("[ratelimit] 计数后端异常，放行 client=%s: %v", clientKey, err)
		}
		return true
	}
	return cur <= int64(rl.config.RPS*60)
}

// GetRateLimitStatus 读取配额状态（与 Allow 的键域一致）。
// 返回值：
//   - remaining: 剩余可用令牌数（向下取整）。-1 表示限流器未初始化。
//   - resetAfter: 桶填满所需秒数（0 表示已满或限流器未初始化）。
//
// ctx 用于取租户维度，必须与 `RateLimitMiddleware` 用的是同一个租户：
// 调用方传 `tenant.WithTenantID(ctx, tid)` 才会读到该租户的桶，
// 传原始 ctx（无租户）读到的是 IP 口径的桶 —— 键函数与写入侧同一处（scopedRateLimitKeyFromCtx），
// 不存在"读的桶和写的桶不是同一个"。
func GetRateLimitStatus(ctx context.Context, clientKey string) (remaining int, resetAfter float64) {
	if globalRateLimiter == nil {
		return -1, 0
	}
	clientKey = scopedRateLimitKeyFromCtx(ctx, clientKey)

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
	curStr, err := cache.GetGlobalCache().Get(ctx, key)
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
