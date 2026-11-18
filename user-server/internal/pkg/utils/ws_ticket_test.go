package utils

import (
	"testing"
	"time"
)

// TestWSTicketJanitorSweepsExpiredEntries 回归钉（2026-11-18 审计）：
// 七审给票据商店加上限时配了 janitor 却漏了接线（startWsTicketJanitor 零调用点），
// 废弃票只涨不清，涨满 wsTicketStoreCap 后铸造永久 fail-closed。本测断言两件事：
// ① 铸造路径会把 janitor 拉起来（接线钉）；② 过期废弃票确实会被清掉（清扫钉）。
func TestWSTicketJanitorSweepsExpiredEntries(t *testing.T) {
	prevInterval := wsTicketJanitorInterval
	wsTicketJanitorInterval = 20 * time.Millisecond
	t.Cleanup(func() { wsTicketJanitorInterval = prevInterval })

	// 直接塞一枚过期的废弃票（模拟「铸了没连」的泄漏路径，不依赖真实 TTL 等待）
	wsTicketMu.Lock()
	wsTicketStore["leaked-expired"] = wsTicketEntry{
		claims:    WsTicketClaims{UserID: 1},
		expiresAt: time.Now().Add(-time.Second),
	}
	wsTicketMu.Unlock()

	// 铸造必须触发 janitor 接线；正常铸造也不应被拒（远未到上限）
	if MintWSTicket(WsTicketClaims{UserID: 2}) == "" {
		t.Fatal("正常铸造不应被拒")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		wsTicketMu.Lock()
		_, alive := wsTicketStore["leaked-expired"]
		wsTicketMu.Unlock()
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("过期废弃票 2s 内未被 janitor 回收——startWsTicketJanitor 未接线或清扫失效")
}
