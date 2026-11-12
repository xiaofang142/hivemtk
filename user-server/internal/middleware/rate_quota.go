package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// PathQuotaConfig 单路径限流配置
type PathQuotaConfig struct {
	Path       string  `json:"path"`
	RPS        float64 `json:"rps"`
	BucketSize int     `json:"bucket_size"`
}

// DefaultPathQuota 默认路径级配额
//
// 参数中心 ratelimit.path_quotas 可整份覆写这张表（每请求热读，见 resolvePathQuotas）。
// 两者保持同源：种子 DefaultValue 就是本表的 JSON 序列化（键序按 Go map 序列化可能
// 漂移，语义等价即可），条数与键集合由本包用例对账。
var DefaultPathQuota = map[string]PathQuotaConfig{
	"/api/integrations/external-orders-by-customer": {RPS: 5, BucketSize: 20},
	"/api/ai-suggestions":                           {RPS: 3, BucketSize: 10},
	"/api/monitor/trace-eval/trigger":               {RPS: 1, BucketSize: 5},
	"/api/reach":                                    {RPS: 50, BucketSize: 500},
}

// pathQuotaEntry JSON 覆写值的单条形状：路径做 map key，值只带两个数值字段。
// 不直接 Unmarshal 进 map[string]PathQuotaConfig 是因为那个结构体的 Path 字段
// 会与 map key 重复、还能写出两者不一致的行；这里让 key 唯一定义路径，解析后回填。
type pathQuotaEntry struct {
	RPS        float64 `json:"rps"`
	BucketSize int     `json:"bucket_size"`
}

// resolvePathQuotas 返回当前生效的路径级配额表。
//
// 读取口径（ratelimit.path_quotas，每请求热读——参数中心有 60s 内存缓存，成本可控）：
//   - 参数未装配 / 值为空串 → DefaultPathQuota（行为与接线前一致）；
//   - 整份解析失败 → Warn 并回落 DefaultPathQuota（写侧 validateValue 已拦非法 JSON，
//     这里再兜一层是防手改库）；
//   - 单条非法（路径不以 / 开头、rps<=0、bucket_size<=0）→ 丢弃该条并 Warn，
//     其余条目照常生效——一条坏数据不应把整张限流表打回默认。
//
// 解析结果不自建缓存：ConfigParamService 的 60s 缓存已经挡住高频 DB 读，
// 再加一层只会引入第二份失效时机（改参后新旧表并存），得不偿失。
func resolvePathQuotas(ctx context.Context) map[string]PathQuotaConfig {
	raw := service.GlobalConfigParam().GetString(ctx, "ratelimit", "path_quotas", "")
	if raw == "" {
		return DefaultPathQuota
	}
	var entries map[string]pathQuotaEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		logger.Warnf("[ratelimit] path_quotas 不是合法 JSON，回落内置默认配额表: %v", err)
		return DefaultPathQuota
	}
	resolved := make(map[string]PathQuotaConfig, len(entries))
	for path, e := range entries {
		if len(path) == 0 || path[0] != '/' || e.RPS <= 0 || e.BucketSize <= 0 {
			logger.Warnf("[ratelimit] path_quotas 含非法条目已丢弃 path=%q rps=%v bucket=%d", path, e.RPS, e.BucketSize)
			continue
		}
		resolved[path] = PathQuotaConfig{Path: path, RPS: e.RPS, BucketSize: e.BucketSize}
	}
	if len(resolved) == 0 {
		// 整份 JSON 存在但一条都没活下来：等价于「限流全关」，比解析失败更危险，
		// 同样回落默认表而不是放行所有路径。
		logger.Warnf("[ratelimit] path_quotas 解析后为空表，回落内置默认配额表")
		return DefaultPathQuota
	}
	return resolved
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

// pathQuotaBucket 单 (客户端 IP × 路径) 令牌桶条目。
// rps/bucketSize 记录构建时参数：参数中心改表后（resolvePathQuotas 热读），
// 与现存桶参数不一致即按新值重建该桶——这就是「改表 60s 内生效」的落点。
type pathQuotaBucket struct {
	limiter    *rate.Limiter
	rps        float64
	bucketSize int
	lastSeen   time.Time
}

// pathQuotaStats 单路径当分钟计数（本进程聚合，非 Redis 部署下给快照读数用）。
type pathQuotaStats struct {
	minute int64
	used   int64
}

