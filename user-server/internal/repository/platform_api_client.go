package repository

import (
	"context"
	"encoding/json"
	"time"
)

type PlatformAssetPayload struct {
	AssetID     string          `json:"asset_id"`
	AssetType   string          `json:"asset_type"`
	Industry    string          `json:"industry"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	PurchaseID  int64           `json:"purchase_id"`
	SHA256      string          `json:"sha256"`
	Data        json.RawMessage `json:"data"`
	PurchasedAt time.Time       `json:"purchased_at"`
	// 文件型资产（browser_extension）的 sync 响应没有 data，只有安装包元信息。
	FileURL   string `json:"file_url"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
}

// PlatformAPIClient 平台资产市场客户端接口（实现放在 platform 包适配层）
type PlatformAPIClient interface {
	ListAssets(ctx context.Context, assetType, industry string, page, size int) ([]map[string]any, int64, error)
	GetAssetDetail(ctx context.Context, assetID string) (map[string]any, error)
	Purchase(ctx context.Context, assetID string) error
	PullData(ctx context.Context, assetID string) (*PlatformAssetPayload, error)
	MyPurchases(ctx context.Context) ([]map[string]any, error)
	ReportUsage(ctx context.Context, assetID string, delta int64) error
	// RefundPurchase 退款补偿（扣费成功但交付失败时的自动冲正，平台侧幂等）
	RefundPurchase(ctx context.Context, assetID, reason string) error
	// GetCredits 商户积分余额自查（AM-02）：GET /merchant-api/asset-market/credits
	GetCredits(ctx context.Context) (map[string]any, error)
}
