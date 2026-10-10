package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 1. WorkflowRetryPolicy Backoff 默认值
func TestWorkflowRetryPolicy_Backoff(t *testing.T) {
	p := DefaultWorkflowRetryPolicy()
	if d := p.Backoff(1); d != 1*time.Second {
		t.Errorf("Backoff(1)=%v want=1s", d)
	}
	if d := p.Backoff(2); d != 2*time.Second {
		t.Errorf("Backoff(2)=%v want=2s", d)
	}
	if d := p.Backoff(3); d != 4*time.Second {
		t.Errorf("Backoff(3)=%v want=4s", d)
	}
}

// 2. Backoff capped at MaxBackoff
func TestWorkflowRetryPolicy_BackoffCappedAtMax(t *testing.T) {
	p := &WorkflowRetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		Multiplier:     2.0,
	}
	if d := p.Backoff(10); d != 5*time.Second {
		t.Errorf("Backoff(10)=%v want=5s (capped)", d)
	}
}

// 3. Registry 注册 & 获取
func TestWorkflowExecutorRegistry_RegisterAndGet(t *testing.T) {
	r := NewWorkflowNodeExecutorRegistry()
	orch := &WorkflowOrchestratorService{}
	RegisterWorkflowNodeExecutors(r, orch)

	got, err := r.Get(context.Background(), WorkflowNodeTypeTrigger)
	if err != nil {
		t.Fatalf("Get(trigger) err: %v", err)
	}
	if got.NodeType() != WorkflowNodeTypeTrigger {
		t.Errorf("NodeType=%s want=trigger", got.NodeType())
	}

	all := r.AllRegistered(context.Background())
	if len(all) != 4 {
		t.Errorf("registered count=%d want=4 (got=%v)", len(all), all)
	}

	// 装配点漏传编排器时，subflow 节点会把"什么都没跑"记成 completed（见上一条用例的兜底分支），
	// 而注册处是唯一决定运行时那份实例的地方，所以在这里钉住它拿到的是非 nil。
	sub, err := r.Get(context.Background(), WorkflowNodeTypeSubflow)
	if err != nil {
		t.Fatalf("Get(subflow) err: %v", err)
	}
	sf, ok := sub.(*SubflowNodeExecutor)
	if !ok {
		t.Fatalf("subflow 执行器类型=%T，装配点换实现时这条判据得跟着改而不是删", sub)
	}
	if sf.orchestrator == nil {
		t.Errorf("注册进 registry 的 subflow 执行器没拿到编排器：编辑器里的「子流程」节点会静默跳过却标成已完成")
	}
}

// 4. MustGet 未注册时兜底 noop
func TestWorkflowExecutorRegistry_MustGet_NoopFallback(t *testing.T) {
	r := NewWorkflowNodeExecutorRegistry()
	noop := r.MustGet(context.Background(), "unknown_type")
	if _, ok := noop.(*WorkflowNoopExecutor); !ok {
		t.Errorf("expect WorkflowNoopExecutor fallback, got %T", noop)
	}
}

// 5. TriggerNodeExecutor 写入 _triggered_at 并合并 TriggerPayload
func TestTriggerNodeExecutor_CompletesWithTriggeredAt(t *testing.T) {
	exec := &model.WorkflowExecution{
		WorkflowID:     "wf-trigger",
		Version:        1,
		TriggerPayload: model.JSONMap{"order_id": float64(123)},
		Context:        model.JSONMap{},
	}
	wctx := &WorkflowExecContext{
		Execution: exec,
		NodeID:    "n1",
		NodeType:  WorkflowNodeTypeTrigger,
		Input:     model.JSONMap{"event": "click"},
	}
	res, err := (&TriggerNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Trigger Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=completed", res.Status)
	}
	if res.Output["event"] != "click" {
		t.Errorf("expect input merged, got %v", res.Output["event"])
	}
	if res.Output["order_id"] != float64(123) {
		t.Errorf("expect trigger payload merged, got %v", res.Output["order_id"])
	}
	if res.Output["_triggered_at"] == nil || res.Output["_triggered_at"] == "" {
		t.Errorf("expect _triggered_at set, got %v", res.Output["_triggered_at"])
	}
}

// 6. ActionNodeExecutor log 写入 message 并标记 side-effect
func TestActionNodeExecutor_LogWritesMessage(t *testing.T) {
	exec := &model.WorkflowExecution{
		WorkflowID: "wf-action",
		Version:    1,
		Context:    model.JSONMap{},
	}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "act1",
		NodeType:   WorkflowNodeTypeAction,
		NodeConfig: model.JSONMap{"action_type": "log", "message": "hello"},
		Input:      model.JSONMap{"order_id": float64(123)},
	}
	res, err := (&ActionNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Action Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=completed", res.Status)
	}
	if res.Output["message"] != "hello" {
		t.Errorf("message=%v want=hello", res.Output["message"])
	}
	if len(res.SideEffects) == 0 {
		t.Errorf("expect at least 1 side effect, got 0")
	}

	res2, _ := (&ActionNodeExecutor{}).Execute(context.Background(), wctx)
	if res2.Output["_already_executed"] != true {
		t.Errorf("expect idempotent _already_executed=true, got %v", res2.Output["_already_executed"])
	}
}

