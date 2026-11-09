// intent_chain_wiring.go TB-01：意图→工具链绑定层装配。
//
// 调用方：cmd/api/main.go（router.Setup 之前，沿 InitContentGuardRuntime 同款
// 「必须先于 router.Setup」次序——router.Setup 内的 InitInferenceOrchestrator
// 要把它注入规划器，setupIntentRoutes 的 HTTP 面要拿同一个实例）。
//
// 幂等：sync.Once；重复调用返回同一实例（main 与 router 双入口安全）。
// db=nil 不装配（返回 nil），消费方 nil-safe 降级：规划器回落硬编码、HTTP 面 503。
//
// 五层归属：装配层（旗标/依赖拼装）；业务判据在 service/intent_tool_chain.go。
package app

import (
	"context"
	"sync"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

var (
	intentChainOnce sync.Once
	intentChainSvc  *service.IntentToolChainService
)

// InitIntentChainRuntime 装配意图→工具链绑定服务（幂等；db=nil 不装配）。
func InitIntentChainRuntime(gdb *gorm.DB) *service.IntentToolChainService {
	intentChainOnce.Do(func() {
		if gdb == nil {
			logger.Warn("[intent_chain] ⚠️ db 未装配，意图→工具链绑定层不启用（规划器回落硬编码）")
			return
		}
		intentChainSvc = service.NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(gdb))
		// TB-03 默认种子：只读查询链（快递/售后）幂等写入默认租户；已有配置（含禁用）跳过。
		seedCtx := tenant.ContextWithDefault(context.Background())
		if n, err := intentChainSvc.SeedDefaultIntentChains(seedCtx); err != nil {
			logger.Warnf("[intent_chain] ⚠️ 默认工具调用流种子失败（不阻塞启动）: %v", err)
		} else if n > 0 {
			logger.Infof("[intent_chain] ✅ 默认工具调用流种子已写入 %d 条（TB-03 只读链）", n)
		}
		logger.Info("[intent_chain] ✅ 意图→工具链绑定服务已装配（TB-01）")
	})
	return intentChainSvc
}

// GetIntentToolChainService 取绑定服务实例（未装配返回 nil）。
func GetIntentToolChainService() *service.IntentToolChainService {
	return intentChainSvc
}
