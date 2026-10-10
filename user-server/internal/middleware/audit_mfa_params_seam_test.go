package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func resetMiddlewareParamProviders() {
	auditFlushIntervalProvider = func() time.Duration { return DefaultAuditFlushInterval }
	mfaRecentVerifyTTLProvider = func() time.Duration { return DefaultMfaRecentVerifyTTL }
}

func TestAuditFlushIntervalSeam(t *testing.T) {
	resetMiddlewareParamProviders()
	t.Cleanup(resetMiddlewareParamProviders)

	t.Run("未注入时返回兜底值", func(t *testing.T) {
		require.Equal(t, 5*time.Second, AuditFlushInterval())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetAuditFlushIntervalProvider(func() time.Duration { return 30 * time.Second })
		require.Equal(t, 30*time.Second, AuditFlushInterval())
	})

	t.Run("注入非法值回落兜底（time.NewTicker 非正数会 panic）", func(t *testing.T) {
		SetAuditFlushIntervalProvider(func() time.Duration { return 0 })
		require.Equal(t, DefaultAuditFlushInterval, AuditFlushInterval())
	})

	t.Run("setter 传 nil 是空操作而非复位", func(t *testing.T) {
		SetAuditFlushIntervalProvider(func() time.Duration { return 30 * time.Second })
		SetAuditFlushIntervalProvider(nil)
		require.Equal(t, 30*time.Second, AuditFlushInterval(),
			"传 nil 应该什么都不做（防装配顺序错时把兜底值顶掉）")
	})
}

func TestMFARecentVerifyTTLSeam(t *testing.T) {
	resetMiddlewareParamProviders()
	t.Cleanup(resetMiddlewareParamProviders)

	t.Run("未注入时返回兜底值", func(t *testing.T) {
		require.Equal(t, 5*time.Minute, MFARecentVerifyTTL())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetMfaRecentVerifyTTLProvider(func() time.Duration { return 30 * time.Minute })
		require.Equal(t, 30*time.Minute, MFARecentVerifyTTL())
	})

	t.Run("注入非法值回落兜底", func(t *testing.T) {
		SetMfaRecentVerifyTTLProvider(func() time.Duration { return -1 })
		require.Equal(t, DefaultMfaRecentVerifyTTL, MFARecentVerifyTTL())
	})

	t.Run("setter 传 nil 是空操作而非复位", func(t *testing.T) {
		SetMfaRecentVerifyTTLProvider(func() time.Duration { return 30 * time.Minute })
		SetMfaRecentVerifyTTLProvider(nil)
		require.Equal(t, 30*time.Minute, MFARecentVerifyTTL())
	})
}

// TestRecentVerifyHonorsConfiguredTTL 行为级断言：宽限期被拉长后，刚过 MFA 的用户
// 在更长时间内仍被判定为「最近验证过」，证明读取点真的走了 seam 而不是只测读取函数本身。
func TestRecentVerifyHonorsConfiguredTTL(t *testing.T) {
	resetMiddlewareParamProviders()
	t.Cleanup(resetMiddlewareParamProviders)

	MarkMFAVerified(90001)
	t.Cleanup(func() { clearRecentVerifyForTest(90001) })

	require.True(t, IsMFAVerifiedRecently(90001), "刚验证过应立即免二次验证")

	SetMfaRecentVerifyTTLProvider(func() time.Duration { return 24 * time.Hour })
	require.True(t, IsMFAVerifiedRecently(90001), "宽限期拉长后仍应免二次验证")

	SetMfaRecentVerifyTTLProvider(func() time.Duration { return time.Nanosecond })
	require.False(t, IsMFAVerifiedRecently(90001), "宽限期收到纳秒级后应立即要求二次验证")
}

// clearRecentVerifyForTest 只清内存 map；Redis 分支由 cache 自身的 TTL 管。
func clearRecentVerifyForTest(userID uint) {
	mfaRecentVerifyMutex.Lock()
	delete(mfaRecentVerify, userID)
	mfaRecentVerifyMutex.Unlock()
}
