package service

// 兼容层：SOP 域已迁入 internal/service/sop（按域拆包第四刀）。
//
// 根包内调用点通过别名/薄包装保持不变；sop 不反向 import 根包，
// 其外依赖走 sop/deps.go 的注入位，本文件在 init 期装配。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service/sop"
)

type SOPService = sop.SOPService
type SOPTemplateService = sop.SOPTemplateService
type IndustrySOP = sop.IndustrySOP
type SOPLoader = sop.SOPLoader
type NodeExecResult = sop.NodeExecResult
type ExecutionContext = sop.ExecutionContext
type SopHeatmapService = sop.SopHeatmapService
type SOPABTestConfig = sop.SOPABTestConfig
type CreateRequest = sop.CreateRequest
type ApprovalResumeBridge = sop.ApprovalResumeBridge
type SOPNodeExecutorDeps = sop.SOPNodeExecutorDeps
type SopHeatmapReport = sop.SopHeatmapReport
type WaitExecutor = sop.WaitExecutor
type SOPExecutionDispatcher = sop.SOPExecutionDispatcher
type SOPScheduler = sop.SOPScheduler
type CompensationManager = sop.CompensationManager
type SOPOutboxDispatcher = sop.SOPOutboxDispatcher
type SOPStuckDetector = sop.SOPStuckDetector
type NodeExecutor = sop.NodeExecutor
type ReachSendExecutor = sop.ReachSendExecutor
type CompensationNoter = sop.CompensationNoter
type ActiveReachSender = sop.ActiveReachSender

const (
	NodeStatusCompleted = sop.NodeStatusCompleted
	NodeStatusWaiting   = sop.NodeStatusWaiting
	NodeStatusFailed    = sop.NodeStatusFailed
	NodeStatusSkipped   = sop.NodeStatusSkipped

	WaitEventTimer    = sop.WaitEventTimer
	WaitEventApproval = sop.WaitEventApproval

	SOPStatusRunning = sop.SOPStatusRunning
	SOPStatusSuccess = sop.SOPStatusSuccess
	SOPTriggerIntent = sop.SOPTriggerIntent
	SOPTriggerManual = sop.SOPTriggerManual

	SOPNodeTypeStart     = sop.SOPNodeTypeStart
	SOPNodeTypeEnd       = sop.SOPNodeTypeEnd
	SOPNodeTypeReachSend = sop.SOPNodeTypeReachSend

	ApprovalSubjectTypeSOPNode  = sop.ApprovalSubjectTypeSOPNode
	ApprovalOutcomeStatusKey    = sop.ApprovalOutcomeStatusKey
	ApprovalOutcomeIDKey        = sop.ApprovalOutcomeIDKey
	ApprovalOutcomeTokenKey     = sop.ApprovalOutcomeTokenKey
	ApprovalOutcomeAllowedKey   = sop.ApprovalOutcomeAllowedKey
	DefaultMaxExecutedNodeTrace = sop.DefaultMaxExecutedNodeTrace
	DefaultMaxRunningPerSOP     = sop.DefaultMaxRunningPerSOP

	sopTopK = sop.DefaultSOPTopK
)

const (
	ParamGroupSOP = sop.ParamGroupSOP

	DefaultSOPDispatcherWorkerCount    = sop.DefaultSOPDispatcherWorkerCount
	DefaultSOPDispatcherQueueCapacity  = sop.DefaultSOPDispatcherQueueCapacity
	DefaultSOPDispatcherLLMConcurrency = sop.DefaultSOPDispatcherLLMConcurrency

	SOPDispatcherWorkerCountParam    = sop.SOPDispatcherWorkerCountParam
	SOPDispatcherQueueCapacityParam  = sop.SOPDispatcherQueueCapacityParam
	SOPDispatcherLLMConcurrencyParam = sop.SOPDispatcherLLMConcurrencyParam

	SOPCompensationMaxAttemptsParam  = sop.SOPCompensationMaxAttemptsParam
	SOPCompensationTTLParam          = sop.SOPCompensationTTLParam
	SOPCompensationMaxPlansKeptParam = sop.SOPCompensationMaxPlansKeptParam

	DefaultCompensationMaxAttempts  = sop.DefaultCompensationMaxAttempts
	DefaultCompensationPerTTL       = sop.DefaultCompensationPerTTL
	DefaultCompensationMaxPlansKept = sop.DefaultCompensationMaxPlansKept
)

