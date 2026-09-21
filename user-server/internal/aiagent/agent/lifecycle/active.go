package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// 本文件兑现「双模式」里一直只有注释的那一半：Passive / Active 两个实现。
//
// C7 裁定的落法：**Active 不自建 Agent 循环**。它把一次主动运行整个交给 SOP 引擎
// （`SOPService.Execute`），工具链与出域都由 SOP 节点执行器去做 —— 于是闸门（T-P3-07 /
// T-P5-03）与归因（P8）天然只有一条路可走。本包因此**不许** import 会话引擎
// （`service` / `agent/runtime`），AC② 由 `TestActiveNeverImportsConversationEngine` 钉住。
//
// 三步职责的边界（本卡刻意不做的事都写在这里，不藏在代码里）：
//
//	选材 —— 只认"请求里那一个客户"。群体选材在 T-P5-01 的圈选器，两处各圈一遍必然漂移。
//	决策 —— 只做"从智能体挂的 SOP 里取第一个可解析的"。`DecisionStrategyIDs` 那条链今天
//	        没有任何执行方读它，接进来就是凭空造一套语义（见 §交付说明的欠账登记）。
//	归因 —— 把客户的 OneID 塞进 `sop_executions.execution_data.one_id`，不新建列、不新建表。

const (
	// inputKeyTrigger 与调度器同名（`sop_scheduler.go` 写的是 "_trigger"），
	// 两条主动入口在执行记录里长得一样，事后不必分"这是定时器跑的还是人跑的"。
	inputKeyTrigger = "_trigger"
	// triggerActiveLifecycle 本次执行的发起方标识。
	triggerActiveLifecycle = "active_lifecycle"
	// inputKeyOneID 归因键。空 OneID 时**不写这个键**：空串会被下游当成一个合法归因对象，
	// 比缺键更难查（`customer.UnifiedID` 对历史客户行确实可能为空）。
	inputKeyOneID = "one_id"
	// inputKeyAgentID 记下是哪个智能体挂的这张 SOP：`sop_executions` 只有 sop_id，
	// 一张 SOP 被多个智能体挂时，没有这个键就回不去"谁发起的"。
	inputKeyAgentID = "agent_id"
)

// ConversationRunner 被动模式依赖的会话引擎（`*service.SalesEngine` 天然满足）。
type ConversationRunner interface {
	HandleWithAgent(ctx context.Context, req *dto.SalesRequest, agentCtx *dto.AgentContext) (*dto.SalesResponse, error)
}

// SOPExecutor 主动模式唯一的编排出口（`*service.SOPService.Execute` 天然满足）。
type SOPExecutor interface {
	Execute(ctx context.Context, req *dto.ExecuteRequest) (*model.SOPExecution, error)
}

// CustomerLookup 取客户行以拿 OneID（`repository.CustomerRepository` 的窄切片）。
//
// 契约里要说清一件事：仓层 not-found 回的是 `(nil, nil)` 而不是错误，
// 所以调用方必须自己判空 —— 把"查不到"当成"没有 OneID"放行，就会建出一条没有归因对象的执行。
type CustomerLookup interface {
	GetByID(ctx context.Context, id string) (*model.Customer, error)
}

// PassiveAgentLifecycle 被动模式：入站消息驱动，交给既有会话引擎跑一轮应答。
//
// 本卡不改它今天在生产里怎么走（渠道入站仍走 `SmartCSOrchestrator`），只是让这个类型第一次
// 真实存在，从而 `Resolver` 回退时有东西可返回、"未知模式走被动"不再是一句注释。
type PassiveAgentLifecycle struct {
	runner ConversationRunner
}

// NewPassiveAgentLifecycle 构造被动生命周期；runner 为 nil 时构造不 panic，Run 时才红。
func NewPassiveAgentLifecycle(runner ConversationRunner) *PassiveAgentLifecycle {
	return &PassiveAgentLifecycle{runner: runner}
}

// Mode 实现 AgentLifecycle。
func (l *PassiveAgentLifecycle) Mode() string { return string(model.AgentModePassive) }

// Run 把一次请求转成会话引擎的一轮应答。
func (l *PassiveAgentLifecycle) Run(ctx context.Context, agentCtx *dto.AgentContext, req *LifecycleRequest) (*LifecycleResult, error) {
	if l == nil || l.runner == nil {
		return nil, errors.New("passive: 会话引擎未装配")
	}
	if req == nil || req.Content == "" {
		return nil, errors.New("passive: 没有入站消息就没有事做（主动入口请挂 active 模式的 SOP）")
	}
	salesReq := &dto.SalesRequest{
		SessionID:   req.SessionID,
		CustomerID:  req.CustomerID,
		OneID:       req.OneID,
		UserMessage: req.Content,
		Platform:    req.Channel,
	}
	resp, err := l.runner.HandleWithAgent(ctx, salesReq, agentCtx)
	if err != nil {
		return nil, fmt.Errorf("passive: 会话引擎失败: %w", err)
	}
	if resp == nil {
		return nil, errors.New("passive: 会话引擎返回空响应")
	}
	out := &LifecycleResult{
		Mode:         l.Mode(),
		ReplyContent: resp.Reply,
		Handoff:      resp.TransferredToHuman,
		StopReason:   "completed",
	}
	if resp.TransferredToHuman {
		out.StopReason = resp.TransferReason
	}
	return out, nil
}

