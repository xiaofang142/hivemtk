package middleware

// brute_force_ma05_test.go —— MA-05：janitor 清理口径（修前 failures==0 恒假 ⇒ 表只增不减）。

import (
	"testing"
	"time"
)

func TestBruteForceCleanupStaleLocked(t *testing.T) {
	ResetBruteForceForTest()
	now := time.Now()

	stale := &bruteForceEntry{failures: 3, firstAt: now.Add(-2 * time.Hour)}                                            // 超窗未锁
	staleLocked := &bruteForceEntry{failures: 9, firstAt: now.Add(-2 * time.Hour), lockedUntil: now.Add(-time.Hour)}    // 锁已过期+超窗
	fresh := &bruteForceEntry{failures: 3, firstAt: now}                                                                // 窗内
	activeLock := &bruteForceEntry{failures: 9, firstAt: now.Add(-time.Minute), lockedUntil: now.Add(30 * time.Minute)} // 锁生效中

	globalBruteForce.mu.Lock()
	globalBruteForce.entries = map[string]*bruteForceEntry{
		"1.1.1.1|auth.login": stale,
		"2.2.2.2|auth.login": staleLocked,
		"3.3.3.3|auth.login": fresh,
		"4.4.4.4|auth.login": activeLock,
	}
	globalBruteForce.mu.Unlock()

	globalBruteForce.cleanupStaleLocked(now)

	globalBruteForce.mu.RLock()
	defer globalBruteForce.mu.RUnlock()
	if _, ok := globalBruteForce.entries["1.1.1.1|auth.login"]; ok {
		t.Error("超窗未锁死条目应被清")
	}
	if _, ok := globalBruteForce.entries["2.2.2.2|auth.login"]; ok {
		t.Error("锁已过期+超窗条目应被清")
	}
	if _, ok := globalBruteForce.entries["3.3.3.3|auth.login"]; !ok {
		t.Error("窗内活跃条目不应被清")
	}
	if _, ok := globalBruteForce.entries["4.4.4.4|auth.login"]; !ok {
		t.Error("锁生效中条目不应被清")
	}
}
