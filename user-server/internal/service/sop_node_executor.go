package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 节点执行结果状态
const (
	NodeStatusCompleted = "completed"
	NodeStatusWaiting   = "waiting"
	NodeStatusFailed    = "failed"
	NodeStatusSkipped   = "skipped"
)

// 节点事件类型（写入 sop_exec_events.event_type）
const (
	NodeEventStarted   = "started"
	NodeEventExecuted  = "executed"
	NodeEventCompleted = "completed"
	NodeEventFailed    = "failed"
	NodeEventWaiting   = "waiting"
	NodeEventRetried   = "retried"
)

// 等待事件类型（写入 sop_timers.wait_event 与 sop_executions.wait_event）
//
// 三档的共同点：**没有任何一档在进程内等**。等的是 `sop_timers` 里那一行 pending，
// 推进的是 outbox 轮询器（T-P3-02 起外加"审批落定即提前点火"这一路推送）。
const (
	WaitEventTimer         = "timer"
	WaitEventCustomerReply = "customer_reply"
	WaitEventExternal      = "external"

	// WaitEventApproval 等一次人工/策略裁决（T-P3-02 / N-4 挂起恢复）。
	// 与 timer 档的差别只有一个：等待对象是一条 approval_requests 记录，
	// 所以到期时刻**取自那条记录自己的 expires_at**（同一事实源，见 WaitExecutor），
	// 而不是节点配置里再写一个秒数 —— 两处各写一份 TTL，漂移时先骗到的是流程。
	WaitEventApproval = "approval"
)

// NodeExecutor 节点执行器接口（Strategy 模式）
//
// 每种 SOP 节点类型实现该接口，由 NodeExecutorRegistry 注册并分发。
// 实现方应保证 Execute 方法幂等（同一 ExecutionContext 多次执行结果一致），
// 重试由 SOPExecutionDispatcher 调度，执行器通过 SideEffects 列表标识已发生的副作用。
type NodeExecutor interface {
	NodeType() string

	Execute(ctx context.Context, execCtx *ExecutionContext) (*NodeExecResult, error)

	IsAsync() bool
}

// ExecutionContext 节点执行上下文
//
// 封装节点执行所需的所有上下文信息，避免执行器直接访问数据库。
// 由 SOPExecutionDispatcher 在派发任务时构造。
type ExecutionContext struct {
	Execution     *model.SOPExecution
	Node          *dto.SOPNode
	Graph         *dto.SOPGraph
	CustomerID    string
	SessionID     string
	Variant       string
	Input         model.JSONMap
	ExecutionData model.JSONMap
	TraceID       string
	StartedAt     time.Time
	Attempt       int

	// ApprovalOutcome 审批定时器点火后回读到的结论（T-P5-03）。
	//
	// 只有"这一条任务由 wait_event=approval 的定时器点火派生、且目标节点不是 wait 节点"
	// 时非空。为什么要分这一格而不是把结论并进 ExecutionData：wait 节点的既有语义是
	// "点火即推进、不重跑执行器"，并进 ExecutionData 对它够用；而外发节点必须重跑
	// （发送这一步在它自己身上），它需要的是"**这一次**重入是带着谁的结论回来的"。
	// 并进 ExecutionData 会让上游任何一条审批等待的结论被下游节点误读成自己的
	// （图里两条审批等待共用一格 = 第二条永远不需要批）。
	// 键值形状见 sop_approval_resume.go 的 approvalOutcomeFromRow。
	ApprovalOutcome model.JSONMap
}

// NodeExecResult 节点执行结果
type NodeExecResult struct {
	Status       string
	Output       model.JSONMap
	NextNodeID   string
	WaitUntil    *time.Time
	WaitEvent    string
	ErrorMessage string
	Retryable    bool
	SideEffects  []string
	TokensUsed   int
}

// NodeExecutorRegistry 节点执行器注册中心
//
// 全局唯一实例，启动时通过 Register 注册所有节点执行器。
// 调度器通过 MustGet 获取执行器，未注册类型返回 NoopExecutor 兜底。
type NodeExecutorRegistry struct {
	mu        sync.RWMutex
	executors map[string]NodeExecutor
}

// NewNodeExecutorRegistry 创建注册中心
func NewNodeExecutorRegistry() *NodeExecutorRegistry {
	return &NodeExecutorRegistry{
		executors: make(map[string]NodeExecutor),
	}
}