// ActiveAgentLifecycle 主动模式：智能体主动出域，编排走 SOP。
type ActiveAgentLifecycle struct {
	sop       SOPExecutor
	customers CustomerLookup
}

// NewActiveAgentLifecycle 构造主动生命周期。两个依赖任一为 nil，Run 时红而不是静默零动作。
func NewActiveAgentLifecycle(sop SOPExecutor, customers CustomerLookup) *ActiveAgentLifecycle {
	return &ActiveAgentLifecycle{sop: sop, customers: customers}
}

// Mode 实现 AgentLifecycle。
func (l *ActiveAgentLifecycle) Mode() string { return string(model.AgentModeActive) }

// Run 按"选材 → 决策 → 交给 SOP"三步跑一次主动运行。
//
// 返回的 error 一律是"这一步没做成"的事实描述，调用方（HTTP 入口）原样上抛：
// 主动入口的每一条拒绝都必须能被运营读懂，因为它的反面是一次已经发出去的外联。
func (l *ActiveAgentLifecycle) Run(ctx context.Context, agentCtx *dto.AgentContext, req *LifecycleRequest) (*LifecycleResult, error) {
	if l == nil || l.sop == nil || l.customers == nil {
		return nil, errors.New("active: SOP 编排出口或客户源未装配")
	}
	if agentCtx == nil || len(agentCtx.SOPIDs) == 0 {
		return nil, errors.New("active: 该智能体没挂任何 SOP，无编排可做")
	}
	if req == nil || req.CustomerID == "" {
		return nil, errors.New("active: 主动运行必须指定一个客户（群体选材在圈选器，不在这里）")
	}

	customer, err := l.customers.GetByID(ctx, req.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("active: 查客户 %s 失败: %w", req.CustomerID, err)
	}
	if customer == nil {
		return nil, fmt.Errorf("active: 客户 %s 不存在，不开工", req.CustomerID)
	}

	sopID, err := firstParsableSOPID(agentCtx.SOPIDs)
	if err != nil {
		return nil, fmt.Errorf("active: 智能体 %d 挂的 SOP 里没有可用项: %w", agentCtx.AgentID, err)
	}

	input := map[string]any{
		inputKeyTrigger: triggerActiveLifecycle,
		inputKeyAgentID: agentCtx.AgentID,
	}
	if customer.UnifiedID != "" {
		input[inputKeyOneID] = customer.UnifiedID
	}
	exec, err := l.sop.Execute(ctx, &dto.ExecuteRequest{
		SOPID:      sopID,
		CustomerID: req.CustomerID,
		// 合成键用 agent_id 而不是 agent_code：`sop_executions.session_id` 是 varchar(120)，
		// 押在 agent_code(64)+customers.id(36) 两列宽度上只剩 12 字节余量，那两列任一放宽
		// 就会把"可读的键"变成一次 INSERT 报错（见 TestActiveSessionKeyStaysWithinColumn）。
		// 与调度器同一形状（`sop_scheduler.go` 用的是 "scheduler-<agent_id>"）。
		SessionID: fmt.Sprintf("active-%d-%s", agentCtx.AgentID, req.CustomerID),
		Input:     input,
	})
	if err != nil {
		return nil, fmt.Errorf("active: 启动 SOP %d 失败: %w", sopID, err)
	}
	if exec == nil {
		return nil, fmt.Errorf("active: SOP %d 返回了空执行记录", sopID)
	}

	out := &LifecycleResult{
		Mode:        l.Mode(),
		ExecutionID: exec.ID,
		OneID:       customer.UnifiedID,
		StopReason:  exec.Status,
		ToolsCalled: []string{fmt.Sprintf("sop.execute:%d", sopID)},
	}
	return out, nil
}

// firstParsableSOPID 取第一个可解析且非零的 SOP ID。
//
// `ai_agents.sop_ids` 是 text[]：运营手填就可能混进空串或非数字项。整批作废太脆
// （一个错字让这台智能体永久跑不动），逐项报错又超出本卡范围 —— 取第一个能用的，
// 并在调用方红因里点名"没有可用项"。
func firstParsableSOPID(ids []string) (uint, error) {
	for _, raw := range ids {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v == 0 {
			continue
		}
		return uint(v), nil
	}
	return 0, errors.New("全部为不可解析项")
}
