// tool_audit_repo_test.go T-P1-08：工具审计读侧仓储（表 tool_call_audits）。
//
// 读侧存在的意义是"运维能在端点上看出这条审计到底落库了没有"，所以这里重点不在
// 能不能查出来，而在**查不到时的措辞**：空表回空列表（不是错误），句柄没了/limit 非法
// 必须报错（不能被读成"没有审计"）。这两条各自都有测试盯着。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupToolAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.ToolCallAudit{})
}

// seedAudit 写一条审计行（直接 Create，绕开异步 logger，让读侧测试只依赖读侧）。
func seedAudit(t *testing.T, database *gorm.DB, tool string, ok bool, durMs int64, at time.Time) {
	t.Helper()
	row := model.ToolCallAudit{
		ToolName:    tool,
		Success:     ok,
		DurationMs:  durMs,
		ExecutedAt:  at,
		TraceID:     "trace-" + tool,
		ArgsSummary: "客户张三的订单备注",
	}
	if !ok {
		row.Error = "下游超时"
	}
	if err := database.Create(&row).Error; err != nil {
		t.Fatalf("写入审计行失败：%v", err)
	}
}

func TestToolAuditRepo_Available(t *testing.T) {
	database := setupToolAuditTestDB(t)
	if !NewToolAuditRepository(database).Available() {
		t.Error("有句柄时 Available 应为 true")
	}
	if NewToolAuditRepository(nil).Available() {
		t.Error("nil 句柄时 Available 应为 false")
	}
	var nilRepo *ToolAuditRepository // 零值/未装配路径不得 panic
	if nilRepo.Available() {
		t.Error("nil 接收者时 Available 应为 false")
	}
}

// 三个读方法在 nil 句柄时必须**报错**而不是回空结果。
//
// 空结果在本端点是合法值（新部署真的可能一条都没有），所以"查不动"只能靠 error 区分；
// 否则一次句柄丢失就会以"当前无审计"的形式被读成系统健康。
func TestToolAuditRepo_NilHandleErrors(t *testing.T) {
	repo := NewToolAuditRepository(nil)
	ctx := context.Background()
	if _, err := repo.Recent(ctx, "", 10); err == nil {
		t.Error("Recent 应报错")
	}
	if _, err := repo.CountAll(ctx); err == nil {
		t.Error("CountAll 应报错")
	}
	if _, err := repo.CostAggregates(ctx); err == nil {
		t.Error("CostAggregates 应报错")
	}
}

