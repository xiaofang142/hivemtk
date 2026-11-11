package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"
)

// AssetPayload 平台同步返回
type AssetPayload struct {
	AssetID     string          `json:"asset_id"`
	AssetType   string          `json:"asset_type"`
	Industry    string          `json:"industry"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	PurchaseID  int64           `json:"purchase_id"`
	SHA256      string          `json:"sha256"`
	Data        json.RawMessage `json:"data"`
	PurchasedAt time.Time       `json:"purchased_at"`
	// 文件型资产（browser_extension）的 sync 响应没有 data，只有安装包元信息；
	// 之前没接这三个字段，扩展落库只能拿到 nil data 撞 NOT NULL（二次回归 5000）。
	FileURL   string `json:"file_url"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
}

// AssetMarketClient 资产市场商户端客户端
type AssetMarketClient struct {
	client *Client
}

func NewAssetMarketClient() *AssetMarketClient {
	key := os.Getenv("PLATFORM_MERCHANT_KEY")
	if key == "" {
		key = GetMerchantKey()
	}
	if key == "" {
		key = "local-dev-merchant"
	}
	return &AssetMarketClient{client: NewPlatformClient(key)}
}

type assetEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *AssetMarketClient) doData(method, path string, req any, out any) error {
	var env assetEnvelope
	// 信封 code 不在这里判：商户客户端的传输层已把"HTTP 200 + code 非 200"统一转成
	// *PlatformError，此处再判一次就是一段永不命中的死分支。
	if err := c.client.Do(method, path, req, &env); err != nil {
		return err
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *AssetMarketClient) ListAssets(ctx context.Context, assetType, industry string, page, size int) ([]map[string]any, int64, error) {
	q := url.Values{}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("size", fmt.Sprintf("%d", size))
	if assetType != "" {
		q.Set("type", assetType)
	}
	if industry != "" {
		q.Set("industry", industry)
	}
	// tenant_code：purchased 标记按 (merchant_key, tenant_code) 判定
	// （平台侧购买记录每租户一行），空串照发走 '' 兜底行=旧行为。
	q.Set("tenant_code", TenantCodeFromContext(ctx))
	path := "/merchant-api/asset-market/list?" + q.Encode()
	var resp struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}
	if err := c.doData("GET", path, nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.List, resp.Total, nil
}

func (c *AssetMarketClient) GetAssetDetail(ctx context.Context, assetID string) (map[string]any, error) {
	var resp map[string]any
	detailPath := "/merchant-api/asset-market/detail/" + url.PathEscape(assetID) +
		"?tenant_code=" + url.QueryEscape(TenantCodeFromContext(ctx))
	if err := c.doData("GET", detailPath, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Purchase 购买资产。
//
// body 带 tenant_code：平台端购买记录按 (merchant_key, tenant_code) 每租户
// 一行归属。空值 omitempty 不上行 = 旧行为字节级不变（取不到租户归属时平台
// 走旧行为兜底），绝不允许因为取不到 code 就不发请求/报错。
func (c *AssetMarketClient) Purchase(ctx context.Context, assetID string) error {
	in := struct {
		AssetID    string `json:"asset_id"`
		TenantCode string `json:"tenant_code,omitempty"`
	}{
		AssetID:    assetID,
		TenantCode: TenantCodeFromContext(ctx),
	}
	return c.doData("POST", "/merchant-api/asset-market/purchase", in, nil)
}

func (c *AssetMarketClient) PullData(ctx context.Context, assetID string) (*AssetPayload, error) {
	// sync 必须带 tenant_code：平台按 (merchant_key, tenant_code) 校验本租户
	// 的购买记录后才放行数据（购买先于拉取，同租户口径）；空值 omitempty
	// 不上行 = 旧行为。
	in := struct {
		AssetID    string `json:"asset_id"`
		TenantCode string `json:"tenant_code,omitempty"`
	}{
		AssetID:    assetID,
		TenantCode: TenantCodeFromContext(ctx),
	}
	var payload AssetPayload
	if err := c.doData("POST", "/merchant-api/asset-market/sync", in, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (c *AssetMarketClient) MyPurchases(ctx context.Context) ([]map[string]any, error) {
	var list []map[string]any
	path := "/merchant-api/asset-market/my-purchases?tenant_code=" +
		url.QueryEscape(TenantCodeFromContext(ctx))
	if err := c.doData("GET", path, nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// GetCredits 商户积分余额自查（AM-02）：GET /merchant-api/asset-market/credits。
// 返回平台 MerchantCreditView（exists/balance/total_topup/total_spent/recent_logs）。
func (c *AssetMarketClient) GetCredits(ctx context.Context) (map[string]any, error) {
	var view map[string]any
	path := "/merchant-api/asset-market/credits?tenant_code=" +
		url.QueryEscape(TenantCodeFromContext(ctx)) + "&log_limit=10"
	if err := c.doData("GET", path, nil, &view); err != nil {
		return nil, err
	}
	if view == nil {
		view = map[string]any{}
	}
	return view, nil
}

// ReportUsage 上报资产使用次数（商户端 → 平台），POST /merchant-api/asset-market/report-usage
func (c *AssetMarketClient) ReportUsage(ctx context.Context, assetID string, delta int64) error {
	return c.doData("POST", "/merchant-api/asset-market/report-usage",
		map[string]any{"asset_id": assetID, "delta": delta}, nil)
}
