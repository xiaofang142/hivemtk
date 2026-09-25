package service

// 批23（§7.28 八-3）契约锁：保留期的**来源**要在扫描入口算好并真的交进 PruneBefore。
//
// 只测 `auditRetention()` 的返回值不够——那证不了「接线」这一段：接线断了的库内形状，
// 与修复前「只有 cutoff、没有来源」逐字节相同，且没有任何一行会红（同 §7.10 那条口径：
// 判据符号在一处消费、测试却只覆盖另一处）。所以这里用两个假仓储把参数接住再断言。

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/repository"
)

type r23CmdRepo struct {
	repository.BrowserCommandLogRepository // 只实现本批用到的三个方法；其余走 nil panic，误用可见
	cutoff                                 time.Time
	source                                 string
	calls                                  int
}

func (r *r23CmdRepo) PruneBefore(_ context.Context, cutoff time.Time, source string) (int64, error) {
	r.cutoff, r.source, r.calls = cutoff, source, r.calls+1
	return 0, nil
}

type r23PlanRepo struct {
	repository.BrowserLLMPlanRepository
	cutoff time.Time
	calls  int
}

func (r *r23PlanRepo) PruneSnapshotText(_ context.Context, cutoff time.Time) (int64, error) {
	r.cutoff, r.calls = cutoff, r.calls+1
	return 0, nil
}

// TestR23RetentionCutoffSourceShape 来源标注的三种形状：未配置 / 配置了 / 配置了但读不出来。
// 第三种最容易做错成「照抄 env 原文」——那会让库里写着一个从未生效过的界。
func TestR23RetentionCutoffSourceShape(t *testing.T) {
	t.Setenv(auditRetentionEnv, "")
	wantDefault := fmt.Sprintf("default:%d", auditRetentionDefault)
	if days, src := auditRetention(); days != auditRetentionDefault || src != wantDefault {
		t.Errorf("未配置 got=(%d,%q) want=(%d,%q)", days, src, auditRetentionDefault, wantDefault)
	}

	t.Setenv(auditRetentionEnv, "30")
	if days, src := auditRetention(); days != 30 || src != auditRetentionEnv+"=30" {
		t.Errorf("配置 30 got=(%d,%q) want=(30,%q)", days, src, auditRetentionEnv+"=30")
	}

	t.Setenv(auditRetentionEnv, "7d")
	days, src := auditRetention()
	if days != auditRetentionDefault {
		t.Errorf("非法值的天数 got=%d want=%d（读不出来时用的是默认值）", days, auditRetentionDefault)
	}
	if !strings.Contains(src, "7d") || !strings.Contains(src, "invalid") {
		t.Errorf("非法值的来源 got=%q，要同时写明原文与「未生效」", src)
	}

	// env 是外部输入：超长值必须截进**列宽**。这里写死字面量而不是引用 `cutoffSourceMaxLen`：
	// 判据若读那个常量，把常量放宽就等于把这条断言一起放宽——变异电池 V5（把 64 改成 1024）
	// 首轮实跑正是"存活"，读出来的是这个自参照形状，不是实现坏了。
	// 64 的事实源有两处：model.BrowserAuditPruneRun 的 `size:64` 与 v3.46.0 的 VARCHAR(64)。
	const columnWidth = 64
	t.Setenv(auditRetentionEnv, strings.Repeat("9", 200))
	if _, src := auditRetention(); len(src) > columnWidth {
		t.Errorf("来源长度 got=%d want≤%d（超长值让留痕 INSERT 失败＝裁剪被一个记账字段堵死）",
			len(src), columnWidth)
	}
}

// TestR23RetentionReadsEnvPerSweep 每次扫描现读：启动时冻结一份，改配置就永不生效，
// 而留痕行会一直写着第一天的来源。
func TestR23RetentionReadsEnvPerSweep(t *testing.T) {
	t.Setenv(auditRetentionEnv, "45")
	d1, s1 := auditRetention()
	t.Setenv(auditRetentionEnv, "5")
	d2, s2 := auditRetention()
	if d1 == d2 || s1 == s2 {
		t.Errorf("改 env 后读数没变 got=(%d,%q) want 与 (%d,%q) 不同", d2, s2, d1, s1)
	}
	if d2 != 5 || s2 != auditRetentionEnv+"=5" {
		t.Errorf("第二次读数 got=(%d,%q) want=(5,%q)", d2, s2, auditRetentionEnv+"=5")
	}
}

// TestR23PruneOncePassesSourceToRepo 接线那一格：来源真的进了 PruneBefore 的参数，
// 且 cutoff 与它同一次算出（两张嘴各读一次 env 会出现「界是新的、来源是旧的」）。
func TestR23PruneOncePassesSourceToRepo(t *testing.T) {
	t.Setenv(auditRetentionEnv, "11")
	cmd, plan := &r23CmdRepo{}, &r23PlanRepo{}
	pruneAuditOnce(context.Background(), cmd, plan)

	if cmd.calls != 1 || plan.calls != 1 {
		t.Fatalf("调用次数 got=(cmd=%d,plan=%d) want=(1,1)", cmd.calls, plan.calls)
	}
	if cmd.source != auditRetentionEnv+"=11" {
		t.Errorf("传给 PruneBefore 的来源 got=%q want=%q（来源半路丢掉＝库里只剩 cutoff，等于没修）",
			cmd.source, auditRetentionEnv+"=11")
	}
	want := time.Now().AddDate(0, 0, -11)
	if cmd.cutoff.Before(want.Add(-time.Hour)) || cmd.cutoff.After(want.Add(time.Hour)) {
		t.Errorf("cutoff got=%v 与 11 天前的界差得太远（界与来源不同源就会对不上）", cmd.cutoff)
	}
	if !plan.cutoff.Equal(cmd.cutoff) {
		t.Errorf("两张表的界 got plan=%v cmd=%v（同一次扫描必须同一个界）", plan.cutoff, cmd.cutoff)
	}
}

// TestR23PruneOnceDisabledByZero 保留期=0 时一轮扫描一次都不碰库：0 是「禁用」这个显式语义，
// 若走到 PruneBefore 就成了「界=今天」的全量裁剪。
func TestR23PruneOnceDisabledByZero(t *testing.T) {
	t.Setenv(auditRetentionEnv, "0")
	cmd, plan := &r23CmdRepo{}, &r23PlanRepo{}
	pruneAuditOnce(context.Background(), cmd, plan)
	if cmd.calls != 0 || plan.calls != 0 {
		t.Errorf("禁用态仍调用了裁剪 got=(cmd=%d,plan=%d) want=(0,0)", cmd.calls, plan.calls)
	}
}

// TestR23RetentionBootGate 启动那道门的三种读数。它挡的是「仓储没装配齐却起了扫描
// goroutine」——那种情况下 panic 发生在一小时后的后台协程里，会带走整个进程，
// 而没有任何一条用例活到那一刻，所以这道门必须就地可断言。
func TestR23RetentionBootGate(t *testing.T) {
	t.Setenv(auditRetentionEnv, "90")
	cmd, plan := &r23CmdRepo{}, &r23PlanRepo{}
	if retentionEnabled(nil, plan) || retentionEnabled(cmd, nil) {
		t.Error("仓储缺一个仍判「可启动」：nil 仓储会在第一跳 panic")
	}
	if !retentionEnabled(cmd, plan) {
		t.Error("配置齐备却判不可启动：裁剪功能永不运行，等于 90 天保留期形同虚设")
	}

	t.Setenv(auditRetentionEnv, "0")
	if retentionEnabled(cmd, plan) {
		t.Error("保留期=0（显式禁用）仍判可启动")
	}
}
