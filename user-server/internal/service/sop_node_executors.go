package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"

	"gorm.io/gorm"
)

type SOPNodeExecutorDeps struct {
	DB          *gorm.DB
	Dispatcher  *llm.Dispatcher
	WSHub       *websocket.Hub
	MsgRepo     *repository.SessionMessageRepository
	SessionRepo *repository.CustomerSessionRepository
	LLMSem      chan struct{}
}

type StartExecutor struct{}

func (e *StartExecutor) NodeType() string { return SOPNodeTypeStart }

func (e *StartExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	logger.Ctx(ctx).Info().
		Str("execution_id", fmt.Sprintf("%d", ec.Execution.ID)).
		Str("sop_id", fmt.Sprintf("%d", ec.Execution.SOPID)).
		Str("customer_id", ec.CustomerID).
		Msg("sop execution started")
	return &NodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{
			"_started_at": ec.StartedAt.Format(time.RFC3339),
			"_trigger":    ec.Execution.ExecutionData["_trigger"],
		},
	}, nil
}

func (e *StartExecutor) IsAsync() bool { return false }

type EndExecutor struct{}

func (e *EndExecutor) NodeType() string { return SOPNodeTypeEnd }

func (e *EndExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	logger.Ctx(ctx).Info().
		Str("execution_id", fmt.Sprintf("%d", ec.Execution.ID)).
		Str("sop_id", fmt.Sprintf("%d", ec.Execution.SOPID)).
		Msg("sop execution ended")
	return &NodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{
			"_ended_at": time.Now().Format(time.RFC3339),
		},
		NextNodeID: "",
	}, nil
}

func (e *EndExecutor) IsAsync() bool { return false }

type MessageNodeBase struct {
	nodeType    string
	scenario    llm.DispatchScenario
	dispatcher  *llm.Dispatcher
	wsHub       *websocket.Hub
	msgRepo     *repository.SessionMessageRepository
	sessionRepo *repository.CustomerSessionRepository

	llmSem chan struct{}
}

func (b *MessageNodeBase) NodeType() string { return b.nodeType }

func (b *MessageNodeBase) IsAsync() bool { return false }

func (b *MessageNodeBase) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	start := time.Now()
	execID := ec.Execution.ID
	nodeID := ec.Node.ID
	sideEffectKey := fmt.Sprintf("message_sent:%d:%s", execID, nodeID)

	if hasSideEffect(ec.Execution, sideEffectKey) {
		logger.Ctx(ctx).Info().
			Str("node_id", nodeID).
			Str("side_effect", sideEffectKey).
			Msg("message already sent, skipping (idempotent)")
		return &NodeExecResult{
			Status: NodeStatusSkipped,
			Output: model.JSONMap{},
		}, nil
	}

	content, contentSource, err := b.resolveContent(ctx, ec)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Str("node_id", nodeID).
			Str("node_type", b.nodeType).
			Msg("resolve message content failed")
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("resolve content failed: %v", err),
			Retryable:    true,
		}, err
	}
	if strings.TrimSpace(content) == "" {
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: "resolved content is empty",
			Retryable:    true,
		}, fmt.Errorf("resolved content is empty for node %s", nodeID)
	}

	if b.msgRepo != nil && ec.SessionID != "" {
		msg := &model.SessionMessage{
			SessionID:   ec.SessionID,
			Content:     content,
			ContentType: model.MessageTypeText,
			SenderType:  "ai",
			SenderID:    fmt.Sprintf("sop_executor:%d", execID),
			SenderName:  "AI 销冠",
			AISource:    contentSource,
		}
		if err := b.msgRepo.Create(ctx, msg); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", ec.SessionID).
				Msg("persist session message failed (non-fatal, will still push WS)")
		}
	}

	if b.wsHub != nil {
		payload := map[string]any{
			"execution_id": execID,
			"node_id":      nodeID,
			"node_type":    b.nodeType,
			"session_id":   ec.SessionID,
			"customer_id":  ec.CustomerID,
			"content":      content,
			"source":       contentSource,
			"timestamp":    time.Now().Format(time.RFC3339),
		}
		_ = b.wsHub.BroadcastToMerchant("", websocket.MsgTypeSOP, payload)
	}

	if ec.SessionID != "" {
		b.updateSessionLastMessage(ctx, ec.SessionID, content)
	}

	latencyMs := time.Since(start).Milliseconds()
	logger.Ctx(ctx).Info().
		Str("node_id", nodeID).
		Str("node_type", b.nodeType).
		Str("content_source", contentSource).
		Int64("latency_ms", latencyMs).
		Int("content_len", len(content)).
		Msg("message node executed")

	return &NodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{
			fmt.Sprintf("_%s_content", b.nodeType): content,
			fmt.Sprintf("_%s_source", b.nodeType):  contentSource,
		},
		SideEffects: []string{sideEffectKey},
	}, nil
}

