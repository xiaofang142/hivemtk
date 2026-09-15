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

// LoadAssetFromDB 通用 DB 加载（优先 local_assets），走装配期注入的 AssetLoaderRepository
func LoadAssetFromDB(assetType, assetID string) ([]byte, bool) {
	if assetLoaderRepo == nil {
		return nil, false
	}
	return assetLoaderRepo.LoadAssetData(context.Background(), assetType, assetID)
}

// ListAssetsFromDB 按类型列出激活资产，走装配期注入的 AssetLoaderRepository
func ListAssetsFromDB(assetType string) ([]struct {
	AssetID string
	Name    string
	Data    json.RawMessage
}, error) {
	if assetLoaderRepo == nil {
		return nil, nil
	}
	rows, err := assetLoaderRepo.ListActiveAssetsByType(context.Background(), assetType)
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
