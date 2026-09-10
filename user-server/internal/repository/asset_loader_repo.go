// asset_loader_base.go 的仓储收口：local_assets/local_asset_data 联表读取
// （五个 Loader 与 LoadAssetFromDB/ListAssetsFromDB 共用的数据访问路径，五层 L5）
package repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
)

// AssetLoaderRepository local_assets 资产读取收口（Loader 专用，只读）
type AssetLoaderRepository struct {
	db *gorm.DB
}

// NewAssetLoaderRepository 构造
func NewAssetLoaderRepository(db *gorm.DB) *AssetLoaderRepository {
	return &AssetLoaderRepository{db: db}
}

// LoadAssetData 取单个激活资产的 JSON 数据（优先 local_assets 联 local_asset_data）
func (r *AssetLoaderRepository) LoadAssetData(ctx context.Context, assetType, assetID string) ([]byte, bool) {
	if r.db == nil {
		return nil, false
	}
	var row struct {
		Data json.RawMessage
	}
	err := r.db.WithContext(ctx).Table("local_assets la").
		Joins("JOIN local_asset_data lad ON lad.local_asset_id = la.id").
		Where("la.asset_id = ? AND la.asset_type = ? AND la.is_active = ? AND la.deleted_at IS NULL", assetID, assetType, true).
		Select("lad.data").
		Scan(&row).Error
	if err != nil {
		return nil, false
	}
	if len(row.Data) == 0 {
		return nil, false
	}
	return row.Data, true
}

// AssetListRow 按类型列出的资产行
type AssetListRow struct {
	AssetID string
	Name    string
	Data    json.RawMessage
}

// ListActiveAssetsByType 按类型列出全部激活资产
func (r *AssetLoaderRepository) ListActiveAssetsByType(ctx context.Context, assetType string) ([]AssetListRow, error) {
	rows := make([]AssetListRow, 0)
	if r.db == nil {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Table("local_assets la").
		Joins("JOIN local_asset_data lad ON lad.local_asset_id = la.id").
		Where("la.asset_type = ? AND la.is_active = ? AND la.deleted_at IS NULL", assetType, true).
		Select("la.asset_id, la.name, lad.data").
		Scan(&rows).Error
	return rows, err
}
