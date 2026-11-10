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
