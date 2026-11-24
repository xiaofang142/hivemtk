package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 平台交互「租户归属参数」端到端测试（与 platform-server 契约对齐）：
//  1. sync 请求 URL 带 ?tenant_code=<本租户 code>（平台据此下发该租户的订阅段）；
//  2. 订阅续费订单 body 含 tenant_code；
//  3. 取不到 code（无租户行）→ 传空串、照常走通（平台旧行为兜底）。

// tenantCodeCaptures 捕获 sync query 与下单 body 的桩平台。
//
// 经带缓冲 channel 回传（对齐 client_test.go refusalServer 的理由）：handler
// 跑在服务器 goroutine 上，共享变量会被 -race 判竞态。
type tenantCodeCaptures struct {
	syncQueryCh chan string
	orderBodyCh chan []byte
}

func setupTenantCodeStub(t *testing.T) *tenantCodeCaptures {
	t.Helper()
	cap := &tenantCodeCaptures{
		syncQueryCh: make(chan string, 8),
		orderBodyCh: make(chan []byte, 8),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			select {
			case cap.syncQueryCh <- r.URL.RawQuery:
			default:
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  []string{"core"},
				"menu_keys":    []string{},
				"catalog": []map[string]any{
					{"module_key": "core", "name": "core", "category": "base",
						"owned": true, "owned_status": "active", "owned_source": "default_base", "menu_keys": []string{}},
				},
			}})
		case "/merchant-api/orders":
			// 续费下单（POST）与订单状态列表（GET）同路径：按方法分流
			if r.Method == http.MethodPost {
				body, _ := io.ReadAll(r.Body)
				select {
				case cap.orderBodyCh <- body:
				default:
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
					"order_no": "SUB-TENANT-CODE-1", "status": "pending_payment",
					"total_amount_cents": 14900, "items": []any{},
					"payment_guide": "联系运营付款",
					// 平台通道就绪时下发的支付参数段（003 T018）：
					// 模块/订阅/LLM 三条下单链路都必须原样透传，否则前端只能
					// 回落到"加微信/联系运营"——本期明确要废弃的旧流程。
					"payment": map[string]any{
						"gateway_url": "https://pay.example.com",
						"pay_url":     "https://pay.example.com/submit.php",
						"params":      map[string]string{"pid": "1295", "sign": "x"},
						"method":      "POST",
					},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 0, "list": []map[string]any{},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-" + "secret"})
	return cap
}

func setupTenantCodeDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.Tenant{}, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	db.SetTestDB(database)
	// on 模式下无绑定删除会被 fail-closed：清空挂 SystemScope（cleanup 跑时 mode 仍是 on，
	// 顺序约定与 clearModuleTablesOnMode 相同：先 withTenantMode 再本函数）
	t.Cleanup(func() {
		sysCtx := tenant.WithSystemScope(context.Background())
		for _, m := range []any{&model.Tenant{}, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{}} {
			if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).
				WithContext(sysCtx).Delete(m).Error; err != nil {
				t.Logf("清理 %T 失败: %v", m, err)
			}
		}
	})
	return database
}

// TestModuleSyncSendsTenantCodeQuery on 模式：sync 请求必须带本租户 code。
func TestModuleSyncSendsTenantCodeQuery(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupTenantCodeDB(t)
	stub := setupTenantCodeStub(t)

	tn := &model.Tenant{Code: "tCODE01", Name: "带码租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}

	// 请求 ctx 绑定该租户（与 TenantMiddleware → 手动同步按钮同形）；
	// Sync 内部挂 SystemScope 后仍应解析出绑定租户的 code。
	ctx := tenant.WithTenantID(context.Background(), tn.ID)
	svc := NewModuleCatalogService()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if got := <-stub.syncQueryCh; got != "tenant_code=tCODE01" {
		t.Errorf("sync URL 应带 ?tenant_code=tCODE01，实际 %q", got)
	}
}

// TestModuleSyncEmptyTenantCodeWhenNoTenantRow 租户表无对应行 → 传空串照发
// （平台旧行为兜底），不许报错。
func TestModuleSyncEmptyTenantCodeWhenNoTenantRow(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	setupTenantCodeDB(t)
	stub := setupTenantCodeStub(t)

	// 绑定一个不存在的租户 id：查不到行 → code 空串
	ctx := tenant.WithTenantID(context.Background(), 4242)
	svc := NewModuleCatalogService()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("查不到租户行时 Sync 不应报错: %v", err)
	}
	if got := <-stub.syncQueryCh; got != "tenant_code=" {
		t.Errorf("查不到租户行应发 ?tenant_code=，实际 %q", got)
	}
}

