// decision_counter_test.go T-P1-05：审批门观察期计数器的口径。
//
// 这个类型的唯一职责是"把判定数字留住"，所以测试全部围绕三件事：
//
//	数字对不对（total / would_deny / 按 reason 拆 / 按工具拆）
//	排序稳不稳（灰度报告要能当处置顺序读）
//	零值安全不安全（off 态下包级计数器是 nil，回调会直接打到它身上）
package approval

import (
	"context"
	"testing"
	"time"
)

func deny(reason string) Decision { return Decision{Allowed: false, Reason: reason} }

func allow(reason string) Decision { return Decision{Allowed: true, Reason: reason} }

func TestDecisionCounter_NilReceiverSafe(t *testing.T) {
	var c *DecisionCounter
	ctx := context.Background()
	c.Observe(ctx, "reach.telegram.dm", "u1", deny(ReasonDisabledByFlag)) // 不得 panic
	rep := c.Report()
	if rep.Total != 0 || rep.WouldDeny != 0 {
		t.Errorf("nil 计数器报告应为零值，实际 %+v", rep)
	}
	if rep.ByReason == nil {
		t.Error("ByReason 应为空 map 而非 nil（端点直接序列化成 JSON {}）")
	}
	c.Reset()
}

func TestDecisionCounter_ReasonBreakdown(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	// 5 次冷触达：3 次闸门没开、1 次开了但账号没被批、1 次白名单放行
	c.Observe(ctx, "reach.batch", "u1", deny(ReasonDisabledByFlag))
	c.Observe(ctx, "reach.batch", "u2", deny(ReasonDisabledByFlag))
	c.Observe(ctx, "reach.batch", "u3", deny(ReasonDisabledByFlag))
	c.Observe(ctx, "reach.schedule", "u4", deny(ReasonDeniedDefault))
	c.Observe(ctx, "reach.schedule", "u5", allow(ReasonWhitelisted))

	rep := c.Report()
	if rep.Total != 5 {
		t.Errorf("Total = %d, want 5", rep.Total)
	}
	if rep.WouldDeny != 4 {
		t.Errorf("WouldDeny = %d, want 4", rep.WouldDeny)
	}
	if rep.WouldDenyRatePct != 80 {
		t.Errorf("WouldDenyRatePct = %v, want 80", rep.WouldDenyRatePct)
	}
	wantReasons := map[string]int64{
		ReasonDisabledByFlag: 3,
		ReasonDeniedDefault:  1,
		ReasonWhitelisted:    1,
	}
	for k, v := range wantReasons {
		if rep.ByReason[k] != v {
			t.Errorf("ByReason[%q] = %d, want %d", k, rep.ByReason[k], v)
		}
	}
	if len(rep.ByReason) != len(wantReasons) {
		t.Errorf("ByReason 多出了未预期的键：%v", rep.ByReason)
	}
	// 各 reason 之和必须等于 total，否则有判定漏记
	var sum int64
	for _, v := range rep.ByReason {
		sum += v
	}
	if sum != rep.Total {
		t.Errorf("ΣByReason=%d ≠ Total=%d", sum, rep.Total)
	}
}

func TestDecisionCounter_PerToolOrderedByWouldDeny(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		c.Observe(ctx, "reach.batch", "u", deny(ReasonDeniedDefault))
	}
	c.Observe(ctx, "reach.schedule", "u", deny(ReasonDeniedDefault))
	c.Observe(ctx, "reach.lead", "u", allow(ReasonWhitelisted))
	// 两个各被拦 1 次的工具，用来验同量时的次级排序
	c.Observe(ctx, "reach.zulu", "u", deny(ReasonDeniedDefault))
	c.Observe(ctx, "reach alpha", "u", deny(ReasonDeniedDefault))

	rep := c.Report()
	if len(rep.PerTool) != 5 {
		t.Fatalf("PerTool 应含 5 个工具，实际 %d", len(rep.PerTool))
	}
	if rep.PerTool[0].ToolName != "reach.batch" {
		t.Errorf("会被拦得最多的工具应排在首位，实际首位 %q", rep.PerTool[0].ToolName)
	}
	if rep.PerTool[0].WouldDeny != 3 || rep.PerTool[0].Total != 3 {
		t.Errorf("reach.batch 统计错：%+v", rep.PerTool[0])
	}
	if rep.PerTool[1].ToolName != "reach alpha" || rep.PerTool[2].ToolName != "reach.schedule" || rep.PerTool[3].ToolName != "reach.zulu" {
		t.Errorf("同为 1 次 would_deny 的应按名字典序：实际 %q / %q / %q",
			rep.PerTool[1].ToolName, rep.PerTool[2].ToolName, rep.PerTool[3].ToolName)
	}
	if rep.PerTool[4].ToolName != "reach.lead" || rep.PerTool[4].WouldDeny != 0 {
		t.Errorf("whitelisted 那次不应计入 would_deny，且零拦下的工具应排末位：%+v", rep.PerTool[4])
	}
}

