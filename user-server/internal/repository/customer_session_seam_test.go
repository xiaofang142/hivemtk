package repository

import (
	"testing"
	"time"
)

// resetSessionActiveTTLProvider 把读取口复位到「未注入」。
// 生产 setter 的 nil 语义是「不注入」（防装配顺序错时把兜底值顶掉），
// 所以它无法用来复位 —— 测试里同包，直接写包内变量。
func resetSessionActiveTTLProvider() {
	sessionActiveTTLProvider = func() time.Duration { return DefaultSessionActiveTTL }
}

// TestSessionActiveTTLSeam 逐格钉死 session.active_ttl 的读取口。
func TestSessionActiveTTLSeam(t *testing.T) {
	t.Cleanup(resetSessionActiveTTLProvider)

	t.Run("未注入时等于种子默认值", func(t *testing.T) {
		resetSessionActiveTTLProvider()
		if got := SessionActiveTTL(); got != DefaultSessionActiveTTL {
			t.Fatalf("未注入时 = %s, 期望兜底 %s", got, DefaultSessionActiveTTL)
		}
	})

	t.Run("注入生效", func(t *testing.T) {
		SetSessionActiveTTLProvider(func() time.Duration { return 2 * time.Hour })
		if got := SessionActiveTTL(); got != 2*time.Hour {
			t.Fatalf("注入 2h 后 = %s", got)
		}
	})

	t.Run("非正值回落兜底", func(t *testing.T) {
		for _, bad := range []time.Duration{0, -time.Hour} {
			SetSessionActiveTTLProvider(func() time.Duration { return bad })
			if got := SessionActiveTTL(); got != DefaultSessionActiveTTL {
				t.Fatalf("注入 %s 时 = %s, 期望回落 %s", bad, got, DefaultSessionActiveTTL)
			}
		}
	})

	t.Run("setter 传 nil 是空操作", func(t *testing.T) {
		SetSessionActiveTTLProvider(func() time.Duration { return 2 * time.Hour })
		SetSessionActiveTTLProvider(nil)
		if got := SessionActiveTTL(); got != 2*time.Hour {
			t.Fatalf("传 nil 后 = %s，期望仍是被注入的 2h（nil 语义是「不注入」，不是「复位」）", got)
		}
	})
}

// TestSessionActiveTTLKeepsServiceSingleSource 钉住「service 侧的 const 仍然初始化自
// repository 的兜底常量」这条单一源不变式。装配层只在 repository 侧注入，service 侧
// 必须读 repository.SessionActiveTTL() 而不是自己那份，否则改一边另一边不动的分叉
// 会重新长出来。
func TestSessionActiveTTLKeepsServiceSingleSource(t *testing.T) {
	if DefaultSessionActiveTTL != 24*time.Hour {
		t.Fatalf("兜底常量被改成了 %s，种子 session.active_ttl 的默认值是 86400 秒（24h）", DefaultSessionActiveTTL)
	}
}
