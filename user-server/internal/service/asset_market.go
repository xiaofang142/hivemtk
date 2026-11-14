package service

import (
	"context"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/repository"
)

// AssetMarketService 资产市场代理服务
type AssetMarketService struct {
	platformClient repository.PlatformAPIClient
}

func NewAssetMarketService(pc repository.PlatformAPIClient) *AssetMarketService {
	return &AssetMarketService{platformClient: pc}
}

func (s *AssetMarketService) ListMarketAssets(ctx context.Context, assetType, industry string, page, size int) ([]map[string]any, int64, error) {
	list, total, err := s.platformClient.ListAssets(ctx, assetType, industry, page, size)
	if err != nil {
		return nil, 0, bizerr.Wrap(bizerr.CodePlatformUnavail, "获取市场列表失败", err)
	}
	return list, total, nil
}

func (s *AssetMarketService) GetMarketAssetDetail(ctx context.Context, assetID string) (map[string]any, error) {
	detail, err := s.platformClient.GetAssetDetail(ctx, assetID)
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodePlatformUnavail, "获取资产详情失败", err)
	}
	return detail, nil
}

// GetCredits 商户积分余额自查（AM-02）：透传平台 MerchantCreditView。
// 平台不可达时报平台不可用业务码（读面失败要可见——余额看不见比报错更误事，
// 与资产列表读面「空列表」语义不同：余额是资金事实，空值会误导购买决策）。
func (s *AssetMarketService) GetCredits(ctx context.Context) (map[string]any, error) {
	view, err := s.platformClient.GetCredits(ctx)
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodePlatformUnavail, "获取积分余额失败", err)
	}
	if view == nil {
		view = map[string]any{}
	}
	return view, nil
}

func (s *AssetMarketService) MyPurchases(ctx context.Context) ([]map[string]any, error) {
	list, err := s.platformClient.MyPurchases(ctx)
	if err != nil {
		// 审计二轮：此前把失败吞成空数组——用户会误以为「没有购买记录」，
		// 而真实原因可能是平台不可达。错误必须透传（关态由 controller 分流为空态）。
		return nil, bizerr.Wrap(bizerr.CodePlatformUnavail, "获取购买记录失败", err)
	}
	if list == nil {
		list = []map[string]any{}
	}
	return list, nil
}
