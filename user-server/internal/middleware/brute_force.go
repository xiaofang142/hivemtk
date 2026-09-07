package middleware

import (
	"net/http"
	"os"
	"sync"
	"time"

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
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-bruteForceJanitorStop:
					return
				case <-ticker.C:
				}
				now := time.Now()
				globalBruteForce.mu.Lock()
				for k, e := range globalBruteForce.entries {
					if e.failures == 0 && now.After(e.lockedUntil) {
						delete(globalBruteForce.entries, k)
					}
				}
				globalBruteForce.mu.Unlock()
			}
		}()
	})
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
//	auth.POST("/license/bind", middleware.BruteForceGuard("license.bind"), controller.BindLicense)
//	控制器中失败时调用：middleware.RecordBruteForceFailure(c, "license.bind")
//	控制器中成功时调用：middleware.ClearBruteForceFailure(c, "license.bind")
//
// # BruteForceGuard 防爆破守卫前置检查
//
// 职责仅限"判定是否已锁定"，避免把计数放在两条路径上引起歧义。
// 真实计数 / 加锁由 RecordBruteForceFailure（控制器失败时调用）单点维护。
//
// 修复：原实现同时在 Guard 与 RecordBruteForceFailure 中自增失败计数，
//  1. 同一 entry 在两条路径上各 ++ 一次，实际触发阈值 = MaxFailures/2（实测 3 次失败即锁），
//     行为与配置的 MaxFailures=5 不一致；
//  2. Guard 内的 `failures = 0` 复位逻辑在并发序列下是死代码（被 RecordBruteForceFailure
//     抢先触发锁定），且 `getBruteForceEntry` 在 entry 不存在时返回临时 struct，
//     自增后被丢弃 — 对从未失败过的成功请求是浪费 + 误导。
//  3. 调用点唯一：`/api/auth/login`，单点维护更清晰。
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
	cfg := DefaultBruteForceConfig
	clientKey := c.ClientIP() + "|" + endpoint
	now := time.Now()

	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()

	entry, exists := globalBruteForce.entries[clientKey]
	if !exists {
		entry = &bruteForceEntry{firstAt: now}
		globalBruteForce.entries[clientKey] = entry
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

// ClearBruteForceFailure 清除失败计数（成功时调用）
func ClearBruteForceFailure(c *gin.Context, endpoint string) {
	clientKey := c.ClientIP() + "|" + endpoint
	globalBruteForce.mu.Lock()
	defer globalBruteForce.mu.Unlock()
	delete(globalBruteForce.entries, clientKey)
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
