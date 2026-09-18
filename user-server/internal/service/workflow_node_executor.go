package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// WorkflowNodeExecutor 工作流节点执行器接口（Strategy 模式）
//
// 独立于 SOP 的 NodeExecutor，使用 WorkflowExecContext/WorkflowNodeExecResult
// 避免 SOP 与 Workflow 类型耦合。
type WorkflowNodeExecutor interface {
	NodeType() string
	Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error)
	IsAsync() bool
}

// WorkflowExecContext 工作流节点执行上下文（独立类型，不与 SOP 的 ExecutionContext 冲突）
type WorkflowExecContext struct {
	Execution  *model.WorkflowExecution
	NodeID     string
	NodeType   string
	NodeConfig model.JSONMap
	Graph      *WorkflowDefinition
	Input      model.JSONMap
	Context    model.JSONMap
	TraceID    string
	StartedAt  time.Time
	Attempt    int
}

// WorkflowNodeExecResult 工作流节点执行结果
type WorkflowNodeExecResult struct {
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

// WorkflowNodeExecutorRegistry 工作流节点执行器注册中心（并发安全）
type WorkflowNodeExecutorRegistry struct {
	mu        sync.RWMutex
	executors map[string]WorkflowNodeExecutor
}

func NewWorkflowNodeExecutorRegistry() *WorkflowNodeExecutorRegistry {
	return &WorkflowNodeExecutorRegistry{executors: make(map[string]WorkflowNodeExecutor)}
}

func (r *WorkflowNodeExecutorRegistry) Register(ctx context.Context, e WorkflowNodeExecutor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[e.NodeType()]; exists {
		panic(fmt.Sprintf("workflow node executor already registered: %s", e.NodeType()))
	}
	r.executors[e.NodeType()] = e
	return nil
}

func (r *WorkflowNodeExecutorRegistry) Get(ctx context.Context, nodeType string) (WorkflowNodeExecutor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[nodeType]
	if !ok {
		return nil, fmt.Errorf("workflow node executor not found: %s", nodeType)
	}
	return e, nil
}

// MustGet 获取执行器，未注册时返回 WorkflowNoopExecutor 兜底
func (r *WorkflowNodeExecutorRegistry) MustGet(ctx context.Context, nodeType string) WorkflowNodeExecutor {
	e, err := r.Get(context.Background(), nodeType)
	if err != nil {
		logger.Warnf("workflow node executor not found, using noop: %s", nodeType)
		return &WorkflowNoopExecutor{nodeType: nodeType}
	}
	return e
}

func (r *WorkflowNodeExecutorRegistry) AllRegistered(ctx context.Context) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.executors))
	for k := range r.executors {
		out = append(out, k)
	}
	return out
}

func (r *WorkflowNodeExecutorRegistry) AllExecutors(ctx context.Context) []WorkflowNodeExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkflowNodeExecutor, 0, len(r.executors))
	for _, e := range r.executors {
		out = append(out, e)
	}
	return out
}

// WorkflowNoopExecutor 工作流空操作执行器（兜底）
type WorkflowNoopExecutor struct {
	nodeType string
}

func (n *WorkflowNoopExecutor) NodeType() string { return n.nodeType }

func (n *WorkflowNoopExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	logger.Ctx(ctx).Warn().
		Str("node_type", n.nodeType).
		Str("node_id", wctx.NodeID).
		Msg("workflow noop executor: node type not registered, skipping")
	return &WorkflowNodeExecResult{
		Status: NodeStatusCompleted,
		Output: model.JSONMap{},
	}, nil
}

func (n *WorkflowNoopExecutor) IsAsync() bool { return false }

// WorkflowDefinition 工作流编排图（用于解析 version.definition JSONB）
type WorkflowDefinition struct {
	Nodes []WorkflowDefNode `json:"nodes"`
	Edges []WorkflowDefEdge `json:"edges"`
}

type WorkflowDefNode struct {
	ID     string        `json:"id"`
	Type   string        `json:"type"`
	Name   string        `json:"name"`
	Config model.JSONMap `json:"config"`
}

type WorkflowDefEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label"`
}

// FindWorkflowNode 按 id 在 def 中查找节点
func FindWorkflowNode(def *WorkflowDefinition, nodeID string) *WorkflowDefNode {
	if def == nil {
		return nil
	}
	for i := range def.Nodes {
		if def.Nodes[i].ID == nodeID {
			return &def.Nodes[i]
		}
	}
	return nil
}

// NextWorkflowNode 在 def 中查找以 currentID 为 source 且 label 匹配的边的 target 节点。
// branch 为空字符串时表示走默认边。
func NextWorkflowNode(def *WorkflowDefinition, currentID, branch string) *WorkflowDefNode {
	if def == nil {
		return nil
	}
	for _, e := range def.Edges {
		if e.Source != currentID {
			continue
		}
		if e.Label == branch {
			return FindWorkflowNode(def, e.Target)
		}
	}
	return nil
}

// ParseWorkflowDefinition 从 JSONMap 解析 WorkflowDefinition（使用 encoding/json 编解码）
func ParseWorkflowDefinition(m model.JSONMap) (*WorkflowDefinition, error) {
	if m == nil {
		return &WorkflowDefinition{}, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal definition failed: %w", err)
	}
	var def WorkflowDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("unmarshal definition failed: %w", err)
	}
	return &def, nil
}

// WorkflowSideEffectKey 生成副作用键
func WorkflowSideEffectKey(ctx *WorkflowExecContext, action string) string {
	if ctx == nil || ctx.Execution == nil {
		return fmt.Sprintf("wf_%s", action)
	}
	return fmt.Sprintf("wf_%d_%s_%s", ctx.Execution.ID, ctx.NodeID, action)
}

func extractWorkflowSideEffects(data model.JSONMap) []string {
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
		return append([]string(nil), arr...)
	}
	return nil
}

// HasWorkflowSideEffect 判断 ctx.Execution.Context 是否已存在指定副作用
func HasWorkflowSideEffect(ctx *WorkflowExecContext, effect string) bool {
	if ctx == nil || ctx.Execution == nil {
		return false
	}
	for _, e := range extractWorkflowSideEffects(ctx.Execution.Context) {
		if e == effect {
			return true
		}
	}
	return false
}

// AppendWorkflowSideEffect 追加副作用到 ctx.Execution.Context（去重）
func AppendWorkflowSideEffect(ctx *WorkflowExecContext, effect string) {
	if ctx == nil || ctx.Execution == nil {
		return
	}
	if ctx.Execution.Context == nil {
		ctx.Execution.Context = model.JSONMap{}
	}
	existing := extractWorkflowSideEffects(ctx.Execution.Context)
	for _, e := range existing {
		if e == effect {
			return
		}
	}
	existing = append(existing, effect)
	ctx.Execution.Context["_side_effects"] = existing
}

// RegisterWorkflowNodeExecutors 注册 4 种工作流节点执行器
func RegisterWorkflowNodeExecutors(registry *WorkflowNodeExecutorRegistry) {
	if registry == nil {
		return
	}
	ctx := context.Background()
	// Register 仅在重复注册时 panic，正常恒返回 nil；此处兜底记录任何意外错误。
	reg := func(e WorkflowNodeExecutor) {
		if err := registry.Register(ctx, e); err != nil {
			logger.GetLogger().Error().Err(err).Str("node_type", e.NodeType()).
				Msg("register workflow node executor failed")
		}
	}
	reg(&TriggerNodeExecutor{})
	reg(&ActionNodeExecutor{})
	reg(&ConditionNodeExecutor{})
	reg(&SubflowNodeExecutor{})
}
