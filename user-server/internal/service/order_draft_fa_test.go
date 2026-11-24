// order_draft_fa_test.go — FA-ORDER-01：草稿确认失败必须保留可恢复的建单状态。
//
// 关注点不是"报错句子"，而是控制流：建单失败/关联失败后草稿不得停在
// "已 confirmed、无订单、不可再确认"的死状态——必须持久化为 order_failed +
// 幂等键，且能通过对账入口恰好重建一单（双确认/重启不产生第二单）。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// faOrderSvc 构造一个只带内存 store + 建单 seam 的服务（不依赖 DB / orderService）。
func faOrderSvc(t *testing.T, createFn func(context.Context, *OrderDraft) (*orderRecord, error)) *OrderDraftService {
	t.Helper()
	return &OrderDraftService{
		store:         newMemoryDraftStore(),
		createOrderFn: createFn,
	}
}

// faPendingDraft 直接落一条 pending 草稿。
func faPendingDraft(t *testing.T, svc *OrderDraftService) *OrderDraft {
	t.Helper()
	d := &OrderDraft{
		ID:          generateDraftID(),
		CustomerID:  "cust-fa-" + generateDraftID(),
		OwnerID:     "7",
		ProductName: "FA 产品-" + generateDraftID(),
		Quantity:    1,
		UnitPrice:   100,
		TotalAmount: 100,
		Status:      DraftStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
		Metadata:    map[string]any{},
	}
	if err := svc.store.put(context.Background(), d); err != nil {
		t.Fatalf("预置草稿失败: %v", err)
	}
	return d
}

// TestFAOrder_CreateFailurePersistsRecoverable：建单失败 → 草稿 order_failed + 幂等键 + 原因。
func TestFAOrder_CreateFailurePersistsRecoverable(t *testing.T) {
	ctx := context.Background()
	createErr := errors.New("上游建单超时")
	svc := faOrderSvc(t, func(context.Context, *OrderDraft) (*orderRecord, error) {
		return nil, createErr
	})
	d := faPendingDraft(t, svc)

	_, err := svc.Confirm(ctx, d.ID, "sales-1")
	if err == nil || !strings.Contains(err.Error(), "创建订单失败") {
		t.Fatalf("建单失败应返回可诊断错误，实际 %v", err)
	}
	got, err := svc.store.get(ctx, d.ID)
	if err != nil {
		t.Fatalf("回读草稿失败: %v", err)
	}
	if got.Status != DraftStatusOrderFailed {
		t.Fatalf("建单失败后状态应为 %s，实际 %s", DraftStatusOrderFailed, got.Status)
	}
	if got.IdempotencyKey == "" {
		t.Fatal("建单失败后应固化幂等键")
	}
	if !strings.Contains(got.OrderFailedReason, createErr.Error()) {
		t.Fatalf("失败原因应记录建单错误，实际 %q", got.OrderFailedReason)
	}
}

// TestFAOrder_AssocFailurePersistsRecoverable：订单已建但关联写失败 → order_failed，不盲目重复建单。
func TestFAOrder_AssocFailurePersistsRecoverable(t *testing.T) {
	ctx := context.Background()
	svc := faOrderSvc(t, func(_ context.Context, d *OrderDraft) (*orderRecord, error) {
		return &orderRecord{ID: "order-fa-1", AccountID: d.CustomerID, Status: "pending"}, nil
	})
	// 用一个 put 在关联阶段失败、首阶段成功的 store 包裹内存 store。
	svc.store = &assocFailStore{draftStore: newMemoryDraftStore(), failOnN: 2}
	d := faPendingDraft(t, svc)

	_, err := svc.Confirm(ctx, d.ID, "sales-1")
	if err == nil || !strings.Contains(err.Error(), "关联未落库") {
		t.Fatalf("关联失败应返回明确错误，实际 %v", err)
	}
	got, _ := svc.store.get(ctx, d.ID)
	if got.Status != DraftStatusOrderFailed || got.OrderID != "order-fa-1" {
		t.Fatalf("关联失败应保留已建订单号并标记 order_failed，实际 status=%s orderID=%s", got.Status, got.OrderID)
	}
	// 对账：只回填关联，不重新建单（createFn 若再被调用会让计数增加）。
	createCalls := 0
	svc.createOrderFn = func(_ context.Context, d *OrderDraft) (*orderRecord, error) {
		createCalls++
		return &orderRecord{ID: "order-fa-DUP", Status: "pending"}, nil
	}
	rec, err := svc.ReconcileDraft(ctx, d.ID)
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if rec.OrderID != "order-fa-1" || createCalls != 0 {
		t.Fatalf("对账应只回填已建订单且不重复建单，orderID=%s createCalls=%d", rec.OrderID, createCalls)
	}
	got, _ = svc.store.get(ctx, d.ID)
	if got.Status != DraftStatusConfirmed || got.OrderFailedReason != "" {
		t.Fatalf("对账后应 confirmed 且清空失败原因，实际 status=%s reason=%q", got.Status, got.OrderFailedReason)
	}
}

