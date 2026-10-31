package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// S-30（2026-10-31 审计）：recallFallback 的候选池必须有硬上界——
// 降级路径整表载入客户全部记忆（含 embedding 串）会随条数线性退化。
// 本用例钉住：超过 limit 的存量记忆不会被整表拉出，且按 importance 降序取前 K。
func TestListLongTermMemoriesForFallbackBoundedByLimit(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	db := testutil.NewTestDB(t, &model.CustomerLongTermMemory{})
	if db == nil {
		t.Fatal("测试库不可达：上界口径无法判定")
	}
	ctx := context.Background()
	repo := NewMemoryRepositoryWithDB(db)

	const total = 12
	now := time.Now()
	for i := 0; i < total; i++ {
		item := &model.CustomerLongTermMemory{
			CustomerID: "cust-fb-bound",
			MemoryType: model.LongTermMemoryType("fact"),
			Source:     model.LongTermMemorySource("auto"),
			Content:    "记忆条目",
			Importance: i + 1, // 重要性递增：期望按 importance DESC 取前 5
			Embedding: "[" + func() string {
				s := "0.1"
				for i := 1; i < 1024; i++ {
					s += ",0.1"
				}
				return s
			}() + "]",
			CreatedAt: now,
		}
		if err := repo.CreateLongTermMemory(ctx, item); err != nil {
			t.Fatalf("种子第 %d 条失败: %v", i, err)
		}
	}

	items, err := repo.ListLongTermMemoriesForFallback(ctx, "cust-fb-bound", now, 5)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("上界失效：limit=5 却拉回 %d 条（存量 %d）", len(items), total)
	}
	if items[0].Importance != total {
		t.Errorf("排序失效：首条 importance=%d, want %d（应为 importance DESC 首条）", items[0].Importance, total)
	}

	// limit<=0 保持显式全量语义（测试/迁移场景专用），但生产调用恒传正数。
	all, err := repo.ListLongTermMemoriesForFallback(ctx, "cust-fb-bound", now, 0)
	if err != nil || len(all) != total {
		t.Errorf("limit<=0 全量语义被破坏: got %d 条, err=%v", len(all), err)
	}
}
