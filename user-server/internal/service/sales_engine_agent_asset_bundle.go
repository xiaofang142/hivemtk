package service

import (
	"context"
	"sync"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
)

// AssetBundleResolver 资产包解析器接口
//
// 实现方：由 service 层提供具体实现（如 AssetBundleService.ResolveSystemPrompt）
// 注入点：main.go 装配时调用 SetAssetBundleResolver
type AssetBundleResolver interface {
	ResolveSystemPrompt(ctx context.Context, assetBundleID string) (string, error)
}

var (
	assetBundleResolverForSalesEngine AssetBundleResolver
	assetBundleResolverMu             sync.RWMutex
)

// SetAssetBundleResolver 注入全局资产包解析器
// 调用方：main.go 在装配 AssetBundleService 后调用
// 线程安全：使用 RWMutex 保护，支持运行期热更新
func SetAssetBundleResolver(r AssetBundleResolver) {
	assetBundleResolverMu.Lock()
	defer assetBundleResolverMu.Unlock()
	assetBundleResolverForSalesEngine = r
}

// GetAssetBundleResolver 获取全局资产包解析器（未注入返回 nil）
func GetAssetBundleResolver() AssetBundleResolver {
	assetBundleResolverMu.RLock()
	defer assetBundleResolverMu.RUnlock()
	return assetBundleResolverForSalesEngine
}

// assetBundleBindValidator 绑定校验钩子：校验 asset_id 存在且可用。
// 实现方 AssetBundleService.ValidateBundleForBind；装配点 router 层。
var (
	assetBundleBindValidator   func(ctx context.Context, assetID string) error
	assetBundleBindValidatorMu sync.RWMutex
)

// SetAssetBundleBindValidator 注入绑定校验器
func SetAssetBundleBindValidator(f func(ctx context.Context, assetID string) error) {
	assetBundleBindValidatorMu.Lock()
	defer assetBundleBindValidatorMu.Unlock()
	assetBundleBindValidator = f
}

// ValidateAssetBundleForBind 智能体写入 asset_bundle_id 前的统一校验入口。
// 校验器未注入（如部分测试场景）时放行——与解析器未注入时 Persona 直通同一口径。
func ValidateAssetBundleForBind(ctx context.Context, assetID string) error {
	if assetID == "" {
		return nil
	}
	assetBundleBindValidatorMu.RLock()
	f := assetBundleBindValidator
	assetBundleBindValidatorMu.RUnlock()
	if f == nil {
		return nil
	}
	return f(ctx, assetID)
}

func resolveAssetBundlePersona(ctx context.Context, agentCtx *dto.AgentContext, resolver AssetBundleResolver) string {
	if agentCtx == nil || agentCtx.AssetBundleID == "" {
		return ""
	}
	if resolver == nil {
		return ""
	}
	prompt, err := resolver.ResolveSystemPrompt(ctx, agentCtx.AssetBundleID)
	if err != nil {
		logger.Warnf("[sales_engine] resolve asset bundle persona failed: asset=%s err=%v (fallback to original persona)",
			agentCtx.AssetBundleID, err)
		return ""
	}
	return prompt
}
