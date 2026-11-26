// Package service FA-REPORT-01 合成测试：日聚合按完整受影响业务日重算。
//
// 覆盖判据：同日不同时段、两个重叠窗口、跨午夜、业务时区、删除/迟到数据一致、
// 任意重跑后日总数/成本恒等于权威明细（不只重复同一窗口验证幂等）。
package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// faCST 业务时区（Asia/Shanghai）；精简镜像无 zoneinfo 时退回固定 +8（无夏令时等价）。
func faCST(t *testing.T) *time.Location {
	t.Helper()
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

func faAt(t *testing.T, loc *time.Location, y, m, d, h, min int) time.Time {
	t.Helper()
	return time.Date(y, time.Month(m), d, h, min, 0, 0, loc)
}

func faReportSvc(t *testing.T) (*ReportService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.LLMRoutingLog{}, &model.LLMUsageDaily{})
	addT3Columns(t, db)
	return NewReportService(repository.NewLLMUsageReportRepository(db)), db
}

// faSeedLog 在 ts 时刻写入 n 条成功明细（cost 0.02 元 = 2 cents/条）。
func faSeedLog(t *testing.T, db *gorm.DB, ts time.Time, trace string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := db.Table("llm_routing_logs").Create(map[string]any{
			"tenant_id": 1, "trace_id": fmt.Sprintf("%s-%d", trace, i),
			"scenario": "chat", "provider": "p1", "model": "m1",
			"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150,
			"cost": 0.02, "latency_ms": 800, "success": true,
			"channel": "web", "source": "platform", "created_at": ts,
		}).Error; err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}
}

// faDaily 读某业务日的日聚合 (call_count, total_cost_cents)。
func faDaily(t *testing.T, db *gorm.DB, statDate string) (int64, int64) {
	t.Helper()
	var row struct {
		Calls int64 `gorm:"column:calls"`
		Cost  int64 `gorm:"column:cost"`
	}
	if err := db.Table("llm_usage_daily").Where("stat_date = ?", statDate).
		Select("COALESCE(SUM(call_count),0) AS calls, COALESCE(SUM(total_cost_cents),0) AS cost").
		Scan(&row).Error; err != nil {
		t.Fatalf("dump daily: %v", err)
	}
	return row.Calls, row.Cost
}

// TestFAReport_SameDaySlotsNotOverwritten 核心回归：最近 10 分钟窗口只覆盖尾部时段，
// 旧实现会把整天计数覆盖成窗口值；重算后应等于整天权威明细。
func TestFAReport_SameDaySlotsNotOverwritten(t *testing.T) {
	svc, db := faReportSvc(t)
	loc := faCST(t)
	day := faAt(t, loc, 2026, 11, 20, 9, 0)
	faSeedLog(t, db, day, "early", 2)                               // 09:00 两条
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 23, 55), "late", 3) // 23:55 三条
	statDate := timeutil.BusinessDate(day)

	// now=23:57 → 窗口 [23:47,23:57] 只触达 23:55 那条时段。
	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 20, 23, 57) }
	ctx := context.Background()
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("AggregateRecent: %v", err)
	}
	calls, cost := faDaily(t, db, statDate)
	if calls != 5 || cost != 10 { // 2+3 条，每条 2 cents
		t.Fatalf("同日整天重算后 call=%d cost=%d, want 5/10（窗口覆盖是旧缺陷）", calls, cost)
	}
	// 任意重跑恒等于明细。
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	calls2, _ := faDaily(t, db, statDate)
	if calls2 != 5 {
		t.Fatalf("重跑后 call=%d, want 5", calls2)
	}
}

// TestFAReport_TwoOverlappingWindows 两个重叠窗口各自重算整天，结果收敛到权威明细，
// 而不是被第二个窗口覆盖成它的局部值。
func TestFAReport_TwoOverlappingWindows(t *testing.T) {
	svc, db := faReportSvc(t)
	loc := faCST(t)
	day := faAt(t, loc, 2026, 11, 20, 10, 0)
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 10, 0), "w1", 2)
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 10, 5), "w2", 2)
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 10, 10), "w3", 1)
	statDate := timeutil.BusinessDate(day)
	ctx := context.Background()

	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 20, 10, 7) }
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("window1: %v", err)
	}
	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 20, 10, 12) }
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("window2: %v", err)
	}
	calls, _ := faDaily(t, db, statDate)
	if calls != 5 {
		t.Fatalf("重叠窗口重算后 call=%d, want 5", calls)
	}
}

