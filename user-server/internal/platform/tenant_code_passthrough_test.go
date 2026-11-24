package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// LLM 钱包 / 钱包流水 / 资产购买「租户归属参数」契约测试（平台端这三条链路
// 按 (merchant_key, tenant_code) 每租户一行记账）：
//  1. GET /merchant-api/llm/wallet 与 /wallet/txns 带 ?tenant_code=<本租户 code>，
//     取不到时带空串（平台旧行为兜底），绝不允许因为缺 code 就不发请求/报错；
//  2. POST /merchant-api/asset-market/purchase body 含 tenant_code；
//     空值 omitempty 不上行（旧行为字节级不变）。
//
// tenant_code 的解析走 TenantCodeFromContext（与 service.currentTenantCode 同口径），
// 所以非空用例需要一张真的 tenants 行：租户本体是全局表（无 tenant_id 列），
// off 模式直查即可。
//
// 捕获经带缓冲 channel 回传（对齐 client_test.go refusalServer 的理由）：handler
// 跑在服务器 goroutine 上，共享变量会被 -race 判竞态。

type walletTenantStub struct {
	pathQueryCh chan string // "METHOD path?query"
	purchaseCh  chan []byte
}

func newWalletTenantStub(t *testing.T) (*httptest.Server, *walletTenantStub) {
	t.Helper()
	s := &walletTenantStub{
		pathQueryCh: make(chan string, 8),
		purchaseCh:  make(chan []byte, 4),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/llm/wallet":
			select {
			case s.pathQueryCh <- r.Method + " " + r.URL.RequestURI():
			default:
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": map[string]any{
				"merchant_key": "MK-W", "exists": true,
				"input_tokens_remaining": 100, "output_tokens_remaining": 50,
			}})
		case "/merchant-api/llm/wallet/txns":
			select {
			case s.pathQueryCh <- r.Method + " " + r.URL.RequestURI():
			default:
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": map[string]any{
				"list": []map[string]any{}, "total": 0,
			}})
		case "/merchant-api/asset-market/purchase":
			body, _ := io.ReadAll(r.Body)
			select {
			case s.pathQueryCh <- r.Method + " " + r.URL.RequestURI():
			default:
			}
			select {
			case s.purchaseCh <- body:
			default:
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "no route " + r.URL.Path})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func setupTenantRowDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.Tenant{})
	// 清空挂 SystemScope：隔离 callback 在 on 模式下会 fail-closed 拒绝无绑定删除
	// （本用例默认 off 模式，但整包内别的用例可能切 on，口径对齐 module_tenant_code_test）
	t.Cleanup(func() {
		sysCtx := tenant.WithSystemScope(context.Background())
		if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).
			WithContext(sysCtx).Delete(&model.Tenant{}).Error; err != nil {
			t.Logf("清理 tenants 失败: %v", err)
		}
	})
	return database
}

// TestWalletAndTxnsCarryTenantCodeQuery 钱包与流水查询必须带本租户 code。
func TestWalletAndTxnsCarryTenantCodeQuery(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	database := setupTenantRowDB(t)
	srv, stub := newWalletTenantStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "r3t"})

	tn := &model.Tenant{Code: "tWALLET1", Name: "钱包租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	ctx := tenant.WithTenantID(context.Background(), tn.ID)
	c := NewModuleClient()

	if _, err := c.Wallet(ctx); err != nil {
		t.Fatalf("Wallet: %v", err)
	}
	if got := <-stub.pathQueryCh; got != "GET /merchant-api/llm/wallet?tenant_code=tWALLET1" {
		t.Errorf("wallet 请求应为 ?tenant_code=tWALLET1，实际 %q", got)
	}

	if _, _, err := c.WalletTxns(ctx, 1, 20); err != nil {
		t.Fatalf("WalletTxns: %v", err)
	}
	raw := <-stub.pathQueryCh
	u, err := requestURIQuery(raw)
	if err != nil {
		t.Fatalf("解析流水请求 %q: %v", raw, err)
	}
	if u.Get("tenant_code") != "tWALLET1" {
		t.Errorf("txns query tenant_code 应为 tWALLET1，实际 %q", u.Get("tenant_code"))
	}
	if u.Get("page") != "1" || u.Get("page_size") != "20" {
		t.Errorf("既有分页参数被改动: %v", u)
	}
}

// TestWalletTxnsEmptyTenantCodeStillCalls 无租户绑定（后台/裸 Background）→
// 空串照发 ?tenant_code=，平台旧行为兜底，不许报错。
func TestWalletTxnsEmptyTenantCodeStillCalls(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	setupTenantRowDB(t)
	srv, stub := newWalletTenantStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "r3t"})

	c := NewModuleClient()
	if _, err := c.Wallet(context.Background()); err != nil {
		t.Fatalf("空 tenant_code 的 Wallet 不应报错: %v", err)
	}
	if got := <-stub.pathQueryCh; got != "GET /merchant-api/llm/wallet?tenant_code=" {
		t.Errorf("空 tenant_code 应照发 ?tenant_code=，实际 %q", got)
	}
	if _, _, err := c.WalletTxns(context.Background(), 1, 20); err != nil {
		t.Fatalf("空 tenant_code 的 WalletTxns 不应报错: %v", err)
	}
	raw := <-stub.pathQueryCh
	u, err := requestURIQuery(raw)
	if err != nil {
		t.Fatalf("解析流水请求 %q: %v", raw, err)
	}
	if _, exists := u["tenant_code"]; !exists {
		t.Errorf("空 tenant_code 也应上行 tenant_code= 键（与 sync 同款），实际 %v", u)
	}
}

// TestPurchaseCarriesTenantCodeBody 购买 body 必须含本租户 code。
func TestPurchaseCarriesTenantCodeBody(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	t.Setenv("MERCHANT_API_SECRET", "s")
	database := setupTenantRowDB(t)
	srv, stub := newWalletTenantStub(t)
	withMarketConfig(t, srv.URL)

	tn := &model.Tenant{Code: "tBUY001", Name: "购买租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	ctx := tenant.WithTenantID(context.Background(), tn.ID)

	if err := NewAssetMarketClient().Purchase(ctx, "a9"); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(<-stub.purchaseCh, &body); err != nil {
		t.Fatalf("解析购买 body: %v", err)
	}
	if body["asset_id"] != "a9" || body["tenant_code"] != "tBUY001" {
		t.Errorf("购买 body 应含 asset_id=a9 与 tenant_code=tBUY001，实际 %v", body)
	}

	// 无租户归属 → 空值 omitempty 不上行（旧行为字节级不变），请求照发
	if err := NewAssetMarketClient().Purchase(context.Background(), "a9"); err != nil {
		t.Fatalf("空 tenant_code 的 Purchase 不应报错: %v", err)
	}
	raw := <-stub.purchaseCh
	var body2 map[string]any
	if err := json.Unmarshal(raw, &body2); err != nil {
		t.Fatalf("解析购买 body: %v", err)
	}
	if _, exists := body2["tenant_code"]; exists {
		t.Errorf("空 tenant_code 应被 omitempty 省略，实际 body=%s", raw)
	}
}

// requestURIQuery 从 "GET /path?a=1&b=2" 里剥出 query（避免整串 == 断言脆在参数顺序上）
func requestURIQuery(raw string) (url.Values, error) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == ' ' {
			u, err := url.Parse(raw[i+1:])
			if err != nil {
				return nil, err
			}
			return u.Query(), nil
		}
	}
	return url.Values{}, nil
}
