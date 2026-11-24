package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/config"
)

// 模块客户端「租户归属参数」契约测试（平台端订阅按 (merchant_key, tenant_code)
// 每租户一行下发）：
//  1. sync 请求 URL 必须带 ?tenant_code=<code>，取不到时带空串（平台旧行为兜底，
//     绝不允许因为缺 code 就不发请求/报错）；
//  2. CreateOrder 入参带 tenant_code（订阅类订单必填链路），空值 omitempty 不上行。
//
// 捕获经带缓冲 channel 回传（对齐 refusalServer 的理由）：handler 跑在服务器
// goroutine 上，共享变量会被 -race 判竞态。

// tenantCodeStub 捕获请求 query/body 的桩平台
type tenantCodeStub struct {
	queryCh chan string
	bodyCh  chan []byte
}

func newTenantCodeStub(t *testing.T, path string) (*httptest.Server, *tenantCodeStub) {
	t.Helper()
	stub := &tenantCodeStub{queryCh: make(chan string, 4), bodyCh: make(chan []byte, 4)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == path {
			body, _ := io.ReadAll(r.Body)
			select {
			case stub.queryCh <- r.URL.RawQuery:
			default:
			}
			select {
			case stub.bodyCh <- body:
			default:
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, stub
}

// TestSyncModulesSendsTenantCodeQuery sync 请求必须带 ?tenant_code=。
func TestSyncModulesSendsTenantCodeQuery(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	srv, stub := newTenantCodeStub(t, "/merchant-api/modules/sync")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "r3t"})

	if _, err := NewModuleClient().SyncModules(context.Background(), "tABC123"); err != nil {
		t.Fatalf("SyncModules: %v", err)
	}
	if got := <-stub.queryCh; got != "tenant_code=tABC123" {
		t.Errorf("sync query 应为 tenant_code=tABC123，实际 %q", got)
	}
}

// TestSyncModulesEmptyTenantCodeStillCalls 取不到租户 code 时传空串照发请求
// （平台对空值走旧行为兜底），不许报错。
func TestSyncModulesEmptyTenantCodeStillCalls(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	srv, stub := newTenantCodeStub(t, "/merchant-api/modules/sync")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "r3t"})

	if _, err := NewModuleClient().SyncModules(context.Background(), ""); err != nil {
		t.Fatalf("空 tenant_code 的 SyncModules 不应报错: %v", err)
	}
	if got := <-stub.queryCh; got != "tenant_code=" {
		t.Errorf("空 tenant_code 应照发 ?tenant_code=，实际 %q", got)
	}
}

// TestCreateOrderCarriesTenantCode 下单 body 含 tenant_code；空值 omitempty 不上行。
func TestCreateOrderCarriesTenantCode(t *testing.T) {
	t.Setenv("PLATFORM_MERCHANT_KEY", "MK-TC")
	srv, stub := newTenantCodeStub(t, "/merchant-api/orders")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "r3t"})
	c := NewModuleClient()

	if _, err := c.CreateOrder(context.Background(), CreateOrderInput{
		ClientOrderNo: "CLI-1", OrderType: "subscription", TenantCode: "tABC123",
	}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(<-stub.bodyCh, &body); err != nil {
		t.Fatalf("解析下单 body: %v", err)
	}
	if body["tenant_code"] != "tABC123" {
		t.Errorf("下单 body tenant_code 应为 tABC123，实际 %v", body["tenant_code"])
	}

	// 空值：omitempty 不上行（模块/LLM 单走旧行为）
	if _, err := c.CreateOrder(context.Background(), CreateOrderInput{
		ClientOrderNo: "CLI-2", OrderType: "module",
	}); err != nil {
		t.Fatalf("CreateOrder(无 tenant_code): %v", err)
	}
	raw := <-stub.bodyCh
	var body2 map[string]any
	if err := json.Unmarshal(raw, &body2); err != nil {
		t.Fatalf("解析下单 body: %v", err)
	}
	if _, exists := body2["tenant_code"]; exists {
		t.Errorf("空 tenant_code 应被 omitempty 省略，实际 body=%s", raw)
	}
}
