package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	_type "hivemtk-user/internal/pkg/utils/type"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestOrderRepository_TenantScope EC-07「跨租户/买家越权读单拒绝」：
// 带租户上下文的读取只能看到本租户订单，跨租户 ID 不可读；无租户上下文回落默认租户 1。
func TestOrderRepository_TenantScope(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Order{})
	repo := NewOrderRepositoryWithDB(db)

	mine := &model.Order{TenantID: 1, Status: _type.OrderStatusPending, Price: "99.00", TgID: 111, AccountID: "acc-1"}
	theirs := &model.Order{TenantID: 2, Status: _type.OrderStatusPending, Price: "88.00", TgID: 222, AccountID: "acc-2"}
	if err := db.Create(mine).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(theirs).Error; err != nil {
		t.Fatal(err)
	}

	ctxMine := tenant.WithTenantID(context.Background(), 1)

	t.Run("跨租户读单拒绝", func(t *testing.T) {
		_, err := repo.GetByStringID(ctxMine, theirs.ID)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("租户1不应读到租户2订单，期望 ErrRecordNotFound，实际=%v", err)
		}
	})

	t.Run("本租户可读", func(t *testing.T) {
		got, err := repo.GetByStringID(ctxMine, mine.ID)
		if err != nil || got.ID != mine.ID {
			t.Fatalf("本租户订单应可读，got=%+v err=%v", got, err)
		}
	})

	t.Run("列表仅本租户", func(t *testing.T) {
		rows, _, err := repo.GetOrderList(ctxMine, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.TenantID != 1 {
				t.Fatalf("列表不应含租户2订单，实际 TenantID=%d", row.TenantID)
			}
		}
	})

	t.Run("无租户上下文回落默认租户1", func(t *testing.T) {
		got, err := repo.GetByStringID(context.Background(), mine.ID)
		if err != nil || got.ID != mine.ID {
			t.Fatalf("无 tenant ctx 应回落 default 1 并读到租户1订单，got=%+v err=%v", got, err)
		}
		if _, err := repo.GetByStringID(context.Background(), theirs.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("无 tenant ctx 回落 default 1 后不应读到租户2订单，实际=%v", err)
		}
	})
}