func (b *MessageNodeBase) resolveContent(ctx context.Context, ec *ExecutionContext) (string, string, error) {
	if ec.Node.Prompt != "" {
		content := renderPromptTemplate(ec.Node.Prompt, ec.ExecutionData)
		if strings.TrimSpace(content) != "" {
			return content, "prompt", nil
		}
	}

	if cfgContent, ok := ec.Node.Config["content"].(string); ok && strings.TrimSpace(cfgContent) != "" {
		return renderPromptTemplate(cfgContent, ec.ExecutionData), "config", nil
	}

	if b.dispatcher != nil {

		if b.llmSem != nil {
			select {
			case b.llmSem <- struct{}{}:
				defer func() { <-b.llmSem }()
			case <-ctx.Done():
				return defaultScriptForNodeType(b.nodeType), "fallback", nil
			}
		}
		systemPrompt := fmt.Sprintf("你是一名销冠，正在执行 %s 节点。根据客户上下文生成一段简短、自然、专业的话术（不超过 100 字）。", b.nodeType)
		userPrompt := buildLLMUserPrompt(ec)
		req := llm.DispatchRequest{
			Scenario:     b.scenario,
			SystemPrompt: systemPrompt,
			Prompt:       userPrompt,
			MaxTokens:    200,
			Temperature:  0.7,
		}
		result, err := b.dispatcher.Dispatch(ctx, req)
		if err == nil && result != nil && strings.TrimSpace(result.Content) != "" {
			return strings.TrimSpace(result.Content), "llm", nil
		}
		logger.Ctx(ctx).Warn().Err(err).
			Str("node_type", b.nodeType).
			Msg("llm dispatch failed, using fallback")
	}

	return defaultScriptForNodeType(b.nodeType), "fallback", nil
}

func (b *MessageNodeBase) updateSessionLastMessage(ctx context.Context, sessionID, content string) {

	if b.sessionRepo == nil {
		return
	}
	_ = b.sessionRepo.UpdateLastMessageBySessionID(ctx, sessionID, content, "ai")
}

func buildLLMUserPrompt(ec *ExecutionContext) string {
	parts := []string{
		fmt.Sprintf("当前节点：%s（%s）", ec.Node.Name, ec.Node.Type),
		fmt.Sprintf("客户 ID：%s", ec.CustomerID),
	}
	if ec.Node.Description != "" {
		parts = append(parts, fmt.Sprintf("节点说明：%s", ec.Node.Description))
	}
	if ec.ExecutionData != nil {
		if v, ok := ec.ExecutionData["customer_name"].(string); ok && v != "" {
			parts = append(parts, fmt.Sprintf("客户姓名：%s", v))
		}
		if v, ok := ec.ExecutionData["intent_score"].(float64); ok {
			parts = append(parts, fmt.Sprintf("意向分数：%.2f", v))
		}
		if v, ok := ec.ExecutionData["last_customer_message"].(string); ok && v != "" {
			parts = append(parts, fmt.Sprintf("客户最近消息：%s", v))
		}
	}
	return strings.Join(parts, "\n")
}

func renderPromptTemplate(tmpl string, data model.JSONMap) string {
	if data == nil {
		return tmpl
	}
	out := tmpl
	for k, v := range data {
		placeholder := fmt.Sprintf("{{%s}}", k)
		if strings.Contains(out, placeholder) {
			out = strings.ReplaceAll(out, placeholder, fmt.Sprintf("%v", v))
		}
	}
	return out
}

