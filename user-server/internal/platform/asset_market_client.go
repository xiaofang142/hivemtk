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
	// *PlatformError（R11），此处再判一次就是一段永不命中的死分支。
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
	detailPath := "/merchant-api/asset-market/detail/" + url.PathEscape(assetID)
	if err := c.doData("GET", detailPath, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *AssetMarketClient) Purchase(ctx context.Context, assetID string) error {
	return c.doData("POST", "/merchant-api/asset-market/purchase", map[string]string{"asset_id": assetID}, nil)
}

func (c *AssetMarketClient) PullData(ctx context.Context, assetID string) (*AssetPayload, error) {
	var payload AssetPayload
	if err := c.doData("POST", "/merchant-api/asset-market/sync", map[string]string{"asset_id": assetID}, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (c *AssetMarketClient) MyPurchases(ctx context.Context) ([]map[string]any, error) {
	var list []map[string]any
	if err := c.doData("GET", "/merchant-api/asset-market/my-purchases", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ReportUsage 上报资产使用次数（商户端 → 平台），POST /merchant-api/asset-market/report-usage
func (c *AssetMarketClient) ReportUsage(ctx context.Context, assetID string, delta int64) error {
	return c.doData("POST", "/merchant-api/asset-market/report-usage",
		map[string]any{"asset_id": assetID, "delta": delta}, nil)
}
