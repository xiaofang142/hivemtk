package service

import (
	"context"
	"testing"
)

// —— 服务端下发限速兜底（DeliverRateLimitPerMin，架构文档 §4.2 承诺的落地）——
// 2026-11-14 二次论证发现该承诺从未实现（全库 grep 零命中），本文件锁判定核与默认值。
// 全链路（种行→认领被拦→窗口恢复）见 bridge 包 handler_http_deliver_rate_limit_test.go。

func TestDeliverClaimThrottled(t *testing.T) {
	cases := []struct {
		name          string
		recentClaims  int64
		limit         int
		wantThrottled bool
	}{
		{"limit<=0 = 参数侧停用，永不限", 999999, 0, false},
		{"limit 负值同停用", 100, -1, false},
		{"近窗认领数达到阈值 → 拦", 60, 60, true},
		{"近窗认领数超阈值 → 拦", 61, 60, true},
		{"近窗认领数低于阈值 → 放行", 59, 60, false},
		{"零认领 → 放行", 0, 60, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliverClaimThrottled(tc.recentClaims, tc.limit); got != tc.wantThrottled {
				t.Errorf("deliverClaimThrottled(%d, %d) = %v，期望 %v",
					tc.recentClaims, tc.limit, got, tc.wantThrottled)
			}
		})
	}
}

// 参数未装配（全局单例空，nil-safe）→ 回落架构文档承诺值 60/min。
func TestBridgeDeliverRateLimitPerMin_NotWired_FallsBackToDefault(t *testing.T) {
	SetGlobalForTest(nil)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	if got := BridgeDeliverRateLimitPerMin(context.Background()); got != BridgeDeliverRateLimitDefault {
		t.Errorf("未装配时 = %d，期望默认 %d", got, BridgeDeliverRateLimitDefault)
	}
	if BridgeDeliverRateLimitDefault != 60 {
		t.Errorf("默认值应与架构文档 §4.2 承诺的 60/min 同值，实际 %d", BridgeDeliverRateLimitDefault)
	}
}