// TestFAReport_CrossMidnight 跨午夜窗口重算两侧业务日，各自等于当日权威明细。
func TestFAReport_CrossMidnight(t *testing.T) {
	svc, db := faReportSvc(t)
	loc := faCST(t)
	d0 := faAt(t, loc, 2026, 11, 20, 9, 0)
	d1 := faAt(t, loc, 2026, 11, 21, 0, 5)
	faSeedLog(t, db, d0, "day0early", 4)                                // 当日早段
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 23, 55), "day0late", 2) // 当日尾段
	faSeedLog(t, db, d1, "day1", 3)                                     // 次日
	s0 := timeutil.BusinessDate(d0)
	s1 := timeutil.BusinessDate(d1)

	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 21, 0, 7) }
	if err := svc.AggregateRecent(context.Background()); err != nil {
		t.Fatalf("AggregateRecent: %v", err)
	}
	c0, _ := faDaily(t, db, s0)
	c1, _ := faDaily(t, db, s1)
	if c0 != 6 || c1 != 3 {
		t.Fatalf("跨午夜：day0=%d(要6) day1=%d(要3)", c0, c1)
	}
}

// TestFAReport_DeletedDetailConverges 删除明细后重算，日表随之收敛（删空→重建），
// 证明"删除数据亦有一致规则"，且非幂等校验（数据变了结果必须变）。
func TestFAReport_DeletedDetailConverges(t *testing.T) {
	svc, db := faReportSvc(t)
	loc := faCST(t)
	day := faAt(t, loc, 2026, 11, 20, 9, 0)
	faSeedLog(t, db, day, "early", 2)
	faSeedLog(t, db, faAt(t, loc, 2026, 11, 20, 23, 55), "late", 3)
	statDate := timeutil.BusinessDate(day)
	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 20, 23, 57) }
	ctx := context.Background()
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	calls, _ := faDaily(t, db, statDate)
	if calls != 5 {
		t.Fatalf("初始 call=%d, want 5", calls)
	}
	// 删掉尾段 3 条权威明细后重算。
	if err := db.Table("llm_routing_logs").Where("trace_id LIKE ?", "late-%").Delete(nil).Error; err != nil {
		t.Fatalf("delete detail: %v", err)
	}
	if err := svc.AggregateRecent(ctx); err != nil {
		t.Fatalf("recalc after delete: %v", err)
	}
	calls, _ = faDaily(t, db, statDate)
	if calls != 2 {
		t.Fatalf("删除后重算 call=%d, want 2（日表须收敛到权威明细）", calls)
	}
}

// TestFAReport_RunDailyRecalcBusinessDay 日终重算按 CST 业务自然日取整，
// 不再用 UTC Truncate(24h) 把业务日前 8 小时算到相邻日。
func TestFAReport_RunDailyRecalcBusinessDay(t *testing.T) {
	svc, db := faReportSvc(t)
	loc := faCST(t)
	// 业务日 11-20 的 22:00/23:30 共 5 条；11-21 的 00:30 一条。
	dYesterday1 := faAt(t, loc, 2026, 11, 20, 22, 0)
	dYesterday2 := faAt(t, loc, 2026, 11, 20, 23, 30)
	dToday := faAt(t, loc, 2026, 11, 21, 0, 30)
	faSeedLog(t, db, dYesterday1, "y1", 2)
	faSeedLog(t, db, dYesterday2, "y2", 3)
	faSeedLog(t, db, dToday, "today", 1)

	svc.nowFn = func() time.Time { return faAt(t, loc, 2026, 11, 21, 0, 35) }
	if err := svc.RunDailyRecalc(context.Background()); err != nil {
		t.Fatalf("RunDailyRecalc: %v", err)
	}
	// 重算的是业务日 11-20（5 条），不是 UTC 口径的相邻日。
	calls, _ := faDaily(t, db, timeutil.BusinessDate(dYesterday1))
	if calls != 5 {
		t.Fatalf("业务日重算 call=%d, want 5", calls)
	}
}
