package repository

// 批23（§7.28 八-3）契约锁：裁剪的**界**要连**来源**一起留痕。
//
// 修之前 `browser_audit_prune_runs` 只有 `cutoff`（界是多少）。于是这两种事实在库里同形：
//   - 运维把 BROWSER_AUDIT_RETENTION_DAYS 从 90 改成 7，等一天，改回 90；
//   - 一次按 90 天的正常裁剪。
//
// 批22 立的「裁剪不等于证据消失」管住了「被删的那些行存在过」，管不住「按什么界放的行」。
// 补法与 A6 同一条路子：能在事后反推的都不用记，反推不出来的必须在删之前落库。
//
// 两条腿各钉一半：
//   - 来源确实进了那一行（不是只进了日志）；
//   - 空来源被拒绝，且**一行都没删**、也没留下摘要/留痕（fail-close 的方向：
//     记账字段缺了就不许动手，与「摘要写不进去就不删」同形）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"

	"gorm.io/gorm"
)

// TestR23PruneRunRecordsCutoffSource 界与来源一起落 prune_runs：cutoff 是传进来的那个瞬间值，
// cutoff_source 是调用方给的那句话，两者缺一都证不了「按什么界放的行」。
func TestR23PruneRunRecordsCutoffSource(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db, a6Row(a6SessionA, 1, "click", `{"x":1}`), a6Row(a6SessionA, 2, "type", `{"x":2}`))

	cutoff := a6Cutoff().Truncate(time.Microsecond) // 与 PG timestamp 精度对齐，否则等值断言会假红
	source := "env:BROWSER_AUDIT_RETENTION_DAYS=90"
	if _, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), cutoff, source); err != nil {
		t.Fatalf("PruneBefore 报错：%v", err)
	}

	runs := a6RunRows(t, db)
	if len(runs) != 1 {
		t.Fatalf("留痕行数 got=%d want=1", len(runs))
	}
	got := runs[0]
	if got.CutoffSource != source {
		t.Errorf("cutoff_source got=%q want=%q（来源没进库=本项修复不存在，日志里说过不算留痕）", got.CutoffSource, source)
	}
	if !got.Cutoff.Equal(cutoff) {
		t.Errorf("cutoff got=%v want=%v（界与来源必须成对：只对上一个、另一个来自别处，等于没记）", got.Cutoff, cutoff)
	}
}

// TestR23PruneRefusesEmptyCutoffSource 空来源拒绝执行，且拒绝得干干净净：不删行、不落摘要、不落留痕。
// 允许空串（或补一个 "unknown"）等于把这列做成摆设——人人都能填那个合法的空值。
func TestR23PruneRefusesEmptyCutoffSource(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db, a6Row(a6SessionA, 1, "click", `{"x":1}`), a6Row(a6SessionA, 2, "type", `{"x":2}`))
	before := a6CountLog(t, db, a6SessionA)

	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), a6Cutoff(), "")
	if !errors.Is(err, ErrPruneCutoffSourceRequired) {
		t.Fatalf("空来源的报错 got=%v want=ErrPruneCutoffSourceRequired", err)
	}
	if pruned != 0 {
		t.Errorf("空来源时返回的删除行数 got=%d want=0（报错却删了行＝记账失败反而放行裁剪）", pruned)
	}
	if after := a6CountLog(t, db, a6SessionA); after != before {
		t.Errorf("命令日志行数 got=%d want=%d（一行都不许少）", after, before)
	}
	if n := countRows(t, db, &model.BrowserAuditDigest{}); n != 0 {
		t.Errorf("摘要行数 got=%d want=0（被拒绝的调用不该留下任何痕迹）", n)
	}
	if n := countRows(t, db, &model.BrowserAuditPruneRun{}); n != 0 {
		t.Errorf("留痕行数 got=%d want=0（同上）", n)
	}
}

func countRows(t *testing.T, db *gorm.DB, dest any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(dest).Count(&n).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	return n
}
