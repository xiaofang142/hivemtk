package mcp

import (
	"testing"
	"time"
)

// resetSessionIdleTTLProvider 把读取口复位到「未注入」。生产 setter 的 nil 语义是
// 「不注入」（防装配顺序错时把兜底值顶掉），所以它无法用来复位 —— 测试里同包，直接写包内变量。
func resetSessionIdleTTLProvider() {
	sessionIdleTTLProvider = func() time.Duration { return DefaultSessionIdleTTL }
}

// TestSessionIdleTTLSeam 逐格钉死 session.idle_ttl 的读取口。
func TestSessionIdleTTLSeam(t *testing.T) {
	t.Cleanup(resetSessionIdleTTLProvider)
	resetSessionIdleTTLProvider()

	if got := SessionIdleTTL(); got != DefaultSessionIdleTTL {
		t.Fatalf("未注入 = %s, 期望兜底 %s", got, DefaultSessionIdleTTL)
	}
	SetSessionIdleTTLProvider(func() time.Duration { return 90 * time.Second })
	if got := SessionIdleTTL(); got != 90*time.Second {
		t.Fatalf("注入 90s 后 = %s", got)
	}
	for _, bad := range []time.Duration{0, -time.Second} {
		SetSessionIdleTTLProvider(func() time.Duration { return bad })
		if got := SessionIdleTTL(); got != DefaultSessionIdleTTL {
			t.Fatalf("注入 %s 时 = %s, 期望回落 %s", bad, got, DefaultSessionIdleTTL)
		}
	}
	// setter 的 nil 语义是「不注入」而非「复位」，所以这里断言它是空操作。
	SetSessionIdleTTLProvider(func() time.Duration { return 90 * time.Second })
	SetSessionIdleTTLProvider(nil)
	if got := SessionIdleTTL(); got != 90*time.Second {
		t.Fatalf("传 nil 后 = %s，期望仍是被注入的 90s", got)
	}
}

// TestStoreSessionDropsExpiredByConfiguredTTL 证明读取口真进了 storeSession 的清理判定：
// 空闲超时的会话被删，仍在 TTL 之内的同一条必须留着。
func TestStoreSessionDropsExpiredByConfiguredTTL(t *testing.T) {
	t.Cleanup(resetSessionIdleTTLProvider)

	h := &HTTPHandler{sessions: map[string]*mcpHTTPSession{}}

	// 上一条会话 lastSeen 在 10 分钟前。
	h.sessions["old"] = &mcpHTTPSession{lastSeen: time.Now().Add(-10 * time.Minute)}

	SetSessionIdleTTLProvider(func() time.Duration { return 5 * time.Minute })
	h.storeSession("new", nil)

	if _, ok := h.sessions["old"]; ok {
		t.Fatal("空闲 10 分钟 > 配置的 5 分钟，应当被清理掉")
	}
	if _, ok := h.sessions["new"]; !ok {
		t.Fatal("刚写入的会话不应被清理")
	}

	// TTL 放宽到 30 分钟后，同一条旧会话应当留下。
	h.sessions["old"] = &mcpHTTPSession{lastSeen: time.Now().Add(-10 * time.Minute)}
	SetSessionIdleTTLProvider(func() time.Duration { return 30 * time.Minute })
	h.storeSession("newer", nil)
	if _, ok := h.sessions["old"]; !ok {
		t.Fatal("空闲 10 分钟 < 配置的 30 分钟，应当保留")
	}
}
