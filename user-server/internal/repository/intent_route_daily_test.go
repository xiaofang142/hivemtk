package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestIntentRouteDailyRepo_UpsertDiffGetRow 真库验证 UpsertDiff 增量累加、
// 五列 PK 定位、GetRow 读单行/未命中 (nil,nil)、写侧租户 stamps、纠错事件行。
func TestIntentRouteDailyRepo_UpsertDiffGetRow(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentRouteDaily{})
	repo := NewIntentRouteDailyRepositoryWithDB(db)
	ctx := context.Background()

	const day = "2026-11-10"

	// 1) 新增链级行（chain_name=IntentCode ∧ tool_name=''）
	if err := repo.UpsertDiff(ctx, &model.IntentRouteDaily{
		Date: day, IntentType: "order_status", ChainName: "order_status", ToolName: "",
		ChainMisses: 1,
	}); err != nil {
		t.Fatalf("UpsertDiff 链级行失败: %v", err)
	}
	row, err := repo.GetRow(ctx, day, "order_status", "order_status", "")
	if err != nil {
		t.Fatalf("GetRow 失败: %v", err)
	}
	if row == nil {
		t.Fatal("GetRow 应命中链级行，实得 nil")
	}
	if row.ChainMisses != 1 || row.ChainHits != 0 {
		t.Fatalf("链级行计数错误: %+v", row)
	}
	if row.TenantID != tenant.DefaultTenantID {
		t.Fatalf("无租户上下文应 stamps DefaultTenantID，实得 %d", row.TenantID)
	}

	// 2) 同 PK 再累加：计数为增量之和而非覆盖
	if err := repo.UpsertDiff(ctx, &model.IntentRouteDaily{
		Date: day, IntentType: "order_status", ChainName: "order_status", ToolName: "",
		ChainHits: 2, ToolCalls: 3,
	}); err != nil {
		t.Fatalf("UpsertDiff 累加失败: %v", err)
	}
	row, err = repo.GetRow(ctx, day, "order_status", "order_status", "")
	if err != nil {
		t.Fatalf("GetRow 累加后失败: %v", err)
	}
	if row == nil {
		t.Fatal("累加后 GetRow 应命中")
	}
	if row.ChainHits != 2 || row.ChainMisses != 1 || row.ToolCalls != 3 {
		t.Fatalf("累加语义错误（期望 chain_hits=2 chain_misses=1 tool_calls=3）: %+v", row)
	}

	// 3) 工具级行与纠错事件行（tool_name='__correction__'）
	if err := repo.UpsertDiff(ctx, &model.IntentRouteDaily{
		Date: day, IntentType: "complaint", ChainName: "complaint", ToolName: "order.lookup",
		ToolHits: 1, ToolSuccess: 1,
	}); err != nil {
		t.Fatalf("UpsertDiff 工具级行失败: %v", err)
	}
	if err := repo.UpsertDiff(ctx, &model.IntentRouteDaily{
		Date: day, IntentType: "refund", ChainName: "", ToolName: "__correction__",
		ToolCalls: 1,
	}); err != nil {
		t.Fatalf("UpsertDiff 纠错事件行失败: %v", err)
	}
	corr, err := repo.GetRow(ctx, day, "refund", "", "__correction__")
	if err != nil {
		t.Fatalf("GetRow 纠错行失败: %v", err)
	}
	if corr == nil || corr.ToolCalls != 1 {
		t.Fatalf("纠错事件行错误: %+v err=%v", corr, err)
	}
	toolRow, err := repo.GetRow(ctx, day, "complaint", "complaint", "order.lookup")
	if err != nil {
		t.Fatalf("GetRow 工具级行失败: %v", err)
	}
	if toolRow == nil || toolRow.ToolSuccess != 1 {
		t.Fatalf("工具级行错误: %+v", toolRow)
	}

	// 4) 未命中 → (nil, nil)
	miss, err := repo.GetRow(ctx, day, "faq", "faq", "")
	if err != nil {
		t.Fatalf("GetRow 未命中不应报错: %v", err)
	}
	if miss != nil {
		t.Fatalf("GetRow 未命中应返 nil，实得 %+v", miss)
	}

	// 5) 显式租户 stamps
	ctxA := tenant.WithTenantID(context.Background(), 42)
	if err := repo.UpsertDiff(ctxA, &model.IntentRouteDaily{
		Date: day, IntentType: "greeting", ChainName: "greeting", ToolName: "",
		ChainHits: 1,
	}); err != nil {
		t.Fatalf("UpsertDiff 租户 42 失败: %v", err)
	}
	rowA, err := repo.GetRow(ctxA, day, "greeting", "greeting", "")
	if err != nil {
		t.Fatalf("GetRow 租户 42 失败: %v", err)
	}
	if rowA == nil || rowA.TenantID != 42 {
		t.Fatalf("租户 42 行错误: %+v", rowA)
	}

	// 6) db nil 守卫
	nilRepo := NewIntentRouteDailyRepositoryWithDB(nil)
	if err := nilRepo.UpsertDiff(ctx, &model.IntentRouteDaily{Date: day}); !errors.Is(err, errIntentRouteDailyDBNotReady) {
		t.Fatalf("nil db UpsertDiff 应返 errIntentRouteDailyDBNotReady，实得 %v", err)
	}
	if _, err := nilRepo.GetRow(ctx, day, "x", "x", ""); !errors.Is(err, errIntentRouteDailyDBNotReady) {
		t.Fatalf("nil db GetRow 应返 errIntentRouteDailyDBNotReady，实得 %v", err)
	}

	// 7) nil delta 守卫
	if err := repo.UpsertDiff(ctx, nil); err == nil {
		t.Fatal("nil delta 应报错")
	}
}