var (
	DefaultSOPDispatcherConfig    = sop.DefaultSOPDispatcherConfig
	DefaultCompensationConfig     = sop.DefaultCompensationConfig
	SOPDispatcherConfigFromParams = sop.SOPDispatcherConfigFromParams

	ErrSOPExecNotFound    = sop.ErrSOPExecNotFound
	SOPNodeSupportedTypes = sop.SOPNodeSupportedTypes
)

func NewSOPLoader() *SOPLoader { return sop.NewSOPLoader() }

func NewSOPService(dispatcher *llm.Dispatcher) *SOPService { return sop.NewSOPService(dispatcher) }

func GetSOPService() *SOPService { return sop.GetSOPService() }

func NewSOPTemplateService() *SOPTemplateService { return sop.NewSOPTemplateService() }

func NewSOPTemplateServiceDefault() *SOPTemplateService { return sop.NewSOPTemplateServiceDefault() }

func NewSopHeatmapService(agentRepo sop.SopAgentGetter, execRepo sop.SopExecLister) *SopHeatmapService {
	return sop.NewSopHeatmapService(agentRepo, execRepo)
}

func NewWaitExecutor(deps *SOPNodeExecutorDeps) *WaitExecutor { return sop.NewWaitExecutor(deps) }

func NewReachSendExecutor() *ReachSendExecutor { return sop.NewReachSendExecutor() }

func NewNodeExecutorRegistry() *sop.NodeExecutorRegistry { return sop.NewNodeExecutorRegistry() }

func RegisterAllNodeExecutors(reg *sop.NodeExecutorRegistry, deps *SOPNodeExecutorDeps) {
	sop.RegisterAllNodeExecutors(reg, deps)
}

func NewSOPExecutionDispatcher(sopSvc *SOPService, reg *sop.NodeExecutorRegistry, cfg *sop.SOPDispatcherConfig) *SOPExecutionDispatcher {
	return sop.NewSOPExecutionDispatcher(sopSvc, reg, cfg)
}

func NewSOPOutboxDispatcher(execDispatcher sop.SOPDispatchSender) *SOPOutboxDispatcher {
	return sop.NewSOPOutboxDispatcher(execDispatcher)
}

func InitSOPCompensation(d *SOPExecutionDispatcher) *CompensationManager {
	return sop.InitSOPCompensation(d)
}

func InitSOPExecutionDispatcher(sopSvc *SOPService, cfg *sop.SOPDispatcherConfig) *SOPExecutionDispatcher {
	return sop.InitSOPExecutionDispatcher(sopSvc, cfg)
}

func InitSOPOutboxDispatcher(execDispatcher *SOPExecutionDispatcher) *SOPOutboxDispatcher {
	return sop.InitSOPOutboxDispatcher(execDispatcher)
}

func InitSOPScheduler(dispatcher any) *SOPScheduler { return sop.InitSOPScheduler(dispatcher) }

func InitSOPStuckDetector(execDispatcher *SOPExecutionDispatcher) *SOPStuckDetector {
	return sop.InitSOPStuckDetector(execDispatcher)
}

func NewApprovalResumeBridge(svc *ApprovalRequestService) *ApprovalResumeBridge {
	if svc == nil {
		return nil
	}
	return sop.NewApprovalResumeBridge(svc)
}

func SetApprovalResumeBridge(b *ApprovalResumeBridge) { sop.SetApprovalResumeBridge(b) }

func GetApprovalResumeBridge() *ApprovalResumeBridge { return sop.GetApprovalResumeBridge() }

func EncodeApprovalSubject(executionID uint, nodeID string) string {
	return sop.EncodeApprovalSubject(executionID, nodeID)
}

func SetMaxExecutedNodeTraceProvider(fn func() int) { sop.SetMaxExecutedNodeTraceProvider(fn) }
func ProbeMaxExecutedNodeTrace() int                { return sop.ProbeMaxExecutedNodeTrace() }
func SetMaxRunningPerSOPProvider(fn func() int)     { sop.SetMaxRunningPerSOPProvider(fn) }
func ProbeMaxRunningPerSOP() int                    { return sop.ProbeMaxRunningPerSOP() }

func SetSOPSchedulerScanIntervalProvider(fn func() time.Duration) {
	sop.SetSOPSchedulerScanIntervalProvider(fn)
}

func SetSOPTimerMaxWaitProvider(fn func() time.Duration) { sop.SetSOPTimerMaxWaitProvider(fn) }