// 7. ConditionNodeExecutor 命中规则时跳转到 branch 目标
func TestConditionNodeExecutor_MatchedJumpToBranch(t *testing.T) {
	def := &WorkflowDefinition{
		Nodes: []WorkflowDefNode{
			{ID: "c1", Type: WorkflowNodeTypeCondition, Name: "c1"},
			{ID: "go_a", Type: WorkflowNodeTypeAction, Name: "A"},
			{ID: "go_b", Type: WorkflowNodeTypeAction, Name: "B"},
		},
		Edges: []WorkflowDefEdge{
			{Source: "c1", Target: "go_a", Label: "yes"},
			{Source: "c1", Target: "go_b", Label: "no"},
		},
	}
	exec := &model.WorkflowExecution{WorkflowID: "wf-cond", Version: 1, Context: model.JSONMap{"amount": float64(10)}}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "c1",
		NodeType:   WorkflowNodeTypeCondition,
		NodeConfig: model.JSONMap{"rules": []any{map[string]any{"field": "amount", "op": "gt", "value": float64(5), "branch": "yes"}}},
		Graph:      def,
		Context:    model.JSONMap{"amount": float64(10)},
	}
	res, err := (&ConditionNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Condition Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=completed", res.Status)
	}
	if res.NextNodeID != "go_a" {
		t.Errorf("NextNodeID=%s want=go_a", res.NextNodeID)
	}
}

// 8. ConditionNodeExecutor 无匹配规则走默认边
func TestConditionNodeExecutor_NoMatchFallsToDefault(t *testing.T) {
	def := &WorkflowDefinition{
		Nodes: []WorkflowDefNode{
			{ID: "c1", Type: WorkflowNodeTypeCondition},
			{ID: "default_next", Type: WorkflowNodeTypeAction},
		},
		Edges: []WorkflowDefEdge{{Source: "c1", Target: "default_next", Label: ""}},
	}
	exec := &model.WorkflowExecution{WorkflowID: "wf-cond2", Version: 1, Context: model.JSONMap{"amount": float64(1)}}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "c1",
		NodeType:   WorkflowNodeTypeCondition,
		NodeConfig: model.JSONMap{"rules": []any{map[string]any{"field": "amount", "op": "gt", "value": float64(100), "branch": "yes"}}},
		Graph:      def,
		Context:    model.JSONMap{"amount": float64(1)},
	}
	res, _ := (&ConditionNodeExecutor{}).Execute(context.Background(), wctx)
	if res.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=completed", res.Status)
	}
	if res.NextNodeID != "default_next" {
		t.Errorf("NextNodeID=%s want=default_next", res.NextNodeID)
	}
}

// 9. SubflowNodeExecutor 在没有编排器时走兜底分支：标成 completed、写 _subflow_invoked、
// 但仍什么都没跑。这条用例只验这个兜底形状（名字以前叫 InvokesSubflow，其实恰恰相反，
// 真调用在下一条用例里）。
func TestSubflowNodeExecutor_WithoutOrchestratorSkips(t *testing.T) {
	exec := &model.WorkflowExecution{WorkflowID: "wf-sub", Version: 1, Context: model.JSONMap{}}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     "sub1",
		NodeType:   WorkflowNodeTypeSubflow,
		NodeConfig: model.JSONMap{"sub_workflow_id": "sub-wf-2"},
	}
	res, err := (&SubflowNodeExecutor{}).Execute(context.Background(), wctx)
	if err != nil {
		t.Fatalf("Subflow Execute err: %v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=completed", res.Status)
	}
	if res.Output["_subflow_invoked"] != "sub-wf-2" {
		t.Errorf("_subflow_invoked=%v want=sub-wf-2", res.Output["_subflow_invoked"])
	}
	if res.Output["_skipped_reason"] == nil {
		t.Errorf("兜底分支必须自报没跑的原因，否则执行记录看上去就是一次真投递")
	}
	if len(res.SideEffects) == 0 {
		t.Errorf("expect side-effect key recorded")
	}
}

// 10. ParseWorkflowDefinition 解析合法 JSONMap
func TestParseWorkflowDefinition_Valid(t *testing.T) {
	def := &WorkflowDefinition{
		Nodes: []WorkflowDefNode{
			{ID: "n1", Type: WorkflowNodeTypeTrigger, Name: "start"},
			{ID: "n2", Type: WorkflowNodeTypeAction, Name: "act", Config: model.JSONMap{"action_type": "log"}},
		},
		Edges: []WorkflowDefEdge{{Source: "n1", Target: "n2", Label: "yes"}},
	}
	m := model.JSONMap{"nodes": []any{
		map[string]any{"id": "n1", "type": "trigger", "name": "start"},
		map[string]any{"id": "n2", "type": "action", "name": "act", "config": map[string]any{"action_type": "log"}},
	}, "edges": []any{
		map[string]any{"source": "n1", "target": "n2", "label": "yes"},
	}}
	parsed, err := ParseWorkflowDefinition(m)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if len(parsed.Nodes) != 2 || len(parsed.Edges) != 1 {
		t.Errorf("parsed nodes=%d edges=%d, want 2/1", len(parsed.Nodes), len(parsed.Edges))
	}
	if parsed.Edges[0].Source != "n1" || parsed.Edges[0].Target != "n2" {
		t.Errorf("edge=%+v want source=n1 target=n2", parsed.Edges[0])
	}

	if parsed.Edges[0].Label != def.Edges[0].Label {
		t.Errorf("Label=%s want=%s", parsed.Edges[0].Label, def.Edges[0].Label)
	}
}