func defaultScriptForNodeType(nodeType string) string {
	switch nodeType {
	case SOPNodeTypeGreeting:
		return "您好，欢迎咨询，我是您的专属顾问，请问有什么可以帮您？"
	case SOPNodeTypeInquire:
		return "请问您主要想了解哪方面的产品或服务？"
	case SOPNodeTypeIntroduce:
		return "我们的产品具有以下核心优势，我为您简要介绍一下。"
	case SOPNodeTypeHandle:
		return "理解您的考虑，这是非常重要的因素，我们一起来分析一下。"
	case SOPNodeTypeClose:
		return "针对您的情况，我们可以提供专属优惠，您看是否方便确认一下？"
	case SOPNodeTypeInvite:
		return "诚邀您参与我们的体验活动，名额有限，期待您的加入。"
	case SOPNodeTypeFollowUp:
		return "好的，我先把相关资料发给您，后续有任何问题随时联系我。"
	case SOPNodeTypeActivate:
		return "好久不见，最近我们推出了新的产品方案，想与您分享一下。"
	case SOPNodeTypeNurture:
		return "感谢您的关注，我先为您提供一些参考资料，您可以慢慢了解。"
	case SOPNodeTypeMessage, SOPNodeTypeAction:
		return "您好，有什么可以帮您的吗？"
	case SOPNodeTypeSendOffer:
		return "针对您这样的优质客户，我们提供专属优惠方案。"
	default:
		return "您好，我是您的专属顾问。"
	}
}

func (b *MessageNodeBase) SetWSHub(ctx context.Context, hub *websocket.Hub) {
	b.wsHub = hub
}

func NewMessageNodeExecutor(nodeType string, scenario llm.DispatchScenario, deps *SOPNodeExecutorDeps) *MessageNodeBase {
	return &MessageNodeBase{
		nodeType:    nodeType,
		scenario:    scenario,
		dispatcher:  deps.Dispatcher,
		wsHub:       deps.WSHub,
		msgRepo:     deps.MsgRepo,
		sessionRepo: deps.SessionRepo,
		llmSem:      deps.LLMSem,
	}
}

type ConditionExecutor struct {
	nodeType string
}

func (e *ConditionExecutor) NodeType() string { return e.nodeType }

func (e *ConditionExecutor) IsAsync() bool { return false }

func (e *ConditionExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	if len(ec.Node.Conditions) > 0 {
		br, err := SOPEvaluateConditionBranches(ec.Node.Conditions, ec.ExecutionData)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("node_id", ec.Node.ID).
				Msg("evaluate condition branches failed, fallback to Next[0]")
		} else if br.Matched && br.NextNode != "" {
			matchedLabel := ""
			for _, branch := range ec.Node.Conditions {
				if branch.Next == br.NextNode {
					matchedLabel = branch.Label
					break
				}
			}
			logger.Ctx(ctx).Info().
				Str("node_id", ec.Node.ID).
				Str("matched_branch", matchedLabel).
				Str("next_node", br.NextNode).
				Msg("condition branch matched")
			return &NodeExecResult{
				Status:     NodeStatusCompleted,
				Output:     model.JSONMap{"_condition_branch": matchedLabel},
				NextNodeID: br.NextNode,
			}, nil
		}
	}

	if ec.Node.Condition != "" {
		result, err := SOPEvaluateNodeCondition(ec.Node, ec.ExecutionData)
		if err == nil {
			if nextID, ok := result["_next_node"].(string); ok && nextID != "" {
				return &NodeExecResult{
					Status:     NodeStatusCompleted,
					Output:     model.JSONMap{"_condition_result": result},
					NextNodeID: nextID,
				}, nil
			}
		}
	}

	nextID := ""
	if len(ec.Node.Next) > 0 {
		nextID = ec.Node.Next[0]
	}
	logger.Ctx(ctx).Info().
		Str("node_id", ec.Node.ID).
		Str("fallback_next", nextID).
		Msg("condition node fallback to Next[0]")
	return &NodeExecResult{
		Status:     NodeStatusCompleted,
		Output:     model.JSONMap{"_condition_fallback": true},
		NextNodeID: nextID,
	}, nil
}

type LLMNodeExecutor struct {
	nodeType   string
	dispatcher *llm.Dispatcher
	llmSem     chan struct{}
	execRepo   *repository.SopExecutionRepository
}

func (e *LLMNodeExecutor) NodeType() string { return e.nodeType }

func (e *LLMNodeExecutor) IsAsync() bool { return false }