// pathQuotaLimiterManager 路径级配额限流管理器。
//
// 桶键 = clientKey + "\x00" + path：**每客户端每路径一个桶**（clientKey 含租户前缀）。
// 做全局共享桶，一个活跃租户就能把 /api/reach 的全站配额打满、所有人吃 429——
// 按客户端分桶后各桶互不影响，配额语义回到「单客户端单路径的速率上限」。
//
// 参数热更新：每请求 resolvePathQuotas 热读（参数中心 60s 缓存），cfg 与桶
// 构建时参数不一致（RPS 或 BucketSize 变了）时按新参数重建该客户端的桶。
//
// 内存上限：活跃 (ip,path) 对数量级可控；与 globalRateLimiter 同款治理——
// 5 分钟 tick 清 30 分钟未见的闲置桶，防止 IP 扫描把 map 撑爆。
type pathQuotaLimiterManager struct {
	mu       sync.Mutex
	buckets  map[string]*pathQuotaBucket
	stats    map[string]*pathQuotaStats
	stopChan chan struct{}
}

func newPathQuotaLimiterManager() *pathQuotaLimiterManager {
	return &pathQuotaLimiterManager{
		buckets:  make(map[string]*pathQuotaBucket),
		stats:    make(map[string]*pathQuotaStats),
		stopChan: make(chan struct{}),
	}
}

var (
	pathQuotaMgrOnce   sync.Once
	globalPathQuotaMgr *pathQuotaLimiterManager
)

// getPathQuotaManager 懒建全局管理器（含清理协程）。测试可整体换掉它（见用例内
// reset helper）；Once.Do 之后每次调用只是原子快路径，热读成本可忽略。
func getPathQuotaManager() *pathQuotaLimiterManager {
	pathQuotaMgrOnce.Do(func() {
		globalPathQuotaMgr = newPathQuotaLimiterManager()
		go globalPathQuotaMgr.cleanupLoop()
	})
	return globalPathQuotaMgr
}

// allow 取 (clientKey, path) 桶并消费一个令牌；桶不存在或构建参数与当前 cfg 不一致时
// （参数热更新）按 cfg 重建。返回 false 表示超出该路径配额。
//
// clientKey 由调用方用 `scopedRateLimitKey` 构造（= 租户前缀 + IP），
// 本方法不碰租户——键的构造只有一处。
func (m *pathQuotaLimiterManager) allow(clientKey, path string, cfg PathQuotaConfig) bool {
	key := clientKey + "\x00" + path
	now := time.Now()
	m.mu.Lock()
	b, ok := m.buckets[key]
	if !ok || b.rps != cfg.RPS || b.bucketSize != cfg.BucketSize {
		b = &pathQuotaBucket{
			limiter:    rate.NewLimiter(rate.Limit(cfg.RPS), cfg.BucketSize),
			rps:        cfg.RPS,
			bucketSize: cfg.BucketSize,
			lastSeen:   now,
		}
		m.buckets[key] = b
	} else {
		b.lastSeen = now
	}
	allowed := b.limiter.Allow()
	// Used 计数含被拒请求（与旧 Redis 分钟计数口径一致：Incr 先于限额判定）。
	minute := now.Truncate(time.Minute).Unix()
	st, ok := m.stats[path]
	if !ok || st.minute != minute {
		st = &pathQuotaStats{minute: minute}
		m.stats[path] = st
	}
	st.used++
	m.mu.Unlock()
	return allowed
}

// pathUsed 返回该路径在指定分钟内的本进程请求数（含被拒）；分钟不匹配返回 0。
func (m *pathQuotaLimiterManager) pathUsed(path string, minute int64) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.stats[path]; ok && st.minute == minute {
		return st.used
	}
	return 0
}

// pathRemaining 聚合该路径所有活跃客户端桶的剩余令牌之和。
// 每客户端分桶后不存在"全路径统一剩余"，监控读数取各桶余量之和。
func (m *pathQuotaLimiterManager) pathRemaining(path string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	suffix := "\x00" + path
	for k, b := range m.buckets {
		if len(k) >= len(suffix) && k[len(k)-len(suffix):] == suffix {
			if tok := b.limiter.Tokens(); tok > 0 {
				total += int64(tok)
			}
		}
	}
	return total
}

func (m *pathQuotaLimiterManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopChan:
			return
		}
	}
}

// cleanup 清 30 分钟未见的闲置桶（防 IP 扫描撑爆 map）与过期分钟计数。
func (m *pathQuotaLimiterManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, b := range m.buckets {
		if now.Sub(b.lastSeen) > 30*time.Minute {
			delete(m.buckets, k)
		}
	}
	cur := now.Truncate(time.Minute).Unix()
	for p, st := range m.stats {
		if st.minute < cur {
			delete(m.stats, p)
		}
	}
}

