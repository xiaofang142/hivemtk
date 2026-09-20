package approval

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/featureflag"
)

func setFlag(t *testing.T, name string, on bool) {
	t.Helper()
	featureflag.Get(name)
	os.Setenv("FF_"+strings.ToUpper(name), boolToEnv(on))
	featureflag.DefaultManager().ReloadAll()
}

func boolToEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func TestBootstrapFlag(t *testing.T) {
	setFlag(t, FlagKey, false)
	if featureflag.Get(FlagKey).Bool() {
		t.Fatalf("expected default off for tool_approval_gate")
	}
}

func TestDefaultDeny(t *testing.T) {
	setFlag(t, FlagKey, true)
	defer setFlag(t, FlagKey, false)
	w := NewWhiteList(nil, time.Now)
	decisions := 0
	w.callback = func(ctx context.Context, tn, acc string, d Decision) { decisions++ }
	if w.IsApproved(context.Background(), "reach.proactive.whatsapp", "acct1") {
		t.Fatalf("expected deny by default")
	}
	if decisions != 1 {
		t.Fatalf("expected 1 callback")
	}
}

func TestWhitelistAllows(t *testing.T) {
	setFlag(t, FlagKey, true)
	defer setFlag(t, FlagKey, false)
	w := NewWhiteList(nil, time.Now)
	w.Whitelist("reach.proactive.whatsapp", "acct1", time.Time{})
	if !w.IsApproved(context.Background(), "reach.proactive.whatsapp", "acct1") {
		t.Fatalf("expected allow after whitelist")
	}
	if w.IsApproved(context.Background(), "reach.proactive.whatsapp", "acct2") {
		t.Fatalf("expected deny for other account")
	}
}

func TestWhitelistExpiry(t *testing.T) {
	setFlag(t, FlagKey, true)
	defer setFlag(t, FlagKey, false)
	now := time.Now()
	w := NewWhiteList(nil, func() time.Time { return now })
	w.Whitelist("reach.batch_send.email", "acct1", now.Add(-time.Minute))
	if w.IsApproved(context.Background(), "reach.batch_send.email", "acct1") {
		t.Fatalf("expected deny after expiry")
	}
}

func TestFlagOffAllDenied(t *testing.T) {
	setFlag(t, FlagKey, false)
	w := NewWhiteList(nil, time.Now)
	w.Whitelist("reach.dm.telegram", "acct1", time.Time{})
	if w.IsApproved(context.Background(), "reach.dm.telegram", "acct1") {
		t.Fatalf("expected deny when flag off")
	}
}

func TestDecisionCallbackThreadSafety(t *testing.T) {
	setFlag(t, FlagKey, true)
	defer setFlag(t, FlagKey, false)
	var (
		mu  sync.Mutex
		cnt int
	)
	w := NewWhiteList(func(ctx context.Context, tn, acc string, d Decision) {
		mu.Lock()
		cnt++
		mu.Unlock()
	}, time.Now)
	w.Whitelist("reach.proactive.whatsapp", "acct1", time.Time{})
	for i := 0; i < 50; i++ {
		w.IsApproved(context.Background(), "reach.proactive.whatsapp", "acct1")
	}
	if cnt != 50 {
		t.Fatalf("expected 50 callbacks, got %d", cnt)
	}
}

// TestVerdictAndIsApprovedSameSource Verdict 与 IsApproved 必须同源、且各只回调一次。
//
// IsApproved 现在是 Verdict 的一层薄封装。若有人把两者各自实现一遍，
// 判定与理由就会脱节（allowed=false 配 reason=whitelisted），
// 而 block 态的刹车恰好是靠 reason 判定的——脱节一次就多拦/少拦一批真实外发。
func TestVerdictAndIsApprovedSameSource(t *testing.T) {
	setFlag(t, FlagKey, true)
	defer setFlag(t, FlagKey, false)

	var seen []Decision
	w := NewWhiteList(func(ctx context.Context, tn, acc string, d Decision) {
		seen = append(seen, d)
	}, time.Now)
	w.Whitelist("reach.batch", "acct1", time.Time{})

	cases := []struct {
		account string
		allowed bool
		reason  string
	}{
		{"acct1", true, ReasonWhitelisted},
		{"acct2", false, ReasonDeniedDefault},
	}
	for _, c := range cases {
		allowed, reason := w.Verdict(context.Background(), "reach.batch", c.account)
		if allowed != c.allowed || reason != c.reason {
			t.Errorf("Verdict(%s) = %v/%q, want %v/%q", c.account, allowed, reason, c.allowed, c.reason)
		}
		if got := w.IsApproved(context.Background(), "reach.batch", c.account); got != c.allowed {
			t.Errorf("IsApproved(%s) = %v, want %v", c.account, got, c.allowed)
		}
	}
	// 2 个用例 × 各一次 Verdict + 一次 IsApproved = 4 笔留痕，多一笔就是重复计数
	if len(seen) != 4 {
		t.Errorf("回调次数 = %d, want 4（IsApproved 不得二次判定）", len(seen))
	}
	for i, d := range seen {
		if d.Reason == "" {
			t.Errorf("第 %d 笔留痕缺 reason：%+v", i, d)
		}
	}
}