func (e *LLMNodeExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	if e.llmSem != nil {
		select {
		case e.llmSem <- struct{}{}:
			defer func() { <-e.llmSem }()
		case <-ctx.Done():
			return &NodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: "llm semaphore wait timeout: " + ctx.Err().Error(),
				Retryable:    true,
			}, ctx.Err()
		}
	}

	if e.dispatcher == nil {
		return &NodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: "llm dispatcher not configured",
			Retryable:    false,
		}, fmt.Errorf("llm dispatcher not configured for node %s", ec.Node.ID)
	}

	candidates := ec.Node.Next
	prompt := buildLLMDecisionPrompt(ec, candidates)
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		SystemPrompt: "你是一名销冠决策助手。根据当前对话上下文，从候选节点中选择最合适的下一节点。只返回 JSON。",
		Prompt:       prompt,
		MaxTokens:    200,
		Temperature:  0.3,
		JSONMode:     true,
	}

	result, err := e.dispatcher.Dispatch(ctx, req)
	if err != nil || result == nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("node_id", ec.Node.ID).
			Msg("llm dispatch failed, fallback to Next[0]")
		return &NodeExecResult{
			Status:     NodeStatusCompleted,
			Output:     model.JSONMap{"_llm_error": fmt.Sprintf("%v", err)},
			NextNodeID: firstOrEmpty(candidates),
			TokensUsed: 0,
		}, nil
	}

	decision, parseErr := parseLLMDecision(result.Content)
	if parseErr != nil || decision.NextNodeID == "" {
		logger.Ctx(ctx).Warn().Err(parseErr).
			Str("node_id", ec.Node.ID).
			Str("raw_content", result.Content).
			Msg("parse llm decision failed, fallback to Next[0]")
		return &NodeExecResult{
			Status:     NodeStatusCompleted,
			Output:     model.JSONMap{"_llm_raw": result.Content},
			NextNodeID: firstOrEmpty(candidates),
			TokensUsed: result.TotalTokens,
		}, nil
	}

	if !containsString(candidates, decision.NextNodeID) {
		logger.Ctx(ctx).Warn().
			Str("node_id", ec.Node.ID).
			Str("llm_choice", decision.NextNodeID).
			Strs("candidates", candidates).
			Msg("llm returned invalid node id, fallback to Next[0]")
		return &NodeExecResult{
			Status:     NodeStatusCompleted,
			Output:     model.JSONMap{"_llm_invalid": decision.NextNodeID},
			NextNodeID: firstOrEmpty(candidates),
			TokensUsed: result.TotalTokens,
		}, nil
	}

	logger.Ctx(ctx).Info().
		Str("node_id", ec.Node.ID).
		Str("llm_choice", decision.NextNodeID).
		Str("reason", decision.Reason).
		Int("tokens", result.TotalTokens).
		Msg("llm decision made")

	return &NodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{
			"_llm_decision": decision.NextNodeID,
			"_llm_reason":   decision.Reason,
		},
		NextNodeID: decision.NextNodeID,
		TokensUsed: result.TotalTokens,
	}, nil
}

func NewLLMNodeExecutor(nodeType string, deps *SOPNodeExecutorDeps) *LLMNodeExecutor {
	execRepo := repository.NewSopExecutionRepository(deps.DB)
	return &LLMNodeExecutor{
		nodeType:   nodeType,
		dispatcher: deps.Dispatcher,
		llmSem:     deps.LLMSem,
		execRepo:   execRepo,
	}
}

