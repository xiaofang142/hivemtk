package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

var bruteForceDisabled = os.Getenv("BRUTE_FORCE_DISABLED") == "1" || os.Getenv("BRUTE_FORCE_DISABLED") == "true"

type bruteForceEntry struct {
	failures    int
	firstAt     time.Time
	lockedUntil time.Time
}

type bruteForceProtector struct {
	mu      sync.RWMutex
	entries map[string]*bruteForceEntry
}

var globalBruteForce = &bruteForceProtector{
	entries: make(map[string]*bruteForceEntry),
}

var (
	bruteForceJanitorOnce sync.Once
	bruteForceJanitorStop = make(chan struct{})
)

// StopBruteForceJanitor 停止清理协程（优雅退出 / 测试清理用，重复调用安全）
func StopBruteForceJanitor() {
	select {
	case <-bruteForceJanitorStop:
	default:
		close(bruteForceJanitorStop)
	}
}

func startBruteForceJanitor() {
	bruteForceJanitorOnce.Do(func() {
		// 进协程前把包级注入点快照成本地值（协程体不得直读会被测试改写的
		// globalBruteForce；审计门「协程体不得直读测试会改写的包级注入点」）
		p := globalBruteForce
		utils.SafeGo(context.Background(), "brute_force.janitor", func(ctx context.Context) {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-bruteForceJanitorStop:
					return
				case <-ticker.C:
				}
				now := time.Now()
				p.mu.Lock()
				p.cleanupStaleLocked(now)
				p.mu.Unlock()
			}
		})
	})
}

// cleanupStaleLocked 清理死条目（调用方持锁）。
//
// MA-05：修前条件为 `failures == 0 && 超锁`，而 RecordBruteForceFailure 创建即
// failures≥1、窗口重置在同调用内立即 ++ ⇒ failures==0 的条目实际不存在，
// janitor 恒不删，entries map 只增不减（每 IP 失败 ≤4 次即永久驻留，
// IPv6 扫描长驻进程可持续放大）。口径对齐同包 RateLimiter/pathQuota 的
// lastSeen 清理：**锁已过期且最后活动超窗**的条目即死条目（下次尝试的窗口
// 重置语义与本删除等价）。
func (p *bruteForceProtector) cleanupStaleLocked(now time.Time) {
	window := DefaultBruteForceConfig.Window
	for k, e := range p.entries {
		if now.Sub(e.firstAt) > window && now.After(e.lockedUntil) {
			delete(p.entries, k)
		}
	}
}

// BruteForceLockCallback 锁定时触发的回调
// 用于在锁定时把告警写入数据库 / 推送通知
// endpoint: 触发锁定的端点（如 "auth.login"）
// retryAfter: 剩余锁定秒数
type BruteForceLockCallback func(c *gin.Context, endpoint string, retryAfter int)

var globalBruteForceOnLock BruteForceLockCallback

// SetBruteForceLockCallback 注册锁定回调
// 由 service 层调用（避免 middleware → service 循环依赖）
func SetBruteForceLockCallback(cb BruteForceLockCallback) {
	globalBruteForceOnLock = cb
}

// BruteForceConfig 防爆破配置
type BruteForceConfig struct {
	Endpoint     string
	Window       time.Duration
	MaxFailures  int
	LockDuration time.Duration
}

// DefaultBruteForceConfig 默认防爆破配置
var DefaultBruteForceConfig = BruteForceConfig{
	Window:       15 * time.Minute,
	MaxFailures:  5,
	LockDuration: 30 * time.Minute,
}