func ProbeSchedulerScanInterval() time.Duration { return sop.ProbeSchedulerScanInterval() }

func SwapSchedulerScanIntervalProvider(fn func() time.Duration) func() {
	return sop.SwapSchedulerScanIntervalProvider(fn)
}

func ProbeTimerMaxWait() time.Duration { return sop.ProbeTimerMaxWait() }

type CompensationConfig = sop.CompensationConfig

func CompensationConfigFromParams(ctx context.Context) CompensationConfig {
	return sop.CompensationConfigFromParams(ctx)
}

func SwapCompensationEnabledFn(fn func() bool) func() { return sop.SwapCompensationEnabledFn(fn) }

func SwapTimerMaxWaitProvider(fn func() time.Duration) func() {
	return sop.SwapTimerMaxWaitProvider(fn)
}

// 出域 / 审批适配器。

// SOPReachSenderAdapter 把 *ProactiveReachService 适配成 sop.ActiveReachSender。
type SOPReachSenderAdapter struct{ inner *ProactiveReachService }

// NewSOPReachSenderAdapter 构造适配器；inner 为 nil 时返回 nil。
func NewSOPReachSenderAdapter(inner *ProactiveReachService) *SOPReachSenderAdapter {
	if inner == nil {
		return nil
	}
	return &SOPReachSenderAdapter{inner: inner}
}

// ReachByCustomer 实现 sop.ActiveReachSender：请求/结果/哨兵在边界翻译。
func (a *SOPReachSenderAdapter) ReachByCustomer(ctx context.Context, req *sop.ReachRequest) (*sop.ReachResult, error) {
	resp, err := a.inner.ReachByCustomer(ctx, &ProactiveReachRequest{
		CustomerID:        req.CustomerID,
		OneID:             req.OneID,
		Content:           req.Content,
		PreferredChannels: req.PreferredChannels,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrDoNotContact):
			return nil, sop.ErrDoNotContact
		case errors.Is(err, ErrReachCooldown):
			return nil, sop.ErrReachCooldown
		case errors.Is(err, ErrReachApprovalDenied):
			// 保留原始理由串：门给的是"为什么拒"，下游日志/节点产物要能读到。
			return nil, fmt.Errorf("%w（%v）", sop.ErrReachApprovalDenied, err)
		default:
			return nil, err
		}
	}
	if resp == nil {
		return &sop.ReachResult{}, nil
	}
	return &sop.ReachResult{Channel: resp.Channel, MessageID: resp.MessageID}, nil
}

type sopAssetResolverAdapter struct{ inner *AssetResolver }

func (a sopAssetResolverAdapter) GetActiveSOP(ctx context.Context) (sop.SOPTemplate, bool) {
	s, ok := a.inner.GetActiveSOP(ctx)
	if !ok || s == nil {
		return nil, false
	}
	return s, true
}

func (a sopAssetResolverAdapter) GetActiveABPlan(ctx context.Context) (sop.ABPlan, bool) {
	p, ok := a.inner.GetActiveABPlan(ctx)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}

// SetSOPReachSender 保留根包入口：装配点是 router.Setup，传 *ProactiveReachService。
func SetSOPReachSender(s *ProactiveReachService) {
	if s == nil {
		sop.SetSOPReachSender(nil)
		return
	}
	sop.SetSOPReachSender(NewSOPReachSenderAdapter(s))
}

func GetSOPReachSender() sop.ActiveReachSender { return sop.GetSOPReachSender() }

func containsString(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

// dynamicParamGetter 每次调用都取当前全局参数中心：拆包后用例会 SetGlobalForTest 换实例，
// 若 init 期捕获指针就会读到旧实例（表现为"参数改了但读不到"）。
type dynamicParamGetter struct{}

func (dynamicParamGetter) GetInt(ctx context.Context, group, key string, fallback int) int {
	return GlobalConfigParam().GetInt(ctx, group, key, fallback)
}

func (dynamicParamGetter) GetDuration(ctx context.Context, group, key string, fallback time.Duration) time.Duration {
	return GlobalConfigParam().GetDuration(ctx, group, key, fallback)
}

func init() {
	sop.SetParamGetter(dynamicParamGetter{})
	sop.SetWorkerAllowed(AllowWorker)
	sop.SetAssetProvider(func() sop.AssetProvider {
		if r := GetAssetResolver(); r != nil {
			return sopAssetResolverAdapter{inner: r}
		}
		return nil
	})
	sop.SetAssetDataLoader(LoadAssetFromDB)
}
