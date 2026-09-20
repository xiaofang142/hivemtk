package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/repository"
)

func newMarketTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok",
				"data": map[string]any{"token": "jwt-x", "expires": time.Now().Add(time.Hour).Unix()},
			})
		case strings.HasPrefix(r.URL.Path, "/merchant-api/asset-market/list"):
			q := r.URL.Query()
			if q.Get("page") != "2" || q.Get("size") != "10" ||
				q.Get("type") != "agent_persona" || q.Get("industry") != "美妆" {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 4000, "msg": "bad query " + q.Encode()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok",
				"data": map[string]any{"list": []map[string]any{{"asset_id": "a1"}}, "total": 7},
			})
		case strings.HasPrefix(r.URL.Path, "/merchant-api/asset-market/detail/"):
			id := strings.TrimPrefix(r.URL.Path, "/merchant-api/asset-market/detail/")
			if id != "x/1" { // PathEscape 后服务端应还原
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 4004, "msg": "id=" + id})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"name": "n"}})
		case r.URL.Path == "/merchant-api/asset-market/purchase":
			var b map[string]string
			_ = json.Unmarshal(body, &b)
			if b["asset_id"] != "a9" {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 4001, "msg": "denied"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok"})
		case r.URL.Path == "/merchant-api/asset-market/sync":
			var sb map[string]string
			_ = json.Unmarshal(body, &sb)
			if sb["asset_id"] == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 4003, "msg": "no asset"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"asset_id": "a1", "asset_type": "agent_persona", "industry": "美妆",
				"name": "话术包", "version": "1.2.0", "purchase_id": 5, "sha256": "abc",
				"data": map[string]any{"k": 1}, "purchased_at": "2026-09-01T00:00:00Z",
			}})
		case r.URL.Path == "/merchant-api/asset-market/my-purchases":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"purchase_id": 5}}})
		case r.URL.Path == "/merchant-api/asset-market/report-usage":
			var b map[string]any
			_ = json.Unmarshal(body, &b)
			if b["asset_id"] != "a1" || b["delta"] != float64(3) {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 4002, "msg": "bad delta"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "no route " + r.URL.Path})
		}
	}))
}

func withMarketConfig(t *testing.T, url string) {
	t.Helper()
	old := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = old })
	config.PlatformCfg = &config.PlatformConfig{APIURL: url, Secret: "s"}
}

func TestAssetMarketClient(t *testing.T) {
	srv := newMarketTestServer(t)
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	t.Setenv("PLATFORM_MERCHANT_KEY", "mk-test")
	withMarketConfig(t, srv.URL)

	c := NewAssetMarketClient()
	if c == nil || c.client == nil {
		t.Fatal("NewAssetMarketClient 为空")
	}
	ctx := context.Background()

	list, total, err := c.ListAssets(ctx, "agent_persona", "美妆", 2, 10)
	if err != nil || total != 7 || len(list) != 1 {
		t.Fatalf("ListAssets: %v %v %v", list, total, err)
	}
	// 空可选参数不拼进 query（走 bad-query 分支即证明参数遗漏）
	if _, _, err := c.ListAssets(ctx, "", "", 1, 5); err == nil {
		t.Fatal("缺 page=2/size=10 时服务端应报 bad query")
	}

	detail, err := c.GetAssetDetail(ctx, "x/1")
	if err != nil || detail["name"] != "n" {
		t.Fatalf("GetAssetDetail: %v %v", detail, err)
	}
	if err := c.Purchase(ctx, "a9"); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	// 错误 code 分支：asset_id=a9 时 purchase 通过，改传 aX 触发 4001
	err = c.Purchase(ctx, "aX")
	if err == nil || !strings.Contains(err.Error(), "platform error 4001") {
		t.Fatalf("Purchase 错误分支: %v", err)
	}

	payload, err := c.PullData(ctx, "a1")
	if err != nil {
		t.Fatalf("PullData: %v", err)
	}
	if payload.AssetID != "a1" || payload.SHA256 != "abc" || payload.PurchaseID != 5 ||
		string(payload.Data) != `{"k":1}` {
		t.Fatalf("PullData 解码错误: %+v", payload)
	}
	if purchases, err := c.MyPurchases(ctx); err != nil || len(purchases) != 1 {
		t.Fatalf("MyPurchases: %v %v", purchases, err)
	}
	if err := c.ReportUsage(ctx, "a1", 3); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}
	if err := c.ReportUsage(ctx, "a1", 99); err == nil {
		t.Fatal("delta 不符应报错")
	}
}

func TestAssetMarketClientAdapter(t *testing.T) {
	srv := newMarketTestServer(t)
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	t.Setenv("PLATFORM_MERCHANT_KEY", "mk-test")
	withMarketConfig(t, srv.URL)

	api := NewPlatformAPIClient()
	var _ repository.PlatformAPIClient = api

	ctx := context.Background()
	if _, total, err := api.ListAssets(ctx, "agent_persona", "美妆", 2, 10); err != nil || total != 7 {
		t.Fatalf("adapter.ListAssets: %v %v", total, err)
	}
	if d, err := api.GetAssetDetail(ctx, "x/1"); err != nil || d["name"] != "n" {
		t.Fatalf("adapter.GetAssetDetail: %v %v", d, err)
	}
	if err := api.Purchase(ctx, "a9"); err != nil {
		t.Fatalf("adapter.Purchase: %v", err)
	}
	p, err := api.PullData(ctx, "a1")
	if err != nil || p.Name != "话术包" || p.Version != "1.2.0" || p.Industry != "美妆" {
		t.Fatalf("adapter.PullData 映射: %+v %v", p, err)
	}
	if ls, err := api.MyPurchases(ctx); err != nil || len(ls) != 1 {
		t.Fatalf("adapter.MyPurchases: %v", err)
	}
	if err := api.ReportUsage(ctx, "a1", 3); err != nil {
		t.Fatalf("adapter.ReportUsage: %v", err)
	}
	// 平台业务错误时 PullData 返回 nil payload + err
	if p, err := api.PullData(ctx, ""); err == nil || p != nil {
		t.Fatalf("空 asset_id 应报错且 payload 为 nil: %v %v", p, err)
	}
}

func TestSetMerchantSecret(t *testing.T) {
	c := NewPlatformClient("k")
	c.SetMerchantSecret("")   // 空串忽略
	c.SetMerchantSecret("pp") // 非空注入
	if c.merchantSecret != "pp" {
		t.Fatalf("SetMerchantSecret 未生效: %q", c.merchantSecret)
	}
}