func (m *pathQuotaLimiterManager) stop() { //nolint:unused //// 仅被 *_test.go 引用，生产路径未用
	close(m.stopChan)
}

// PathQuotaMiddleware 路径级限流中间件
//
// 自过滤：只对 resolvePathQuotas 列出的路径生效，未列入的路径直接放行。
// 计数维度：每客户端 × 每路径一个令牌桶（pathQuotaLimiterManager），桶参数
// 取该路径 cfg 的 RPS/BucketSize——杜绝「全站共享桶，一个活跃租户把所有人打到 429」。
// 「客户端」= 租户前缀 + IP（与全局限流同一个键函数 `scopedRateLimitKey`）：
// SaaS 部署下同一出口后的两个租户各自有桶，不会再互相挤占；
// off 模式下前缀为空串，键形与接线前逐字节一致。
// 参数热读：配额表经参数中心 60s 缓存热读，改表后已有桶在参数变化时按新值重建。
// 超配额：429 + 异步打点 api_rate_logs（recordQuotaTrigger）。
//
// 挂载位置约定（见 router.Setup）：JWT 之前——未登录请求也计数，这是保护昂贵端点
// 的本意，不能让攻击者靠不带 token 绕过配额。因此租户只能从请求自带的令牌推（见
// rateLimitTenantPrefix）；无令牌请求按 IP 分桶，语义显式登记在 A5 判据里。
func PathQuotaMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		cfg, ok := resolvePathQuotas(c.Request.Context())[path]
		if !ok {
			c.Next()
			return
		}
		// Redis 部署下仍按分钟聚合计数写 Redis：GET /api/system/rate-quota 的
		// Used 读数口（快照 Redis 分支）依赖这个键。限流判定本身走本地桶
		// （多实例部署时各自计数，见任务边界说明）。
		// 这个键是**跨租户聚合读数口**（"该路径这一分钟被请求多少次"），刻意不带租户段；
		// 带上之后读数会按租户碎片化，监控面板反而看不出全站用量。
		if cache.GlobalIsRedis() {
			minute := time.Now().Truncate(time.Minute).Unix()
			key := fmt.Sprintf("mtk:ratelimit:path:%s:%d", path, minute)
			if _, err := cache.GetGlobalCache().Incr(c.Request.Context(), key, time.Minute); err != nil {
				logger.Debugf("[ratelimit] 路径用量计数失败 path=%s: %v", path, err)
			}
		}
		clientKey := scopedRateLimitKey(c, c.ClientIP())
		if !getPathQuotaManager().allow(clientKey, path, cfg) {
			// path/clientKey 为请求级稳定值；SafeGo 防 recordQuotaTrigger panic 击穿进程
			utils.SafeGo(context.Background(), "ratelimit.quota_trigger", func(_ context.Context) {
				recordQuotaTrigger(path, clientKey)
			})
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

func recordQuotaTrigger(path, clientKey string) {
	if !cache.GlobalIsRedis() {
		return
	}
	c := cache.GetGlobalCache()

	key := "mtk:ratelimit:triggered:" + path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Incr(ctx, key, 24*time.Hour); err != nil {
		logger.Debugf("[ratelimit] 记录触发事件失败 path=%s client=%s: %v", path, clientKey, err)
	}
}

// GetQuotaSnapshots 读取各路径配额快照（ctx 用于租户键域一致性，T010）
//
// 读数口径：
//   - Redis 部署：Used/Triggered 走 Redis 分钟计数与触发打点（中间件写入，键不变）；
//   - 非 Redis：Used = 本进程该路径当分钟请求数（含被拒，pathQuotaLimiterManager），
//     Remaining = 该路径所有活跃客户端桶的剩余令牌之和（每客户端分桶后不存在
//     "全路径统一剩余"，聚合读数取各桶余量之和，语义自洽）。
func GetQuotaSnapshots(ctx context.Context) []quotaUsageSnapshot {
	quotas := resolvePathQuotas(ctx)
	result := make([]quotaUsageSnapshot, 0, len(quotas))
	minute := time.Now().Truncate(time.Minute).Unix()
	isRedis := cache.GlobalIsRedis()

	for path, cfg := range quotas {
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
		} else {
			mgr := getPathQuotaManager()
			snap.Used = mgr.pathUsed(path, minute)
			snap.Remaining = mgr.pathRemaining(path)
		}
		result = append(result, snap)
	}
	return result
}
