package service

// 契约：草稿从库模型转回 service 结构时，tenant_id 必须带回来。
//
// 坑（2026-11-01 API 全量回归实测）：draftFromModel 逐字段手工映射，漏了
// TenantID。GET /api/manage/order-drafts/{id} 的响应体于是恒为 tenant_id:0，
// 而库里那行属于租户 1 —— on 模式下前端/审计拿到的「这条草稿归谁」是假的，
// 而且它长得像「平台公共池」（0 的列语义），排查时会往完全错误的方向走。
//
// 本文件只锁读方向；写方向刻意不回填（tenant_id 由 GORM 回调按请求上下文写入，
// 显式写 0 会触发 on 模式 fail-closed）。

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestDraftFromModelKeepsTenantID(t *testing.T) {
	const tid = uint(7)
	m := &model.OrderDraft{
		TenantID:    tid,
		ID:          "draft_test_1",
		CustomerID:  "cust-1",
		OwnerID:     "system",
		ProductName: "测试产品",
		Quantity:    2,
		UnitPrice:   10,
		TotalAmount: 20,
		Status:      model.OrderDraftStatusPending,
	}

	got := draftFromModel(m)
	if got.TenantID != tid {
		t.Fatalf("读口 tenant_id 必须回填：want %d, got %d（响应体会把归属渲染成 0）", tid, got.TenantID)
	}

	// 其余关键字段不能被这次修复带偏
	if got.ID != m.ID || got.CustomerID != m.CustomerID || got.ProductName != m.ProductName {
		t.Fatalf("字段映射被破坏：%+v", got)
	}
}

// 批量读口走的是同一个映射函数，这里再钉一次，防止以后有人给列表另写一份。
func TestDraftsFromModelsKeepsTenantID(t *testing.T) {
	rows := []*model.OrderDraft{
		{TenantID: 3, ID: "draft_a", Status: model.OrderDraftStatusPending},
		{TenantID: 4, ID: "draft_b", Status: model.OrderDraftStatusPending},
	}
	out := draftsFromModels(rows)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	for i, d := range out {
		if d.TenantID != rows[i].TenantID {
			t.Fatalf("第 %d 条 tenant_id 丢失：want %d got %d", i, rows[i].TenantID, d.TenantID)
		}
	}
}
