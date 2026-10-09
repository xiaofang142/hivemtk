// order_draft_sentinels_test.go 草稿三类可预期失败的哨兵契约。
//
// 这组断言的对象是"HTTP 层能不能靠 errors.Is 分岔"，不是"报错句子好不好看"：
// 控制器把 不存在/已过 pending 态/已过期 翻成 404/409/409，判据是哨兵本身。
// 消息子串也一起断言，因为服务层的原句子同时是既有用例与前端提示的输入，
// 换文案会静默改掉两边的判据。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// draftForSentinel 直接落一条可控状态的草稿（同包所以够得着 store）。
func draftForSentinel(t *testing.T, svc *OrderDraftService, status DraftStatus, expiresAt time.Time) *OrderDraft {
	t.Helper()
	d := &OrderDraft{
		ID:          generateDraftID(),
		CustomerID:  "cust-sentinel-" + string(status) + "-" + generateDraftID(),
		OwnerID:     "7",
		ProductName: "哨兵产品-" + generateDraftID(),
		Quantity:    1,
		UnitPrice:   100,
		TotalAmount: 100,
		Confidence:  0.9,
		Source:      "manual",
		Status:      status,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
		Metadata:    map[string]any{},
	}
	if err := svc.store.put(context.Background(), d); err != nil {
		t.Fatalf("预置草稿失败：%v", err)
	}
	return d
}

func TestOrderDraftSentinels_Confirm(t *testing.T) {
	svc := NewOrderDraftService(nil)
	ctx := context.Background()

	_, err := svc.Confirm(ctx, "draft_absent", "7")
	if !errors.Is(err, ErrOrderDraftNotFound) {
		t.Errorf("确认不存在的草稿应命中 ErrOrderDraftNotFound，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("原句子子串要留住（既有用例与前端提示都读它），实际 %q", err.Error())
	}

	pending := draftForSentinel(t, svc, DraftStatusPending, time.Now().Add(time.Hour))
	if _, err := svc.Confirm(ctx, pending.ID, "7"); err != nil {
		t.Fatalf("首次确认失败：%v", err)
	}
	_, err = svc.Confirm(ctx, pending.ID, "7")
	if !errors.Is(err, ErrOrderDraftNotPending) {
		t.Errorf("重复确认应命中 ErrOrderDraftNotPending，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "不可确认") {
		t.Errorf("重复确认的句子要留住「不可确认」，实际 %q", err.Error())
	}

	expired := draftForSentinel(t, svc, DraftStatusPending, time.Now().Add(-time.Minute))
	_, err = svc.Confirm(ctx, expired.ID, "7")
	if !errors.Is(err, ErrOrderDraftExpired) {
		t.Errorf("过期草稿应命中 ErrOrderDraftExpired，实际 %v", err)
	}
	// 过期那条会就地翻状态：翻完之后再确认，读到的就是"已过 pending 态"。
	cur := mustDraftForSentinel(t, svc, expired.ID)
	if cur.Status != DraftStatusExpired {
		t.Errorf("确认过期草稿时应把它翻成 expired，实际 %s", cur.Status)
	}
}

func TestOrderDraftSentinels_CancelAndEdit(t *testing.T) {
	svc := NewOrderDraftService(nil)
	ctx := context.Background()

	for _, call := range []struct {
		name string
		fn   func(id string) error
	}{
		{"cancel", func(id string) error { return svc.Cancel(ctx, id, "理由", "7") }},
		{"edit", func(id string) error { return svc.Edit(ctx, id, DraftUpdates{Quantity: intPtr(2)}) }},
	} {
		err := call.fn("draft_absent")
		if !errors.Is(err, ErrOrderDraftNotFound) {
			t.Errorf("%s 不存在的草稿应命中 ErrOrderDraftNotFound，实际 %v", call.name, err)
		}
	}

	terminal := draftForSentinel(t, svc, DraftStatusCancelled, time.Now().Add(time.Hour))
	if err := svc.Cancel(ctx, terminal.ID, "再来一次", "7"); !errors.Is(err, ErrOrderDraftNotPending) {
		t.Errorf("取消已取消的草稿应命中 ErrOrderDraftNotPending，实际 %v", err)
	} else if !strings.Contains(err.Error(), "不可取消") {
		t.Errorf("取消失败的句子要留住「不可取消」，实际 %q", err.Error())
	}
	if err := svc.Edit(ctx, terminal.ID, DraftUpdates{Quantity: intPtr(3)}); !errors.Is(err, ErrOrderDraftNotPending) {
		t.Errorf("编辑已取消的草稿应命中 ErrOrderDraftNotPending，实际 %v", err)
	} else if !strings.Contains(err.Error(), "不可编辑") {
		t.Errorf("编辑失败的句子要留住「不可编辑」，实际 %q", err.Error())
	}
}

// TestOrderDraftEditRoundsTotalToCents 改价后重算总额要夹到分。
//
// 19.99*3 在 float64 里是 59.969999999999995；不夹的话这个尾巴会一路写进
// NUMERIC(12,2) 列并回给前端，与创建侧（roundMoney）两套口径。
func TestOrderDraftEditRoundsTotalToCents(t *testing.T) {
	svc := NewOrderDraftService(nil)
	ctx := context.Background()
	d := draftForSentinel(t, svc, DraftStatusPending, time.Now().Add(time.Hour))

	if err := svc.Edit(ctx, d.ID, DraftUpdates{UnitPrice: float64Ptr(19.99), Quantity: intPtr(3)}); err != nil {
		t.Fatalf("改价失败：%v", err)
	}
	got := mustDraftForSentinel(t, svc, d.ID)
	if got.TotalAmount != 59.97 {
		t.Errorf("总额应为 59.97，实际 %v", got.TotalAmount)
	}
}

func mustDraftForSentinel(t *testing.T, svc *OrderDraftService, id string) *OrderDraft {
	t.Helper()
	d, err := svc.store.get(context.Background(), id)
	if err != nil || d == nil {
		t.Fatalf("读回草稿 %s 失败：%v / %v", id, err, d)
	}
	return d
}

func intPtr(v int) *int             { return &v }
func float64Ptr(v float64) *float64 { return &v }