func TestToolAuditRepo_EmptyTable(t *testing.T) {
	repo := NewToolAuditRepository(setupToolAuditTestDB(t))
	ctx := context.Background()
	rows, err := repo.Recent(ctx, "", 10)
	if err != nil {
		t.Fatalf("空表不是错误：%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("空表期望 0 行，实际 %d", len(rows))
	}
	total, err := repo.CountAll(ctx)
	if err != nil || total != 0 {
		t.Fatalf("空表 CountAll 期望 (0,nil)，实际 (%d,%v)", total, err)
	}
	costs, err := repo.CostAggregates(ctx)
	if err != nil || len(costs) != 0 {
		t.Fatalf("空表 CostAggregates 期望 (0 行,nil)，实际 (%d,%v)", len(costs), err)
	}
}

// 倒序 + 过滤 + limit：/agent/tools/audit?tool=x&limit=n 的读侧口径。
func TestToolAuditRepo_RecentOrderAndFilter(t *testing.T) {
	database := setupToolAuditTestDB(t)
	repo := NewToolAuditRepository(database)
	base := time.Now()
	seedAudit(t, database, "reach.sms", true, 100, base)
	seedAudit(t, database, "reach.sms", false, 200, base.Add(time.Second))
	seedAudit(t, database, "kb.search", true, 300, base.Add(2*time.Second))

	all, err := repo.Recent(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("Recent 失败：%v", err)
	}
	if len(all) != 3 {
		t.Fatalf("期望 3 行，实际 %d", len(all))
	}
	if all[0].ExecutedAt.Before(all[1].ExecutedAt) || all[1].ExecutedAt.Before(all[2].ExecutedAt) {
		t.Errorf("应按 executed_at 倒序，实际 %v %v %v", all[0].ExecutedAt, all[1].ExecutedAt, all[2].ExecutedAt)
	}
	if all[0].ToolName != "kb.search" {
		t.Errorf("最新一行应是 kb.search，实际 %s", all[0].ToolName)
	}

	sms, err := repo.Recent(context.Background(), "reach.sms", 10)
	if err != nil || len(sms) != 2 {
		t.Fatalf("按工具过滤期望 2 行，实际 %d（err=%v）", len(sms), err)
	}
	if sms[0].Success {
		t.Error("reach.sms 最新一条应是失败那条")
	}
	if sms[0].Error != "下游超时" {
		t.Errorf("失败原因应落库，实际 %q", sms[0].Error)
	}

	one, err := repo.Recent(context.Background(), "", 1)
	if err != nil || len(one) != 1 {
		t.Fatalf("limit=1 期望 1 行，实际 %d（err=%v）", len(one), err)
	}
	none, err := repo.Recent(context.Background(), "不存在的工具", 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("不存在的工具期望 0 行，实际 %d（err=%v）", len(none), err)
	}
}

// limit 非正数必须报错：GORM 的 Limit(0) 会原样翻成 PG 的 `LIMIT 0`（恒空结果集），
// 于是"参数没传对"会以"最近没有审计"的形式出现。
func TestToolAuditRepo_RecentRejectsNonPositiveLimit(t *testing.T) {
	repo := NewToolAuditRepository(setupToolAuditTestDB(t))
	for _, limit := range []int{0, -1} {
		if _, err := repo.Recent(context.Background(), "", limit); err == nil {
			t.Errorf("limit=%d 应报错", limit)
		}
	}
}

// 聚合口径：/agent/tools/cost?source=db 的数据源，列名与内存版 CostStats 对齐。
func TestToolAuditRepo_CostAggregates(t *testing.T) {
	database := setupToolAuditTestDB(t)
	repo := NewToolAuditRepository(database)
	base := time.Now()
	// kb.search：3 次全成功（150/150/300ms）；reach.sms：2 成功 2 失败
	seedAudit(t, database, "kb.search", true, 150, base)
	seedAudit(t, database, "kb.search", true, 150, base.Add(time.Second))
	seedAudit(t, database, "kb.search", true, 300, base.Add(2*time.Second))
	seedAudit(t, database, "reach.sms", true, 100, base.Add(3*time.Second))
	seedAudit(t, database, "reach.sms", true, 100, base.Add(4*time.Second))
	seedAudit(t, database, "reach.sms", false, 50, base.Add(5*time.Second))
	seedAudit(t, database, "reach.sms", false, 50, base.Add(6*time.Second))

	rows, err := repo.CostAggregates(context.Background())
	if err != nil {
		t.Fatalf("CostAggregates 失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 个工具，实际 %d：%+v", len(rows), rows)
	}
	// 调用数降序 ⇒ reach.sms(4) 在前
	if rows[0].ToolName != "reach.sms" || rows[1].ToolName != "kb.search" {
		t.Errorf("应按 total_calls 降序，实际 %s, %s", rows[0].ToolName, rows[1].ToolName)
	}
	sms, kb := rows[0], rows[1]
	if sms.TotalCalls != 4 || sms.SuccessCalls != 2 || sms.FailedCalls != 2 {
		t.Errorf("reach.sms 计数异常：%+v", sms)
	}
	if sms.TotalDurationMs != 300 {
		t.Errorf("reach.sms 总耗时期望 300，实际 %d", sms.TotalDurationMs)
	}
	if rate := sms.SuccessRate(); rate != 0.5 {
		t.Errorf("reach.sms 成功期望 0.5，实际 %v", rate)
	}
	if avg := sms.AvgDurationMs(); avg != 75 {
		t.Errorf("reach.sms 平均耗时期望 75，实际 %v", avg)
	}
	if kb.TotalCalls != 3 || kb.FailedCalls != 0 || kb.TotalDurationMs != 600 {
		t.Errorf("kb.search 聚合异常：%+v", kb)
	}
	if rate := kb.SuccessRate(); rate < 0.999 || rate > 1.001 {
		t.Errorf("kb.search 成功期望 ≈1.0，实际 %v", rate)
	}
}

// 零调用行必须回 0 而不是 NaN：NaN 无法进 JSON（encoding/json 直接报错），
// 一个空分组就能让整个 /cost 端点 500。
func TestToolAuditCostRow_ZeroCallsNoNaN(t *testing.T) {
	row := ToolAuditCostRow{ToolName: "empty"}
	if got := row.SuccessRate(); got != 0 {
		t.Errorf("零调用成功期望 0，实际 %v", got)
	}
	if got := row.AvgDurationMs(); got != 0 {
		t.Errorf("零调用平均耗时期望 0，实际 %v", got)
	}
}