// TestActiveEntryCountCountsOnlyUsableEntries 有效条目数只算"现在还能放行"的那些。
//
// 这个数字是 block 态放量前的自检读数（0 条 = 一开旗子所有冷触达都会被拒），
// 所以已过期与永不过期、以及 nil 接收者三种情形都必须判对，偏大比偏小更危险。
func TestActiveEntryCountCountsOnlyUsableEntries(t *testing.T) {
	var nilChecker *WhiteListApprovalChecker
	if got := nilChecker.ActiveEntryCount(); got != 0 {
		t.Errorf("nil 接收者应返回 0，实际 %d", got)
	}
	if got := NewWhiteList(nil, time.Now).ActiveEntryCount(); got != 0 {
		t.Errorf("空表应为 0，实际 %d", got)
	}

	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	w := NewWhiteList(nil, func() time.Time { return now })
	w.Whitelist("reach.batch", "never", time.Time{})
	w.Whitelist("reach.batch", "live", now.Add(time.Hour))
	w.Whitelist("reach.schedule", "expired", now.Add(-time.Second))
	w.Whitelist("reach.dm", "also-expired", now.Add(-72*time.Hour))
	// 同一 (tool, account) 覆盖写：只能算一条
	w.Whitelist("reach.batch", "never", now.Add(time.Hour))
	if got := w.ActiveEntryCount(); got != 2 {
		t.Errorf("有效条目 = %d, want 2（过期与重复都不该计入）", got)
	}

	// 时间前进后过期项要跟着掉：读数偏大会让运维以为"还有授权可用"而放量
	w.nowFn = func() time.Time { return now.Add(2 * time.Hour) }
	if got := w.ActiveEntryCount(); got != 0 {
		t.Errorf("时钟前进后有效条目 = %d, want 0", got)
	}
}

// TestActiveEntryCountForIsolatesByTool 按入口拆开的有效条目数。
//
// 存在的理由：W-1 的冷触达工具与 T-P3-07 的 reach 外发门**共用同一张白名单表**，
// 只报总数会让运维在"我只给 reach 放了 3 个账号"和"reach 一条授权都没有、
// 另外 3 条在工具侧"之间读不出差别 —— 而 block 态下后者意味着所有外发都会被拒。
// 口径与 ActiveEntryCount 一致：过期不计、nil 接收者返回 0。
func TestActiveEntryCountForIsolatesByTool(t *testing.T) {
	var nilChecker *WhiteListApprovalChecker
	if got := nilChecker.ActiveEntryCountFor("reach.proactive.send"); got != 0 {
		t.Errorf("nil 接收者应返回 0，实际 %d", got)
	}
	if got := NewWhiteList(nil, time.Now).ActiveEntryCountFor("reach.proactive.send"); got != 0 {
		t.Errorf("空表应为 0，实际 %d", got)
	}

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	w := NewWhiteList(nil, func() time.Time { return now })
	w.Whitelist("reach.proactive.send", "uid-a", time.Time{})
	w.Whitelist("reach.proactive.send", "uid-b", now.Add(time.Hour))
	w.Whitelist("reach.proactive.send", "uid-gone", now.Add(-time.Second))
	w.Whitelist("reach.batch", "acct-1", time.Time{})
	w.Whitelist("reach.batch", "acct-2", time.Time{})

	if got := w.ActiveEntryCountFor("reach.proactive.send"); got != 2 {
		t.Errorf("reach 入口有效条目 = %d, want 2（过期那条与其他入口的都不该计入）", got)
	}
	if got := w.ActiveEntryCountFor("reach.batch"); got != 2 {
		t.Errorf("reach.batch 有效条目 = %d, want 2", got)
	}
	// 没授权过的入口读 0 而不是"和总数一样"：读成总数就是把"没配"显示成"配满了"
	if got := w.ActiveEntryCountFor("reach.never.granted"); got != 0 {
		t.Errorf("未授权入口应读 0，实际 %d", got)
	}
	// 总数 = 两个入口的有效条目相加（过期那条两边都不计）：分项读数必须能被总数校验，
	// 否则"分项各自报个好看的数、总数另说"就是两份互不相干的口径。
	if total := w.ActiveEntryCount(); total != 4 {
		t.Errorf("总有效条目 = %d, want 4（2+2，过期那条两边都不计）", total)
	}
}
