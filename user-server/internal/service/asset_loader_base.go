package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// assetLoaderRepo 五个 Loader 共用的资产读取仓储（构造期注入，L4 不直连 DB）
var assetLoaderRepo *repository.AssetLoaderRepository

// BindAssetLoaderRepository 装配期注入资产读取仓储（main/router 调用一次）
func BindAssetLoaderRepository(db *gorm.DB) {
	assetLoaderRepo = repository.NewAssetLoaderRepository(db)
}

// LoadAssetFromDB 通用 DB 加载（优先 local_assets）
func LoadAssetFromDB(db *gorm.DB, assetType, assetID string) ([]byte, bool) {
	repo := repository.NewAssetLoaderRepository(db)
	return repo.LoadAssetData(context.Background(), assetType, assetID)
}

// ListAssetsFromDB 按类型列出激活资产
func ListAssetsFromDB(db *gorm.DB, assetType string) ([]struct {
	AssetID string
	Name    string
	Data    json.RawMessage
}, error) {
	repo := repository.NewAssetLoaderRepository(db)
	rows, err := repo.ListActiveAssetsByType(context.Background(), assetType)
	out := make([]struct {
		AssetID string
		Name    string
		Data    json.RawMessage
	}, len(rows))
	for i, r := range rows {
		out[i] = struct {
			AssetID string
			Name    string
			Data    json.RawMessage
		}{AssetID: r.AssetID, Name: r.Name, Data: r.Data}
	}
	if err != nil {
		slog.Warn("ListAssetsFromDB error", "asset_type", assetType, "error", err.Error())
	}
	return out, err
}