// TestFAOrder_ReconcileRebuildsExactlyOne：建单失败（无订单）后对账按幂等键重建恰好一单。
func TestFAOrder_ReconcileRebuildsExactlyOne(t *testing.T) {
	ctx := context.Background()
	built := map[string]string{} // idempotencyKey -> orderID，fake 幂等
	svc := faOrderSvc(t, func(_ context.Context, d *OrderDraft) (*orderRecord, error) {
		key := d.IdempotencyKey
		if key == "" {
			key = d.ID
		}
		if id, ok := built[key]; ok {
			return &orderRecord{ID: id, Status: "pending"}, nil
		}
		id := "order-fa-" + key
		built[key] = id
		return &orderRecord{ID: id, Status: "pending"}, nil
	})
	d := faPendingDraft(t, svc)

	// 第一次确认成功建单，但用响应丢失场景（store 里 OrderID 未回填）模拟重启前状态。
	first, err := svc.Confirm(ctx, d.ID, "sales-1")
	if err != nil {
		t.Fatalf("首次确认失败: %v", err)
	}
	if len(built) != 1 {
		t.Fatalf("首次确认应恰建一单，实际 %d", len(built))
	}

	// 模拟"建单成功但草稿未落关联"后重启：清空草稿 OrderID 并标记 order_failed。
	// 关键：幂等键保持 Confirm 首次已固化的值（draft.ID），不改它——对账靠同一把键去重。
	fresh, _ := svc.store.get(ctx, d.ID)
	fresh.OrderID = ""
	fresh.Status = DraftStatusOrderFailed
	// 不修改 fresh.IdempotencyKey（仍为 Confirm 固化的 draft.ID）。
	_ = svc.store.put(ctx, fresh)

	rec, err := svc.ReconcileDraft(ctx, d.ID)
	if err != nil {
		t.Fatalf("对账重建失败: %v", err)
	}
	if len(built) != 1 {
		t.Fatalf("对账后仍应恰好一单（幂等去重），实际 %d", len(built))
	}
	if rec.OrderID != first.OrderID {
		t.Fatalf("对账应复用同一订单号，first=%s reconcile=%s", first.OrderID, rec.OrderID)
	}
}

// TestFAOrder_DoubleConfirmNoDuplicate：双确认（第二次在第一次建单成功后）不产生第二单。
func TestFAOrder_DoubleConfirmNoDuplicate(t *testing.T) {
	ctx := context.Background()
	built := map[string]string{}
	svc := faOrderSvc(t, func(_ context.Context, d *OrderDraft) (*orderRecord, error) {
		key := d.IdempotencyKey
		if id, ok := built[key]; ok {
			return &orderRecord{ID: id, Status: "pending"}, nil
		}
		id := "order-fa-" + key
		built[key] = id
		return &orderRecord{ID: id, Status: "pending"}, nil
	})
	d := faPendingDraft(t, svc)

	first, err := svc.Confirm(ctx, d.ID, "sales-1")
	if err != nil {
		t.Fatalf("首次确认失败: %v", err)
	}
	// 第二次确认：草稿已 confirmed（非 pending）→ 应被拒，不再建单。
	if _, err := svc.Confirm(ctx, d.ID, "sales-1"); err == nil {
		t.Fatal("已确认草稿再次确认应被拒")
	}
	if len(built) != 1 {
		t.Fatalf("双确认不应产生第二单，实际 %d", len(built))
	}
	got, _ := svc.store.get(ctx, d.ID)
	if got.OrderID != first.OrderID {
		t.Fatalf("草稿应保留首次订单号，实际 %s", got.OrderID)
	}
}

// assocFailStore：让第 failOnN 次 put 失败，用于注入"关联写失败"。
type assocFailStore struct {
	draftStore
	puts    int
	failOnN int
}

func (s *assocFailStore) put(ctx context.Context, d *OrderDraft) error {
	s.puts++
	if s.puts == s.failOnN {
		return errors.New("关联落库失败（注入）")
	}
	return s.draftStore.put(ctx, d)
}
