package agent_runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/pkg/utils/logger"
)

// CoreDataFlowOrchestrator 核心数据流编排器
type CoreDataFlowOrchestrator struct {
	cycle    *InferenceCycle
	escalate EscalationTrigger

	assetLoader     AssetLoader
	chatHistory     portcontract.ChatHistoryPort
	toolRouter      ToolRouterExecutor
	trimmer         ResponseTrimmer
	publisher       ResponsePublisher
	executionGate   ExecutionGate
	replyGenerator  ReplyGenerator
	finalReviewer   Reviewer
	deliveryGateway delivery.Gateway

	mu    sync.RWMutex
	stats DataFlowStats
}

// DataFlowStats 数据流统计
type DataFlowStats struct {
	TotalTasks       int64
	FastHandoffCount int64
	ToolCallCount    int64
	DirectReplyCount int64
	ErrorCount       int64
	RejectedCount    int64
	AvgTotalDuration time.Duration
}

// EscalationTrigger 转人工触发器（解耦方向7 的 HumanEscalationManager）
type EscalationTrigger interface {
	Trigger(ctx context.Context, sessionID, reason string) error
}

// AssetLoader 资产包加载器（解耦方向9 的 AssetBundle）
type AssetLoader interface {
	LoadContext(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext) (*AssetContext, error)
}

// AssetContext 资产上下文
type AssetContext struct {
	L1ShortTerm map[string]string
	L2Profile   map[string]string
	PromptText  string
	SystemTools []string
}

// ToolRouterExecutor 工具路由执行器（解耦方向5 的 ToolRouter）
type ToolRouterExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]any) (ToolOutput, error)
}

// ToolOutput 工具输出
type ToolOutput struct {
	Success bool
	Data    string
	Error   string
}

// ResponseTrimmer 响应裁剪器（裁掉末尾 JSON / 调试信息）
type ResponseTrimmer interface {
	Trim(reply string) string
}

// ResponsePublisher 响应发布器（推送到前端 SSE/WS）
type ResponsePublisher interface {
	Publish(ctx context.Context, channel, customerID, content string) error
}

// NewCoreDataFlowOrchestrator 构造编排器
func NewCoreDataFlowOrchestrator(cycle *InferenceCycle, escalate EscalationTrigger) *CoreDataFlowOrchestrator {
	if cycle == nil {
		cycle = NewInferenceCycle()
	}
	return &CoreDataFlowOrchestrator{
		cycle:       cycle,
		escalate:    escalate,
		assetLoader: noopAssetLoader{},
		trimmer:     defaultTrimmer{},
	}
}

// SetAssetLoader 注入资产加载器
func (o *CoreDataFlowOrchestrator) SetAssetLoader(al AssetLoader) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.assetLoader = al
}

// SetChatHistory 注入会话历史端口（S5 写侧：AI 回复成功后 AppendHistory）。
// nil 时跳过写入，保持旧行为（不破坏规划/执行双模）。
func (o *CoreDataFlowOrchestrator) SetChatHistory(h portcontract.ChatHistoryPort) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.chatHistory = h
}

// SetToolRouter 注入工具路由
func (o *CoreDataFlowOrchestrator) SetToolRouter(tr ToolRouterExecutor) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolRouter = tr
}

// SetTrimmer 注入裁剪器
func (o *CoreDataFlowOrchestrator) SetTrimmer(t ResponseTrimmer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.trimmer = t
}

// SetPublisher 注入发布器
func (o *CoreDataFlowOrchestrator) SetPublisher(p ResponsePublisher) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.publisher = p
}

// OrchestratorResult 编排结果
type OrchestratorResult struct {
	Delivery             *delivery.Record      `json:"delivery,omitempty"`
	Mode                 ExecutionMode         `json:"mode"`
	ExecutionStatus      ExecutionStatus       `json:"execution_status"`
	Executed             bool                  `json:"executed"`
	Plan                 *ActionPlan           `json:"plan,omitempty"`
	PlannedToolCallCount int                   `json:"planned_tool_call_count"`
	ToolResults          []ToolExecutionResult `json:"tool_results,omitempty"`
	Outcome              InferenceOutcome      `json:"outcome"`
	Publishable          bool                  `json:"publishable"`
	FailedStage          string                `json:"failed_stage,omitempty"`
	ErrorCode            string                `json:"error_code,omitempty"`
	Degradations         []string              `json:"degradations,omitempty"`
	SessionID            string
	FinalReply           string
	HandoffToHuman       bool
	HandoffReason        string
	ToolCallCount        int
	Stages               []StageDecision
	TotalDuration        time.Duration
	CrisisLevel          CrisisLevel
	AssetContext         *AssetContext
}

// Process 默认仅规划；外部执行必须显式选择 execution 并装配执行端口。
func (o *CoreDataFlowOrchestrator) Process(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext) (*OrchestratorResult, error) {
	return o.ProcessWithMode(ctx, payload, agentCtx, ExecutionModePlanning)
}

