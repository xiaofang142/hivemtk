package app

import (
	"context"
	"sync"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

type memoryProviderAdapter struct{ ms *service.MemorySystem }

func (a *memoryProviderAdapter) LoadEpisodicMemory(ctx context.Context, sessionID, customerID string) (string, error) {
	if a == nil || a.ms == nil {
		return "", nil
	}
	return a.ms.BuildFullContext(ctx, sessionID, customerID)
}

var (
	globalInferenceOrchestrator *agent_runtime.CoreDataFlowOrchestrator
	inferenceOrchestratorOnce   sync.Once
	globalPermissionChecker     *tooluse.WhitelistPermissionChecker
	globalPermissionCheckerOnce sync.Once
)

// InitInferenceOrchestrator 在工具执行器、记忆和检查点存储完成装配后初始化运行时。
func InitInferenceOrchestrator() {
	inferenceOrchestratorOnce.Do(func() {
		cycle := agent_runtime.NewInferenceCycle()
		if p, ok := cycle.PlannerStage.(*agent_runtime.DefaultTaskPlanner); ok {
			if chainSvc := GetIntentToolChainService(); chainSvc != nil {
				p.SetChainLookup(chainSvc)
				logger.Info("[inference] ✅ IntentChainLookup 已注入（TB-01 数据驱动意图→工具链）")
			} else {
				logger.Warn("[inference] ⚠️ 意图→工具链绑定层未装配，规划器走硬编码路径")
			}
		}
		if ms := service.GetMemorySystem(); ms != nil {
			cycle.SetMemoryProvider(&memoryProviderAdapter{ms: ms})
			logger.Info("[inference] ✅ EpisodicMemoryProvider 已注入（包装 MemorySystem.BuildFullContext）")
		} else {
			logger.Warn("[inference] ⚠️ MemorySystem 未初始化，推理闭环将跳过情境记忆读取")
		}
		if store := GetAgentCheckpointStore(); store != nil {
			cycle.SetCheckpointStore(store)
			logger.Info("[inference] ✅ StageCheckpointStore 已注入（T-P1-01，实际是否落点取决于 FF_LTC_CHECKPOINT）")
		}
		globalInferenceOrchestrator = agent_runtime.NewCoreDataFlowOrchestrator(cycle, nil)
		globalInferenceOrchestrator.SetDeliveryGateway(GetInferenceDeliveryService())
		logger.Info("[inference] ✅ CoreDataFlowOrchestrator 已装配（五阶段推理及持久交付账本，默认仅规划）")
	})
}

// GetInferenceOrchestrator 未装配时返回 nil，由调用入口报告不可用。
func GetInferenceOrchestrator() *agent_runtime.CoreDataFlowOrchestrator {
	return globalInferenceOrchestrator
}

// SetGlobalPermissionChecker 由工具执行器装配时注入同一权限检查器。
func SetGlobalPermissionChecker(pc *tooluse.WhitelistPermissionChecker) {
	globalPermissionChecker = pc
}

// GetGlobalPermissionChecker 保留原有惰性初始化和默认放行策略。
func GetGlobalPermissionChecker() *tooluse.WhitelistPermissionChecker {
	globalPermissionCheckerOnce.Do(func() {
		if globalPermissionChecker == nil {
			globalPermissionChecker = tooluse.NewWhitelistPermissionChecker()
		}
	})
	return globalPermissionChecker
}
