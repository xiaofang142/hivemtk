package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

func TestModuleSyncRefreshesOrderStatus(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")
	const orderNo = "MOD-TEST-ORDER-SYNC"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  []string{"core", "reach-link"},
				"menu_keys":    []string{"system"},
				"catalog": []map[string]any{
					{"module_key": "core", "name": "x", "category": "base", "owned": true, "owned_status": "active", "owned_source": "default_base", "menu_keys": []string{"system"}},
					{"module_key": "reach-link", "name": "y", "category": "paid", "price_cents": 38000, "owned": true, "owned_status": "active", "owned_source": "purchase", "menu_keys": []string{"shortLink"}},
				},
			}})
		case "/merchant-api/orders":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 1,
				"list":  []map[string]any{{"order_no": orderNo, "status": "fulfilled"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Secret 必填：平台客户端对每次请求做 HMAC 签名，缺 Secret 会在**发出请求前**
	// 直接报错 → Sync 退回离线兜底 —— 本用例测不到订单状态刷新这条链路。
	// 夹具同时保证 PLATFORM_ENABLED 开启：平台地址的解析入口 config.PlatformURL()
	// 在关态恒返回空串，只设 APIURL 会拼出无协议的相对路径。
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test" + "-secret"})

	database := testutil.NewTestDB(t, &model.ModuleOrder{}, &model.TenantModule{}, &model.ModuleSyncLog{})
	db.SetTestDB(database)
	for _, m := range []any{&model.ModuleOrder{}, &model.TenantModule{}, &model.ModuleSyncLog{}} {
		if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(m).Error; err != nil {
			t.Fatalf("清空: %v", err)
		}
	}

	svc := NewModuleCatalogService()
	if err := svc.order.Upsert(context.Background(), &model.ModuleOrder{
		OrderNo: orderNo, ClientOrderNo: "STALE-1", OrderType: "module",
		Status: model.ModuleOrderPendingPayment, TotalAmountCents: 38000,
		Currency: "CNY", Items: "[]", PaymentGuide: "旧指引",
	}); err != nil {
		t.Fatalf("插入陈旧订单: %v", err)
	}

	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if res.Degraded {
		t.Fatalf("不应降级: %s", res.Message)
	}
	got, _ := svc.order.GetByOrderNo(context.Background(), orderNo)
	if got == nil {
		t.Fatal("订单丢失")
	}
	t.Logf("同步结果: active=%d source=%s", res.ActiveCount, res.Source)
	t.Logf("订单状态: %s -> 期望 fulfilled", got.Status)
	if got.Status != model.ModuleOrderFulfilled {
		t.Errorf("订单状态应刷新为 fulfilled，实际 %q", got.Status)
	}
	if got.PaymentGuide != "旧指引" {
		t.Errorf("快照字段不应被覆盖，实际 %q", got.PaymentGuide)
	}
	if got.PaidAt == nil {
		t.Error("应回填 paid_at")
	}
}