// ProcessWithMode 区分规划完成与实际执行，规划不触发外部副作用。
func (o *CoreDataFlowOrchestrator) ProcessWithMode(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext, mode ExecutionMode) (*OrchestratorResult, error) {
	if agentCtx == nil {
		agentCtx = &AgentContext{
			AgentID:   0,
			AgentCode: "default",
			AgentType: "customer_service",
			LoadedAt:  time.Now(),
		}
	}

	start := time.Now()
	result := &OrchestratorResult{
		SessionID:       payload.SessionID,
		Mode:            mode,
		ExecutionStatus: ExecutionNotExecuted,
	}
	defer func() {
		result.TotalDuration = time.Since(start)
		o.mu.Lock()
		defer o.mu.Unlock()
		o.stats.TotalTasks++
		if o.stats.AvgTotalDuration == 0 {
			o.stats.AvgTotalDuration = result.TotalDuration
		} else {
			o.stats.AvgTotalDuration = (o.stats.AvgTotalDuration*9 + result.TotalDuration) / 10
		}
	}()
	if mode == "" {
		mode, result.Mode = ExecutionModePlanning, ExecutionModePlanning
	}
	if mode != ExecutionModePlanning && mode != ExecutionModeExecution {
		return o.executionFailure(ctx, result, "invalid_execution_mode", nil)
	}
	o.mu.RLock()
	assetLoader := o.assetLoader
	ports := executionPorts{o.executionGate, o.toolRouter, o.replyGenerator, o.finalReviewer, o.trimmer, o.deliveryGateway}
	o.mu.RUnlock()

	if result.SessionID == "" {
		result.SessionID = payload.ChannelType + ":" + payload.CustomerID
	}

	if assetLoader != nil {
		assetCtx, err := assetLoader.LoadContext(ctx, payload, agentCtx)
		if err != nil {
			logger.Warnf("[dataflow] asset load failed session=%s err=%v", result.SessionID, err)
		} else {
			result.AssetContext = assetCtx
		}
	}

	decision, err := o.cycle.RunOnce(ctx, payload, agentCtx)
	if decision != nil {
		result.Outcome, result.Publishable = decision.Outcome, decision.Publishable
		result.FailedStage, result.ErrorCode = decision.FailedStage, decision.ErrorCode
		result.Stages = append([]StageDecision{}, decision.Stages...)
		result.Degradations = append([]string(nil), decision.Degradations...)
		result.Plan = decision.Plan
		if decision.Plan != nil {
			result.PlannedToolCallCount = len(decision.Plan.ToolCalls)
		}
	}
	result.Publishable = false
	if err != nil {
		result.Outcome = InferenceFailed
		result.ExecutionStatus = ExecutionFailed
		o.recordError()
		return result, err
	}
	if decision == nil {
		result.Outcome, result.ErrorCode = InferenceFailed, "inference_unavailable"
		result.ExecutionStatus = ExecutionFailed
		o.recordError()
		return result, errors.New("inference decision is nil")
	}
	result.HandoffToHuman = decision.HandoffToHuman
	result.HandoffReason = decision.HandoffReason
	result.CrisisLevel = decision.Crisis.Level
	result.TotalDuration = time.Since(start)
	if decision.Outcome == InferenceRejected {
		result.ExecutionStatus = ExecutionRejected
		o.mu.Lock()
		o.stats.RejectedCount++
		o.mu.Unlock()
		return result, nil
	}

	if mode == ExecutionModePlanning {
		return result, nil
	}
	return o.executePlan(ctx, payload, agentCtx, decision, result, ports)
}

// GetStats 获取统计
func (o *CoreDataFlowOrchestrator) GetStats() DataFlowStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.stats
}

func (o *CoreDataFlowOrchestrator) recordHandoff() {
	o.mu.Lock()
	o.stats.FastHandoffCount++
	o.mu.Unlock()
}

func (o *CoreDataFlowOrchestrator) recordToolCall() {
	o.mu.Lock()
	o.stats.ToolCallCount++
	o.mu.Unlock()
}

func (o *CoreDataFlowOrchestrator) recordDirectReply() {
	o.mu.Lock()
	o.stats.DirectReplyCount++
	o.mu.Unlock()
}

func (o *CoreDataFlowOrchestrator) recordError() {
	o.mu.Lock()
	o.stats.ErrorCount++
	o.mu.Unlock()
}

type noopAssetLoader struct{}

func (noopAssetLoader) LoadContext(_ context.Context, _ CustomerMessagePayload, _ *AgentContext) (*AssetContext, error) {
	return &AssetContext{
		L1ShortTerm: map[string]string{},
		L2Profile:   map[string]string{},
		PromptText:  "",
		SystemTools: []string{},
	}, nil
}

type defaultTrimmer struct{}

func (defaultTrimmer) Trim(reply string) string {
	if reply == "" {
		return reply
	}

	for {
		idx := strings.LastIndex(reply, "```json")
		if idx < 0 {
			break
		}
		rest := reply[idx+len("```json"):]
		if nl := strings.Index(rest, "\n"); nl == 0 {
			rest = rest[1:]
		} else if nl > 0 {
			rest = rest[nl+1:]
		}
		closeIdx := strings.Index(rest, "```")
		if closeIdx < 0 {
			break
		}
		before := reply[:idx]
		after := rest[closeIdx+3:]
		reply = strings.TrimRight(before, "\n") + after
	}

	for {
		idx := strings.LastIndex(reply, "{\"intent\"")
		if idx < 0 {
			break
		}
		depth := 0
		end := -1
		for i := idx; i < len(reply); i++ {
			if reply[i] == '{' {
				depth++
			} else if reply[i] == '}' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			break
		}
		before := reply[:idx]
		reply = strings.TrimRight(before, "\n")
	}

	return reply
}

// EscalationAdapter 把 HumanEscalationManager 适配成 EscalationTrigger
type EscalationAdapter struct {
	Fn func(ctx context.Context, sessionID, reason string) error
}

// Trigger 触发转人工
func (a EscalationAdapter) Trigger(ctx context.Context, sessionID, reason string) error {
	if a.Fn == nil {
		return nil
	}
	return a.Fn(ctx, sessionID, reason)
}