// Compensate D03 试点 1：清除该节点在 ExecutionData 中的产物键（业务状态回滚，幂等）。
// 已知粗粒度：_llm_decision/_llm_reason 为共享键（多 LLM 节点互相覆盖），
// 清理影响最后一个写入者——单 LLM 节点 SOP 语义精确；多节点 SOP 声明为粗粒度回滚。
// dispatcher/db 为 nil（直构/降级）时直接成功——无状态可回滚。
func (e *LLMNodeExecutor) Compensate(ctx context.Context, execCtx *ExecutionContext) error {
	if e == nil || execCtx == nil || execCtx.Execution == nil {
		return nil
	}
	if e.execRepo == nil || execCtx.Node == nil {
		return nil
	}

	exec, err := e.execRepo.GetByID(ctx, execCtx.Execution.ID)
	if err != nil {
		return err
	}
	if exec.ExecutionData == nil {
		return nil
	}
	changed := false
	for _, k := range []string{"_llm_decision", "_llm_reason", "_llm_" + execCtx.Node.ID} {
		if _, exists := exec.ExecutionData[k]; exists {
			delete(exec.ExecutionData, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return e.execRepo.UpdateFields(ctx, exec.ID, map[string]any{"execution_data": exec.ExecutionData})
}

type llmDecision struct {
	NextNodeID string `json:"next_node_id"`
	Reason     string `json:"reason"`
}

func parseLLMDecision(content string) (*llmDecision, error) {
	content = strings.TrimSpace(content)

	var d llmDecision
	if err := json.Unmarshal([]byte(content), &d); err == nil {
		return &d, nil
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		sub := content[start : end+1]
		if err := json.Unmarshal([]byte(sub), &d); err == nil {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("invalid llm decision json: %s", content)
}

func buildLLMDecisionPrompt(ec *ExecutionContext, candidates []string) string {
	parts := []string{
		fmt.Sprintf("当前节点：%s（%s）", ec.Node.Name, ec.Node.Type),
		fmt.Sprintf("候选下一节点：%s", strings.Join(candidates, ", ")),
	}
	if ec.Node.Prompt != "" {
		parts = append(parts, fmt.Sprintf("决策提示：%s", ec.Node.Prompt))
	}
	if ec.ExecutionData != nil {
		if v, ok := ec.ExecutionData["intent_score"].(float64); ok {
			parts = append(parts, fmt.Sprintf("意向分数：%.2f", v))
		}
		if v, ok := ec.ExecutionData["last_customer_message"].(string); ok && v != "" {
			parts = append(parts, fmt.Sprintf("客户最近消息：%s", v))
		}
	}
	parts = append(parts, "请返回 JSON：{\"next_node_id\":\"<候选节点ID>\",\"reason\":\"<选择理由>\"}")
	return strings.Join(parts, "\n")
}

type WaitExecutor struct {
	db        *gorm.DB
	timerRepo *repository.SOPTimerRepository
}

const sopTimerDefaultMaxWait = 24 * time.Hour

func (e *WaitExecutor) NodeType() string { return SOPNodeTypeWait }

func (e *WaitExecutor) IsAsync() bool { return true }

func (e *WaitExecutor) Execute(ctx context.Context, ec *ExecutionContext) (*NodeExecResult, error) {
	waitSeconds, _ := ec.Node.Config["wait_seconds"].(float64)
	waitEventStr, _ := ec.Node.Config["wait_event"].(string)
	waitUntilStr, _ := ec.Node.Config["wait_until"].(string)

	var waitUntil time.Time
	waitEvent := WaitEventTimer

	if waitUntilStr != "" {
		if t, err := time.Parse(time.RFC3339, waitUntilStr); err == nil {
			waitUntil = t
		}
	}
	if waitUntil.IsZero() && waitSeconds > 0 {
		waitUntil = time.Now().Add(time.Duration(int64(waitSeconds)) * time.Second)
	}
	if waitUntil.IsZero() {
		waitEvent = WaitEventCustomerReply
		if waitEventStr != "" {
			waitEvent = waitEventStr
		}
		waitUntil = time.Now().Add(24 * time.Hour)
	} else if waitEventStr != "" {
		waitEvent = waitEventStr
	}

	if e.timerRepo != nil {

		now := time.Now()
		maxWaitSeconds, _ := ec.Node.Config["max_wait_seconds"].(float64)
		if maxWaitSeconds <= 0 {
			maxWaitSeconds = float64(sopTimerDefaultMaxWait / time.Second)
		}
		maxWaitAt := now.Add(time.Duration(int64(maxWaitSeconds)) * time.Second)
		timer := &model.SOPTimer{
			ExecutionID: ec.Execution.ID,
			NodeID:      ec.Node.ID,
			WaitEvent:   waitEvent,
			WaitUntil:   waitUntil,
			Status:      "pending",

			ExpiresAt: &waitUntil,
			MaxWaitAt: &maxWaitAt,
			Payload: model.JSONMap{
				"trace_id":    ec.TraceID,
				"customer_id": ec.CustomerID,
				"session_id":  ec.SessionID,
				"attempt":     ec.Attempt,
				"expires_at":  waitUntil.Format(time.RFC3339),
				"max_wait_at": maxWaitAt.Format(time.RFC3339),
			},
		}
		if err := e.timerRepo.Create(ctx, timer); err != nil {
			logger.Ctx(ctx).Error().Err(err).
				Str("node_id", ec.Node.ID).
				Msg("create sop_timer failed")
			return &NodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: fmt.Sprintf("create timer failed: %v", err),
				Retryable:    true,
			}, err
		}
		logger.Ctx(ctx).Info().
			Uint("timer_id", timer.ID).
			Str("node_id", ec.Node.ID).
			Str("wait_event", waitEvent).
			Time("wait_until", waitUntil).
			Msg("sop timer created")
	}

	return &NodeExecResult{
		Status:    NodeStatusWaiting,
		Output:    model.JSONMap{"_wait_event": waitEvent, "_wait_until": waitUntil.Format(time.RFC3339)},
		WaitUntil: &waitUntil,
		WaitEvent: waitEvent,
	}, nil
}

// NewWaitExecutor 创建等待节点执行器
//
// 当 deps.DB 非 nil 时构造 timerRepo，否则 timerRepo 为 nil（Execute 会跳过 DB 写入）。
func NewWaitExecutor(deps *SOPNodeExecutorDeps) *WaitExecutor {
	e := &WaitExecutor{db: deps.DB}
	if deps.DB != nil {
		e.timerRepo = repository.NewSOPTimerRepository(deps.DB)
	}
	return e
}

// Compensate D03 试点 2：删除该执行+节点的 pending 定时器（幂等）。
// 价值场景：wait 节点重试（每次 attempt 都 Create）可产生重复 pending timer；
// 补偿清空防重启恢复后幽灵触发。带 status='pending' 守卫与 MarkFired 原子互斥——
// 客户已回复（timer 已 fired）时删不掉，不打断已完成节点。repo nil（直构/降级）直接成功。
func (e *WaitExecutor) Compensate(ctx context.Context, execCtx *ExecutionContext) error {
	if e == nil || execCtx == nil || execCtx.Execution == nil || execCtx.Node == nil {
		return nil
	}
	if e.timerRepo == nil {
		return nil
	}
	_, err := e.timerRepo.DeletePendingByExecutionAndNode(ctx, execCtx.Execution.ID, execCtx.Node.ID)
	return err
}

// RegisterAllNodeExecutors 注册所有 14 种节点执行器 + 5 种旧版兼容执行器
//
// 应在 SOPExecutionDispatcher 初始化时调用一次。
// 重复注册会 panic（启动期错误）。
//
// 节点-Saga 补偿能力对照表（D03，Compensable 接口断言决定是否补偿）：
//   - wait            → 可补偿：删除 pending 定时器（WaitExecutor.Compensate，防重复/幽灵 timer）
//   - llm / ai_decide → 可补偿：清 ExecutionData 中该节点产物键（LLMNodeExecutor.Compensate，业务状态回滚）
//   - 9 种 message / message / action / send_offer → 不可补偿（发消息=外部副作用事务点，只能"补偿通知"，留 TODO）
//   - start / end / condition / branch → 控制流，无副作用无需补偿（skipped）
func RegisterAllNodeExecutors(registry *NodeExecutorRegistry, deps *SOPNodeExecutorDeps) {
	registry.Register(context.Background(), &StartExecutor{})
	registry.Register(context.Background(), &EndExecutor{})

	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeInquire, llm.ScenarioSOPReply, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeIntroduce, llm.ScenarioSOPReply, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeHandle, llm.ScenarioObjection, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeClose, llm.ScenarioHighQuality, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeInvite, llm.ScenarioSOPReply, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeFollowUp, llm.ScenarioFriendlyChat, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeActivate, llm.ScenarioFriendlyChat, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeNurture, llm.ScenarioFriendlyChat, deps))

	registry.Register(context.Background(), &ConditionExecutor{nodeType: SOPNodeTypeCondition})

	registry.Register(context.Background(), NewLLMNodeExecutor(SOPNodeTypeLLM, deps))

	registry.Register(context.Background(), NewWaitExecutor(deps))

	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeMessage, llm.ScenarioFriendlyChat, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeAction, llm.ScenarioSOPReply, deps))
	registry.Register(context.Background(), NewMessageNodeExecutor(SOPNodeTypeSendOffer, llm.ScenarioObjection, deps))
	registry.Register(context.Background(), NewLLMNodeExecutor(SOPNodeTypeAIDecide, deps))
	registry.Register(context.Background(), &ConditionExecutor{nodeType: SOPNodeTypeBranch})

	logger.GetLogger().Info().
		Strs("registered_types", registry.AllRegistered(context.Background())).
		Msg("all sop node executors registered")
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func containsString(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