// Register 注册节点执行器
//
// 重复注册时 panic：这是设计契约（启动期 init 错乱属 fatal 错误，
// 必须立刻暴露而不是吞 error 后让 SOP 在运行时找不到节点类型）。
// 参见 TestNodeExecutorRegistry_DuplicateRegisterPanics。
func (r *NodeExecutorRegistry) Register(ctx context.Context, e NodeExecutor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[e.NodeType()]; exists {
		panic(fmt.Sprintf("node executor already registered: %s", e.NodeType()))
	}
	r.executors[e.NodeType()] = e
	return nil
}

// Get 获取节点执行器
func (r *NodeExecutorRegistry) Get(ctx context.Context, nodeType string) (NodeExecutor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[nodeType]
	if !ok {
		return nil, fmt.Errorf("node executor not found: %s", nodeType)
	}
	return e, nil
}

// MustGet 获取节点执行器，未注册时返回 NoopExecutor 兜底
//
// 兜底策略保证 SOP 流程不因未知节点类型中断，
// NoopExecutor 会记录 warn 日志并将节点标记为 completed 推进下一节点。
func (r *NodeExecutorRegistry) MustGet(ctx context.Context, nodeType string) NodeExecutor {
	e, err := r.Get(ctx, nodeType)
	if err != nil {
		logger.Warnf("node executor not found, using noop: %s", nodeType)
		return &NoopExecutor{nodeType: nodeType}
	}
	return e
}

// AllRegistered 返回所有已注册的节点类型（用于调试与启动校验）
func (r *NodeExecutorRegistry) AllRegistered(ctx context.Context) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.executors))
	for k := range r.executors {
		out = append(out, k)
	}
	return out
}

// AllExecutors 返回所有已注册的节点执行器（用于运行时依赖注入）
//
// 调用方应自行使用类型断言过滤关心的执行器类型（如 *MessageNodeBase）。
// 返回的切片在调用瞬间是注册中心的一份快照，后续注册/反注册不影响其内容。
func (r *NodeExecutorRegistry) AllExecutors(ctx context.Context) []NodeExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeExecutor, 0, len(r.executors))
	for _, e := range r.executors {
		out = append(out, e)
	}
	return out
}

// NoopExecutor 空操作执行器（兜底）
//
// 用于未注册的节点类型：记录 warn 日志，节点标记为 completed，
// 按默认 Next[0] 流转，保证 SOP 流程不中断。
type NoopExecutor struct {
	nodeType string
}

// NodeType 返回节点类型
func (n *NoopExecutor) NodeType() string { return n.nodeType }

// Execute 空操作：返回 completed
func (n *NoopExecutor) Execute(ctx context.Context, execCtx *ExecutionContext) (*NodeExecResult, error) {
	logger.Ctx(ctx).Warn().
		Str("node_type", n.nodeType).
		Str("node_id", execCtx.Node.ID).
		Str("execution_id", fmt.Sprintf("%d", execCtx.Execution.ID)).
		Msg("noop executor: node type not registered, skipping")
	return &NodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{},
	}, nil
}

// IsAsync 同步执行
func (n *NoopExecutor) IsAsync() bool { return false }

// CompensationNote 声明兜底路径无可撤销状态（见 CompensationNoter）。
//
// 注意这条"无可撤销"成立于**补偿时刻**而非执行时刻：Noop 代表节点类型未注册，
// 它自己确实什么都没做，但同一次运行里真发消息的节点若因注册缺失退化成 Noop，
// 出域副作用已经发生且这里无从得知。故 MustGet 的 warn 日志是这条路径唯一的现场线索。
func (n *NoopExecutor) CompensationNote() string {
	return "节点类型未注册（Noop 兜底）：执行期即空操作，补偿期同样无可撤销状态"
}

func hasSideEffect(exec *model.SOPExecution, effect string) bool {
	if exec == nil {
		return false
	}
	sideEffects := extractSideEffects(exec.ExecutionData)
	for _, e := range sideEffects {
		if e == effect {
			return true
		}
	}
	return false
}

func extractSideEffects(data model.JSONMap) []string {
	if data == nil {
		return nil
	}
	raw, ok := data["_side_effects"]
	if !ok {
		return nil
	}
	if arr, ok := raw.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	if arr, ok := raw.([]string); ok {
		out := make([]string, 0, len(arr))
		out = append(out, arr...)
		return out
	}
	return nil
}

func appendSideEffect(data model.JSONMap, effect string) model.JSONMap {
	if data == nil {
		data = model.JSONMap{}
	}
	existing := extractSideEffects(data)
	for _, e := range existing {
		if e == effect {
			return data
		}
	}
	existing = append(existing, effect)
	data["_side_effects"] = existing
	return data
}
