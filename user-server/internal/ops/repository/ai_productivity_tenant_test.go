// ai_productivity_tenant_test.go S-18（specs/001 审计二期）：AI 产能看板跨租户聚合回归。
//
// GetAvgResponseTime 曾把派生表写成 `Table("(SELECT … FROM session_messages …)")`
// 字面量：派生表外层解析不出表名 ⇒ 隔离 callback 不注入；字面量里的 FROM 也在
// callback 的 JOIN/预构建 SQL 扫描面之外 ⇒ on 模式下把**全部租户**的
// session_messages 聚进同一个「平均响应时长」（/api/analytics/ai-productivity
// 是 auth 组租户身份端点）。修法：派生表改 GORM 子查询
// （*gorm.DB 经 callbacks.Query().Execute(DryRun) 展开，租户注入照常生效）。
//
// 本用例钉死两件事：
//  1. 正对照（防空转绿）：off 模式（隔离直通）下同一时间窗内的混池平均必须是
//     (60+3600)/2=1830 —— 证明两个租户的行都在窗口里，排除租户 B 靠的是注入，
//     不是数据窗口或播种失败；
//  2. 隔离判据：on 模式下租户 A 只能用 A 自己的行（60s），租户 B 只能用 B 的（3600s）。
package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

const a18PooledAvg = 1830.0 // (60s + 3600s) / 2

func TestGetAvgResponseTime_TenantScopedSubquery(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	if db == nil {
		t.Fatal("测试库不可达：AI 产能跨租户聚合判不了（不 Skip，跳过等于没锁）")
	}
	prev := dbutil.GetDB()
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(prev) })

	base := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	seed := func(tenantID uint, session string, gap time.Duration) {
		t.Helper()
		ctx := tenant.WithTenantID(context.Background(), tenantID)
		rows := []model.SessionMessage{
			{SessionID: session, SenderType: "user", Content: "q", CreatedAt: base},
			{SessionID: session, SenderType: "ai", Content: "a", CreatedAt: base.Add(gap)},
		}
		if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
			t.Fatalf("播种租户 %d 会话失败: %v", tenantID, err)
		}
	}
	const (
		tenantA = uint(51)
		tenantB = uint(52)
	)
	seed(tenantA, "s-a18-a", 60*time.Second)
	seed(tenantB, "s-a18-b", 3600*time.Second)

	repo := NewAIProductivityRepository()
	start, end := base.Add(-time.Hour), base.Add(2*time.Hour)

	// 混池对照：off 模式 callback 直通，两租户的行都进平均。
	// 没有这一步，下面的"租户 A = 60"可能只是 B 的行根本没进窗口（空转绿）。
	tenant.SetMode(tenant.ModeOff)
	pooled, err := repo.GetAvgResponseTime(context.Background(), start, end)
	if err != nil {
		t.Fatalf("off 模式混池查询失败: %v", err)
	}
	if math.Abs(pooled-a18PooledAvg) > 0.001 {
		t.Fatalf("混池对照失败：got %v want %v —— 两租户的行没同时进窗口，隔离断言会空转", pooled, a18PooledAvg)
	}

	// on 模式：各租户只见自己的行。
	tenant.SetMode(tenant.ModeOn)
	avgA, err := repo.GetAvgResponseTime(tenant.WithTenantID(context.Background(), tenantA), start, end)
	if err != nil {
		t.Fatalf("租户 A 查询失败: %v", err)
	}
	if math.Abs(avgA-60) > 0.001 {
		t.Fatalf("跨租户聚合泄漏：租户 %d 的平均响应时长 got %v want 60（混入了租户 %d 的 3600s 行距）",
			tenantA, avgA, tenantB)
	}
	avgB, err := repo.GetAvgResponseTime(tenant.WithTenantID(context.Background(), tenantB), start, end)
	if err != nil {
		t.Fatalf("租户 B 查询失败: %v", err)
	}
	if math.Abs(avgB-3600) > 0.001 {
		t.Fatalf("租户 B 平均响应时长 got %v want 3600（被租户 %d 的 60s 行距拉低）", avgB, tenantA)
	}
}