func TestDecisionCounter_LastReasonReflectsLatest(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	c.Observe(ctx, "reach.batch", "u", deny(ReasonDisabledByFlag))
	c.Observe(ctx, "reach.batch", "u", allow(ReasonWhitelisted))
	rep := c.Report()
	if rep.PerTool[0].LastReason != ReasonWhitelisted {
		t.Errorf("LastReason = %q, want 最近一次的 %q", rep.PerTool[0].LastReason, ReasonWhitelisted)
	}
}

func TestDecisionCounter_EmptyReasonBucketed(t *testing.T) {
	c := NewDecisionCounter()
	c.Observe(context.Background(), "reach.batch", "u", Decision{Allowed: false})
	rep := c.Report()
	if rep.ByReason["unspecified"] != 1 {
		t.Errorf("空 reason 应归入 unspecified，实际 %v", rep.ByReason)
	}
	if rep.WouldDeny != 1 {
		t.Errorf("Allowed=false 即计入 would_deny，实际 %d", rep.WouldDeny)
	}
}

func TestDecisionCounter_ReportIsSnapshot(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	c.Observe(ctx, "reach.batch", "u", deny(ReasonDeniedDefault))
	rep := c.Report()
	c.Observe(ctx, "reach.batch", "u", deny(ReasonDeniedDefault))
	if rep.Total != 1 {
		t.Errorf("已返回的报告不该被后续写入改动，实际 Total=%d", rep.Total)
	}
	rep.ByReason[ReasonDeniedDefault] = 99
	if c.Report().ByReason[ReasonDeniedDefault] != 2 {
		t.Error("报告里的 ByReason 必须是拷贝（改它不该污染内部状态）")
	}
}

func TestDecisionCounter_Reset(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	c.Observe(ctx, "reach.batch", "u", deny(ReasonDeniedDefault))
	c.Reset()
	rep := c.Report()
	if rep.Total != 0 || rep.WouldDeny != 0 || len(rep.ByReason) != 0 || len(rep.PerTool) != 0 {
		t.Errorf("Reset 后应为零值报告，实际 %+v", rep)
	}
	c.Observe(ctx, "reach.batch", "u", allow(ReasonWhitelisted))
	if c.Report().Total != 1 {
		t.Error("Reset 后必须还能继续记账（map 不能是 nil）")
	}
}

func TestDecisionCounter_ConcurrentObserve(t *testing.T) {
	c := NewDecisionCounter()
	ctx := context.Background()
	const n = 64
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			if i%2 == 0 {
				c.Observe(ctx, "reach.batch", "u", deny(ReasonDeniedDefault))
				return
			}
			c.Observe(ctx, "reach.batch", "u", allow(ReasonWhitelisted))
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}
	rep := c.Report()
	if rep.Total != n {
		t.Errorf("Total = %d, want %d", rep.Total, n)
	}
	if rep.WouldDeny != n/2 {
		t.Errorf("WouldDeny = %d, want %d", rep.WouldDeny, n/2)
	}
}

// TestReasonConstantsMatchWhitelistDecisions 报告键与判定产出的字面量必须同源。
// 白名单实现改了 reason 文案而这里没跟上时，按 reason 拆的报表会静默少一类。
func TestReasonConstantsMatchWhitelistDecisions(t *testing.T) {
	cases := []struct {
		name    string
		flagOn  bool
		grant   bool
		expired bool
		want    string
	}{
		{"旗子没开", false, false, false, ReasonDisabledByFlag},
		{"开了但账号不在表里", true, false, false, ReasonDeniedDefault},
		{"在表里但已过期", true, true, true, ReasonDeniedExplicit},
		{"在表里且未过期", true, true, false, ReasonWhitelisted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setFlag(t, FlagKey, c.flagOn)
			defer setFlag(t, FlagKey, false)

			now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
			var got Decision
			w := NewWhiteList(func(ctx context.Context, toolName, accountID string, d Decision) {
				got = d
			}, func() time.Time { return now })
			if c.grant {
				exp := time.Time{}
				if c.expired {
					exp = now.Add(-time.Minute)
				}
				w.Whitelist("reach.batch", "u1", exp)
			}
			w.IsApproved(context.Background(), "reach.batch", "u1")
			if got.Reason != c.want {
				t.Errorf("reason = %q, want %q", got.Reason, c.want)
			}
		})
	}
}
