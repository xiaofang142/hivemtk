package platform

import (
	"context"

	"hivemtk-user/internal/repository"
)

// disabledClient 平台集成关闭态（PLATFORM_ENABLED 未开启）下的
// repository.PlatformAPIClient 实现。
//
// 为什么要有它，而不是直接用真实 client：真实 client 在关态确实也零网络出站
// （client.go 每条路径先判 config.PlatformCfg == nil 就返回 ErrPlatformNotConfigured），
// 但它把"没启用"表达成 error。读面透传给前端就成了业务码 5001 的红叉，
// 而关态的资产市场语义是"这里没有货"，不是"出错了"。
//
// 因此两面的形状刻意不同，且都是契约而非巧合：
//   - 读 → 非 nil 空值 + nil error（前端直接 .length / 取字段，不给 nil 的机会）
//   - 写 → 既有哨兵 ErrPlatformNotConfigured（绝不静默成功，
//     否则 UI 会提示"购买成功"而本地什么都没有）
type disabledClient struct{}

func (disabledClient) ListAssets(_ context.Context, _, _ string, _, _ int) ([]map[string]any, int64, error) {
	return []map[string]any{}, 0, nil
}

func (disabledClient) GetAssetDetail(_ context.Context, _ string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (disabledClient) MyPurchases(_ context.Context) ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (disabledClient) Purchase(_ context.Context, _ string) error {
	return ErrPlatformNotConfigured
}

func (disabledClient) PullData(_ context.Context, _ string) (*repository.PlatformAssetPayload, error) {
	return nil, ErrPlatformNotConfigured
}

func (disabledClient) ReportUsage(_ context.Context, _ string, _ int64) error {
	return ErrPlatformNotConfigured
}

var _ repository.PlatformAPIClient = disabledClient{}
