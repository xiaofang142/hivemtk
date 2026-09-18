package app

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// initGlobalToolExecutor 初始化全局 ToolExecutor（含真实装饰器链）
//
// 调用方：router.Setup() （必须最先调用，先于所有 register 函数和 buildSalesEngine）
//
// 装饰器配置：
//   - PermissionChecker: 全局 WhitelistPermissionChecker（defaultAllow=true，向后兼容；
//     按 AgentContext.Tools 在 runAgentLoop 中设置执行期白名单，与注入期过滤形成双层防护）
//   - RateLimiter:        TokenBucket 20 QPS / 突发 50（防 LLM 误用导致工具风暴）
//   - RetryPolicy:        指数退避 3 次 / 基础 200ms / 上限 5s（含抖动）
//   - Timeout:            30s（单次工具执行上限）
//   - AuditLogger:        内存版（保留最近 10000 条审计）
//   - CostTracker:        内存版（运营面板可读取统计）
//   - CircuitBreaker:     按 FF_TOOL_CIRCUIT_BREAKER 三态挂载（默认 off = 不接，见 tool_circuit_breaker_wiring.go）
//
// 优化：本地持有 memAuditLogger / memCostTracker 引用，
// 通过 GetGlobalMemoryAuditLogger / GetGlobalMemoryCostTracker 暴露给调试 API（/agent/tools/audit /cost）。
// 未来切换为 DB 持久化版本时，仅需替换此处构造与暴露函数的实现。
func InitGlobalToolExecutor() {
	memAuditLogger = tooluse.NewMemoryAuditLogger(10000)
	memCostTracker = tooluse.NewMemoryCostTracker()
	config := tooluse.ToolExecutorConfig{
		DefaultTimeout:    30 * time.Second,
		PermissionChecker: GetGlobalPermissionChecker(),
		RateLimiter:       tooluse.NewTokenBucketLimiter(20, 50),
		RetryPolicy:       tooluse.NewExponentialBackoffPolicy(3, 200*time.Millisecond, 5*time.Second),
		AuditLogger:       memAuditLogger,
		CostTracker:       memCostTracker,

		FeedbackSink: NewFeedbackCollectorAdapter(service.GetFeedbackCollector()),
	}
	circuitMode := applyToolCircuitBreaker(&config)
	exec := tooluse.NewToolExecutor(tooluse.GetGlobalRegistry(), config)
	tooluse.SetGlobalExecutor(exec)
	logger.Infof("[agent] ✅ 全局 ToolExecutor 已初始化（装饰器链：权限/限流/重试/超时/审计/计费 全部启用；熔断=%s）", circuitMode)
}

var (
	memAuditLogger *tooluse.MemoryAuditLogger
	memCostTracker *tooluse.MemoryCostTracker
)

// GetGlobalMemoryAuditLogger 返回全局内存审计日志器
//
// 调用方：tool_debug_routes.go handleToolAudit
// 未初始化时返回 nil（调用方需 nil-check）
func GetGlobalMemoryAuditLogger() *tooluse.MemoryAuditLogger {
	return memAuditLogger
}

// GetGlobalMemoryCostTracker 返回全局内存计费跟踪器
//
// 调用方：tool_debug_routes.go handleToolCost
// 未初始化时返回 nil（调用方需 nil-check）
func GetGlobalMemoryCostTracker() *tooluse.MemoryCostTracker {
	return memCostTracker
}

var (
	globalToolRouter     *tooluse.ToolRouter
	globalToolRouterOnce sync.Once
)

// initGlobalToolRouter 初始化全局 ToolRouter
//
// 调用方：router.Setup() 中，在 InitGlobalToolExecutor + RegisterAllAgentTools 之后调用
//
// 装配内容：
//   - 复用全局 ToolExecutor（不重复创建）
//   - RateLimiter：**独立实例**，不与 Executor 的那个共享。两者是串联的两道独立准入：
//     Executor 装饰器键 = CallerID:toolName（按调用方配额），
//     ToolRouter 键 = toolName:AgentID（按智能体配额，见 defaultKeyBuilder）。
//     串联不叠加吞吐（整体上限取两道中的较小者），但各自独立计量；
//     若共享同一实例，在 CallerID 与 AgentID 同时为空的退化场景下
//     两道会退化成同一个 bare toolName 桶而互相争用配额。
//   - 配置：FailThreshold=5 / CooldownDuration=30s / DefaultToolCost=0.001
func InitGlobalToolRouter() {
	globalToolRouterOnce.Do(func() {
		exec := tooluse.GetGlobalExecutor()
		if exec == nil {
			logger.Warn("[agent] ⚠️ ToolRouter 跳过初始化：全局 ToolExecutor 未就绪（请检查 InitGlobalToolExecutor 调用顺序）")
			return
		}
		globalToolRouter = tooluse.NewToolRouter(
			exec,
			tooluse.NewTokenBucketLimiter(20, 50),
			tooluse.RouterConfig{
				FailThreshold:    5,
				CooldownDuration: 30 * time.Second,
				DefaultToolCost:  0.001,
			},
		)
		logger.Info("[agent] ✅ 全局 ToolRouter 已初始化（熔断阈值 5 / 冷却 30s / 默认成本 0.001）")
	})
}

// GetGlobalToolRouter 返回全局 ToolRouter
//
// 未初始化时返回 nil（调用方需 nil-check）
func GetGlobalToolRouter() *tooluse.ToolRouter {
	return globalToolRouter
}

// SetGlobalToolRouterForTest 仅用于测试：临时替换/清空全局 ToolRouter
func SetGlobalToolRouterForTest(r *tooluse.ToolRouter) { globalToolRouter = r }

// registerAllAgentTools 一次性注册全部智能体工具到全局注册中心
//
// 调用方：router.Setup()（在 initGlobalToolExecutor 之后、buildSalesEngine 之前）
//
// 使用 Provider 模式装配（见 tool_provider_wiring.go），
// 支持第三方包通过 tooluse.RegisterToolProvider 自注册扩展。
//
// 注册顺序：reach → pm → customer → knowledge → business → 第三方
//
// 工具数量不在注释里写死（历史上曾记为 40，实际已增至 42 而无人更正）。
// 需要准确数字时用 `tooluse.GetGlobalRegistry().List()` 在运行时取。
func RegisterAllAgentTools(gormDB *gorm.DB) {
	registerAllAgentToolsViaProviders(gormDB)
}