// BruteForceGuard 防爆破中间件
// 用法：
//
//	auth.POST("/auth/login", middleware.BruteForceGuard("auth.login"), controller.Login)
//	控制器中失败时调用：middleware.RecordBruteForceFailure(c, "auth.login")
//	控制器中成功时调用：middleware.ClearBruteForceFailure(c, "auth.login")
//
// 职责仅限"判定是否已锁定"：真实计数 / 加锁由 RecordBruteForceFailure（控制器
// 失败时调用）单点维护，避免计数放在两条路径上导致实际触发阈值减半、
// 行为与配置的 MaxFailures 不一致。
func BruteForceGuard(endpoint string) gin.HandlerFunc {
	startBruteForceJanitor()
	return func(c *gin.Context) {
		if bruteForceDisabled {
			c.Next()
			return
		}
		clientKey := c.ClientIP() + "|" + endpoint

		globalBruteForce.mu.RLock()
		entry, exists := globalBruteForce.entries[clientKey]
		var lockedUntil time.Time
		if exists {
			now := time.Now()
			if !entry.lockedUntil.IsZero() && now.Before(entry.lockedUntil) {
				lockedUntil = entry.lockedUntil
			}
		}
		globalBruteForce.mu.RUnlock()

		if !lockedUntil.IsZero() {
			retryAfter := int(time.Until(lockedUntil).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", itoa(retryAfter))
			if globalBruteForceOnLock != nil {
				globalBruteForceOnLock(c, endpoint, retryAfter)
			}
			response.Error(c, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RecordBruteForceFailure 记录一次失败
// 在 controller 中检测到失败时调用
func RecordBruteForceFailure(c *gin.Context, endpoint string) {
	recordBruteForceFailureByKey(c.ClientIP() + "|" + endpoint)
}

// recordBruteForceFailureByKey 计数/加锁单点（IP 维度与账号维度共用一套阈值与退避）。
func recordBruteForceFailureByKey(key string) {
	cfg := DefaultBruteForceConfig
	now := time.Now()

	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()

	entry, exists := globalBruteForce.entries[key]
	if !exists {
		entry = &bruteForceEntry{firstAt: now}
		globalBruteForce.entries[key] = entry
	}

	if now.Sub(entry.firstAt) > cfg.Window {
		entry.failures = 0
		entry.firstAt = now
		entry.lockedUntil = time.Time{}
	}

	entry.failures++

	if entry.failures >= cfg.MaxFailures {
		multiplier := 1
		switch {
		case entry.failures >= cfg.MaxFailures*8:
			multiplier = 8
		case entry.failures >= cfg.MaxFailures*4:
			multiplier = 4
		case entry.failures >= cfg.MaxFailures*2:
			multiplier = 2
		}
		entry.lockedUntil = now.Add(time.Duration(multiplier) * cfg.LockDuration)
	}
}

// bruteForceAccountKey 账号维度的桶键。
//
// 2026-11-15 审计 M-4：此前桶键只有 IP 维度，公网代理池/IPv6 大段轮换源地址可对
// admin 等已知账号无限速在线爆破（每 IP 计数永远到不了阈值，LoginRisk 只事后告警
// 不阻断）。账号维度与 IP 维度独立计数、共享同一套阈值/退避，任一命中即拒。
// username 归一（trim+lower）防「Admin/admin 」变体绕开同一桶；不存在的用户名同样
// 记账——既不泄露「账号是否存在」，也让按用户名字典的探测被限速。
func bruteForceAccountKey(endpoint, username string) string {
	return "account|" + endpoint + "|" + strings.ToLower(strings.TrimSpace(username))
}

// CheckBruteForceAccountLocked 账号维度锁定检查（公开路由的 handler 在解析出
// username 后调用，如 auth.login / auth.mfa；命中即 429，与 IP 维度同文案同档）。
func CheckBruteForceAccountLocked(endpoint, username string) (bool, int) {
	if bruteForceDisabled {
		return false, 0
	}
	globalBruteForce.mu.RLock()
	entry, exists := globalBruteForce.entries[bruteForceAccountKey(endpoint, username)]
	locked := false
	var lockedUntil time.Time
	if exists && !entry.lockedUntil.IsZero() && time.Now().Before(entry.lockedUntil) {
		locked = true
		lockedUntil = entry.lockedUntil
	}
	globalBruteForce.mu.RUnlock()

	if !locked {
		return false, 0
	}
	retryAfter := int(time.Until(lockedUntil).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	return true, retryAfter
}

// RecordBruteForceAccountFailure 记录一次账号维度的失败（登录口令错等）。
func RecordBruteForceAccountFailure(endpoint, username string) {
	if bruteForceDisabled {
		return
	}
	recordBruteForceFailureByKey(bruteForceAccountKey(endpoint, username))
}

// ClearBruteForceAccountFailure 登录成功时清除该账号的失败计数。
func ClearBruteForceAccountFailure(endpoint, username string) {
	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()
	delete(globalBruteForce.entries, bruteForceAccountKey(endpoint, username))
}

// ClearBruteForceFailure 清除失败计数（成功时调用）
func ClearBruteForceFailure(c *gin.Context, endpoint string) {
	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()
	delete(globalBruteForce.entries, c.ClientIP()+"|"+endpoint)
}

// IsBruteForceLocked 检查指定 IP+端点是否处于锁定状态
// 用于 service 层在写入 login_events 时附加风险信息
func IsBruteForceLocked(ip, endpoint string) (bool, time.Duration) {
	clientKey := ip + "|" + endpoint
	globalBruteForce.mu.RLock()
	defer globalBruteForce.mu.RUnlock()
	entry, exists := globalBruteForce.entries[clientKey]
	if !exists {
		return false, 0
	}
	now := time.Now()
	if entry.lockedUntil.IsZero() || !now.Before(entry.lockedUntil) {
		return false, 0
	}
	return true, time.Until(entry.lockedUntil)
}

// GetBruteForceFailureCount 获取当前失败次数（用于风险评估）
func GetBruteForceFailureCount(ip, endpoint string) int {
	clientKey := ip + "|" + endpoint
	globalBruteForce.mu.RLock()
	defer globalBruteForce.mu.RUnlock()
	entry, exists := globalBruteForce.entries[clientKey]
	if !exists {
		return 0
	}
	return entry.failures
}

// ResetBruteForceForTest 重置所有防爆破状态（仅用于测试）
func ResetBruteForceForTest() {
	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()
	globalBruteForce.entries = make(map[string]*bruteForceEntry)
}

// BruteForceDisabledForTest 显式查询禁用开关（仅用于测试环境自适应跳过）
func BruteForceDisabledForTest() bool {
	return bruteForceDisabled
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := false
	if i < 0 {
		negative = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
