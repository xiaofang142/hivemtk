package middleware

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// PathQuotaConfig 单路径限流配置
type PathQuotaConfig struct {
	Path       string  `json:"path"`
	RPS        float64 `json:"rps"`
	BucketSize int     `json:"bucket_size"`
}

// DefaultPathQuota 默认路径级配额
var DefaultPathQuota = map[string]PathQuotaConfig{
	"/api/integrations/external-orders-by-customer": {RPS: 5, BucketSize: 20},
	"/api/ai-suggestions":                           {RPS: 3, BucketSize: 10},
	"/api/monitor/trace-eval/trigger":               {RPS: 1, BucketSize: 5},
	"/api/reach":                                    {RPS: 50, BucketSize: 500},
}

type quotaUsageSnapshot struct {
	Path            string     `json:"path"`
	ConfiguredRPS   float64    `json:"configured_rps"`
	ConfiguredBurst int        `json:"configured_burst"`
	CurrentMinute   int64      `json:"current_minute"`
	Used            int64      `json:"used"`
	Remaining       int64      `json:"remaining"`
	Triggered       int64      `json:"triggered"`
	LastTriggered   *time.Time `json:"last_triggered,omitempty"`
}

// PathQuotaMiddleware 路径级限流中间件
// 复用 globalRateLimiter 底层的 Redis/令牌桶，额外支持对"超过配额"事件打点到 api_rate_logs
func PathQuotaMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		cfg, ok := DefaultPathQuota[path]
		if !ok {
			c.Next()
			return
		}
		clientKey := "path:" + path
		if !globalRateLimiter.Allow(c.Request.Context(), clientKey) {

			go recordQuotaTrigger(path, c.ClientIP())
			c.JSON(429, gin.H{
				"code":        429,
				"msg":         "该接口请求过于频繁，请稍后再试",
				"path":        path,
				"rps":         cfg.RPS,
				"retry_after": 1,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func recordQuotaTrigger(path, clientIP string) {
	if !cache.GlobalIsRedis() {
		return
	}
	c := cache.GetGlobalCache()

	key := "mtk:ratelimit:triggered:" + path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Incr(ctx, key, 24*time.Hour); err != nil {
		logger.Debugf("[ratelimit] 记录触发事件失败 path=%s: %v", path, err)
	}
}

func GetQuotaSnapshots() []quotaUsageSnapshot {
	result := make([]quotaUsageSnapshot, 0, len(DefaultPathQuota))
	minute := time.Now().Truncate(time.Minute).Unix()
	isRedis := cache.GlobalIsRedis()

	for path, cfg := range DefaultPathQuota {
		snap := quotaUsageSnapshot{
			Path:            path,
			ConfiguredRPS:   cfg.RPS,
			ConfiguredBurst: cfg.BucketSize,
			CurrentMinute:   minute,
		}

		if isRedis {
			c := cache.GetGlobalCache()
			key := "mtk:ratelimit:path:" + path + ":" + fmt.Sprintf("%d", minute)
			val, err := c.Get(context.Background(), key)
			if err == nil && val != "" {
				var used int64
				_, _ = fmt.Sscanf(val, "%d", &used)
				snap.Used = used
			}

			triggeredKey := "mtk:ratelimit:triggered:" + path
			tv, err := c.Get(context.Background(), triggeredKey)
			if err == nil && tv != "" {
				var triggered int64
				_, _ = fmt.Sscanf(tv, "%d", &triggered)
				snap.Triggered = triggered
			}
		} else if globalRateLimiter != nil {
			rem, _ := GetRateLimitStatus("path:" + path)
			snap.Remaining = int64(rem)
		}
		result = append(result, snap)
	}
	return result
}