// TestSubscriptionRenewCarriesTenantCode 续费下单 body 必须含本租户 code。
func TestSubscriptionRenewCarriesTenantCode(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupTenantCodeDB(t)
	stub := setupTenantCodeStub(t)

	tn := &model.Tenant{Code: "tRENEW1", Name: "续费租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}

	ctx := tenant.WithTenantID(context.Background(), tn.ID)
	res, err := NewSubscriptionMirrorService().Renew(ctx, "CLI-SUB-TC-1", "张三", "13800000000", "", "")
	if err != nil {
		t.Fatalf("Renew 失败: %v", err)
	}
	if res.OrderNo != "SUB-TENANT-CODE-1" {
		t.Fatalf("续费未走通平台下单: %+v", res)
	}
	var body struct {
		TenantCode string `json:"tenant_code"`
		OrderType  string `json:"order_type"`
	}
	raw := <-stub.orderBodyCh
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析续费下单 body: %v (%s)", err, raw)
	}
	if body.TenantCode != "tRENEW1" {
		t.Errorf("续费下单 body tenant_code 应为 tRENEW1，实际 %q", body.TenantCode)
	}
	if body.OrderType != "subscription" {
		t.Errorf("order_type 应为 subscription，实际 %q", body.OrderType)
	}
}

// TestModuleOrderCarriesTenantCode 模块下单 body 必须含本租户 code。
//
// 线上取证（2026-10-20 生产库）：同一轮端到端验证里续费单在平台侧落了
// tenant_code=tgzgc8d，模块单 MOD1792483831937A0FDEE 落库是空串——三条下单
// 路径里只有模块单漏传（subscription/llm 都带）。空串那单平台侧按租户查不到，
// 本地 module_orders 与平台 orders 对不上账，测试数据也就无法按租户清理。
func TestModuleOrderCarriesTenantCode(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupTenantCodeDB(t)
	stub := setupTenantCodeStub(t)

	tn := &model.Tenant{Code: "tMODORD", Name: "模块下单租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}

	ctx := tenant.WithTenantID(context.Background(), tn.ID)
	res, err := NewModuleCatalogService().CreateOrder(ctx, MarketOrderInput{
		ModuleKeys:    []string{"browser-automation"},
		ClientOrderNo: "CLI-MOD-TC-1",
		ContactName:   "张三",
		ContactEmail:  "zhangsan@qa.example",
	})
	if err != nil {
		t.Fatalf("模块下单失败: %v", err)
	}
	if res.OrderNo != "SUB-TENANT-CODE-1" {
		t.Fatalf("模块单未走通平台下单: %+v", res)
	}
	var body struct {
		TenantCode string   `json:"tenant_code"`
		OrderType  string   `json:"order_type"`
		ModuleKeys []string `json:"module_keys"`
	}
	raw := <-stub.orderBodyCh
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析模块下单 body: %v (%s)", err, raw)
	}
	if body.TenantCode != "tMODORD" {
		t.Errorf("模块下单 body tenant_code 应为 tMODORD，实际 %q (%s)", body.TenantCode, raw)
	}
	if body.OrderType != "module" {
		t.Errorf("order_type 应为 module，实际 %q", body.OrderType)
	}
	if len(body.ModuleKeys) != 1 || body.ModuleKeys[0] != "browser-automation" {
		t.Errorf("module_keys 应为 [browser-automation]，实际 %v", body.ModuleKeys)
	}
}

// TestModuleOrderOmitsTenantCodeWithoutRow 取不到 code（绑定的租户无行）时
// 字段整个不传（tenant_code 是 omitempty），平台按旧行为兜底，不许报错。
func TestModuleOrderOmitsTenantCodeWithoutRow(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	setupTenantCodeDB(t)
	stub := setupTenantCodeStub(t)

	ctx := tenant.WithTenantID(context.Background(), 4343)
	if _, err := NewModuleCatalogService().CreateOrder(ctx, MarketOrderInput{
		ModuleKeys:    []string{"browser-automation"},
		ClientOrderNo: "CLI-MOD-TC-2",
		ContactName:   "张三",
		ContactEmail:  "zhangsan@qa.example",
	}); err != nil {
		t.Fatalf("查不到租户行时模块下单不应报错: %v", err)
	}
	raw := <-stub.orderBodyCh
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析模块下单 body: %v (%s)", err, raw)
	}
	if v, ok := body["tenant_code"]; ok {
		t.Errorf("取不到租户 code 时不该带 tenant_code 字段，实际 %q (%s)", v, raw)
	}
}
