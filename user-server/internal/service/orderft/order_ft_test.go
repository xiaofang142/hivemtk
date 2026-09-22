package orderft

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// TestOrderWebhookAndCustomerLookup_FT 订单功能测试（独立包，规避 service 包内损坏的其它测试文件）：
// 1) Webhook 推送订单 -> 入库(upsert)
// 2) 再次推送同订单不同状态 -> 验证 upsert(不新增行, 只更新状态)
// 3) 按客户手机/姓名查询 -> 验证 360 视图可读到且状态已更新、金额单位为分
func TestOrderWebhookAndCustomerLookup_FT(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ExternalOrder{}, &model.WebhookEvent{})
	db.SetTestDB(database)

	svc := service.NewIntegrationService()
	ctx := context.Background()

	const (
		phone = "13800009999"
		name  = "功能测试客户"
	)

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "FT20260723001", "已付款", map[string]any{
		"order_no":     "NO-FT-001",
		"user_phone":   phone,
		"user_name":    name,
		"order_time":   "2026-07-23 10:00:00",
		"total_amount": float64(19900),
		"pay_amount":   float64(19900),
		"items":        `[{"title":"测试商品","price":199.00,"quantity":1}]`,
	}); err != nil {
		t.Fatalf("UpsertOrderFromWebhook(1st) failed: %v", err)
	}

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "FT20260723001", "已完成", map[string]any{
		"user_phone": phone,
		"user_name":  name,
	}); err != nil {
		t.Fatalf("UpsertOrderFromWebhook(2nd) failed: %v", err)
	}

	orders, err := svc.GetExternalOrdersByCustomer(ctx, phone, name)
	if err != nil {
		t.Fatalf("GetExternalOrdersByCustomer failed: %v", err)
	}

	var found *model.ExternalOrder
	for i := range orders {
		if orders[i].OrderID == "FT20260723001" {
			found = orders[i]
		}
	}
	if found == nil {
		t.Fatalf("订单 FT20260723001 未出现在客户查询结果中 (共 %d 条)", len(orders))
	}
	if found.Status != "已完成" {
		t.Errorf("upsert 后状态期望=已完成, 实际=%s", found.Status)
	}
	if found.PayAmount != 19900 {
		t.Errorf("pay_amount 期望=19900(分), 实际=%d", found.PayAmount)
	}
	if found.Platform != "taobao" {
		t.Errorf("platform 期望=taobao, 实际=%s", found.Platform)
	}
	if found.OrderTime == nil {
		t.Errorf("order_time 期望被 webhook 回填, 实际为 nil")
	}

	t.Logf("PASS: order_id=%s platform=%s status=%s pay=%.2f元 items=%s",
		found.OrderID, found.Platform, found.Status, float64(found.PayAmount)/100, found.Items)
}
