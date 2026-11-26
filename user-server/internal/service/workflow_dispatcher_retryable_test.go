package service

// workflow_dispatcher_retryable_test.go FA-WF-01：调度器尊重执行器的不可重试判定 +
// 重试预算按节点键控（前一节点耗掉的预算不再压给后一节点）。
//
// 走真实测试库 + 计数假执行器（进程内，无副作用外发）。

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// fa01Executor 计数假执行器：按脚本逐次返回失败（可控 Retryable）/错误/完成。
type fa01Executor struct {
	calls     atomic.Int32
	script    []fa01Outcome // 每次调用弹出一个；弹尽后返回完成
	fa01Retry bool          // 无脚本时的失败是否带 Retryable
}

type fa01Outcome struct {
	err        error // 非 nil = 执行器返回 error（瞬时路径）
	retryable  bool  // 与 failResult 搭配：failed 结果的 Retryable 判定
	failResult bool  // true = 返回 failed 结果（而非完成）
}

func (e *fa01Executor) NodeType() string { return "fa01_probe" }

func (e *fa01Executor) IsAsync() bool { return false }

func (e *fa01Executor) Execute(_ context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	n := e.calls.Add(1)
	idx := int(n) - 1
	if idx < len(e.script) {
		o := e.script[idx]
		if o.err != nil {
			return nil, o.err
		}
		if o.failResult { // 显式标记 = 返回 failed 结果（携带 Retryable 判定）
			return &WorkflowNodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: "scripted failure",
				Retryable:    o.retryable,
			}, nil
		}
		return &WorkflowNodeExecResult{Status: NodeStatusCompleted, NextNodeID: defaultNextWorkflowNodeID(wctx)}, nil
	}
	return &WorkflowNodeExecResult{Status: NodeStatusCompleted, NextNodeID: defaultNextWorkflowNodeID(wctx)}, nil
}

func fa01Policy() *WorkflowRetryPolicy {
	return &WorkflowRetryPolicy{MaxAttempts: 3, InitialBackoff: 5 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, Multiplier: 2}
}

func fa01Setup(t *testing.T, def map[string]any) (*WorkflowDispatcher, *model.WorkflowExecution, *fa01Executor) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)

	executor := &fa01Executor{}
	r := NewWorkflowNodeExecutorRegistry()
	RegisterWorkflowNodeExecutors(r, nil)
	if err := r.Register(context.Background(), executor); err != nil {
		t.Fatalf("注册假执行器失败: %v", err)
	}

	d := NewWorkflowDispatcher(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
		r, fa01Policy(),
	)

	v := &model.WorkflowVersion{WorkflowID: "wf-fa01", Version: 1,
		Definition: model.JSONMap(def), Status: model.WorkflowStatusPublished}
	if err := database.Create(v).Error; err != nil {
		t.Fatalf("seed version 失败: %v", err)
	}
	exec := &model.WorkflowExecution{WorkflowID: v.WorkflowID, Version: 1,
		Status: model.WorkflowExecRunning, Context: model.JSONMap{}}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("seed execution 失败: %v", err)
	}
	return d, exec, executor
}

// waitForTerminal 轮询执行至终态（重试协程异步，backoff 最短 5ms）。
func waitForTerminal(t *testing.T, execID uint) *model.WorkflowExecution {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := &model.WorkflowExecution{}
		if err := db.GetDB().First(got, execID).Error; err == nil &&
			got.Status != model.WorkflowExecRunning && got.Status != model.WorkflowExecWaiting {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := &model.WorkflowExecution{}
	db.GetDB().First(got, execID)
	return got
}

// 执行器显式 Retryable=false：仅执行一次，执行整体 failed，零重试。
func TestWorkflowDispatcher_NotRetryableFailsImmediately(t *testing.T) {
	def := map[string]any{"nodes": []any{
		map[string]any{"id": "n1", "type": "fa01_probe", "config": map[string]any{}},
	}, "edges": []any{}}
	d, exec, executor := fa01Setup(t, def)
	// retryable=false 标记 = 返回 failed 结果且 Retryable=false（显式不可重试判定）。
	executor.script = []fa01Outcome{{retryable: false, failResult: true}}

	d.runExecution(context.Background(), exec.ID, "fa01-nr")

	got := &model.WorkflowExecution{}
	db.GetDB().First(got, exec.ID)
	if got.Status != model.WorkflowExecFailed {
		t.Fatalf("不可重试失败必须整体 failed，实际 %s", got.Status)
	}
	if n := executor.calls.Load(); n != 1 {
		t.Fatalf("不可重试判定必须零重试（恰好 1 次执行），实际 %d", n)
	}
	if !strings.Contains(got.Error, "not retryable") {
		t.Errorf("失败原因应标明不可重试，实际 %q", got.Error)
	}
}

// 执行器错误（无显式判定）：按节点预算重试至成功；预算耗尽保持 failed。
func TestWorkflowDispatcher_TransientRetriesWithinNodeBudget(t *testing.T) {
	def := map[string]any{"nodes": []any{
		map[string]any{"id": "n1", "type": "fa01_probe", "config": map[string]any{}},
	}, "edges": []any{}}
	d, exec, executor := fa01Setup(t, def)
	executor.script = []fa01Outcome{
		{err: errors.New("transient-1")},
		{err: errors.New("transient-2")},
	}

	d.runExecution(context.Background(), exec.ID, "fa01-tr")
	got := waitForTerminal(t, exec.ID)
	if got.Status != model.WorkflowExecCompleted {
		t.Fatalf("瞬时失败在预算内应重试至完成，实际 %s（%s）", got.Status, got.Error)
	}
	if n := executor.calls.Load(); n != 3 {
		t.Fatalf("应恰好尝试 3 次（2 失败 + 1 成功），实际 %d", n)
	}
}

// 前一节点耗掉部分预算不影响后一节点的预算（节点级键控）。
func TestWorkflowDispatcher_NodeBudgetIsolatedPerNode(t *testing.T) {
	def := map[string]any{"nodes": []any{
		map[string]any{"id": "a", "type": "fa01_probe", "config": map[string]any{}},
		map[string]any{"id": "b", "type": "fa01_probe", "config": map[string]any{}},
	}, "edges": []any{
		map[string]any{"source": "a", "target": "b", "label": ""},
	}}
	d, exec, executor := fa01Setup(t, def)
	// a：失败一次后成功（耗 2 次）；b：同样失败一次后成功（应拿满自己的预算）。
	executor.script = []fa01Outcome{
		{err: errors.New("a-transient")},
		{}, // a 完成
		{err: errors.New("b-transient")},
		{}, // b 完成
	}

	d.runExecution(context.Background(), exec.ID, "fa01-iso")
	got := waitForTerminal(t, exec.ID)
	if got.Status != model.WorkflowExecCompleted {
		t.Fatalf("两节点各自在预算内重试应全部完成，实际 %s（%s）", got.Status, got.Error)
	}
	// 关键断言：节点成功后预算键收束——b 的重试在自己名下的键上计数并清零，
	// 且旧全局键已退役（不再作为跨节点预算载体）。
	if got.Context != nil {
		if _, exists := got.Context["_wf_attempt_a"]; exists {
			t.Errorf("节点 a 成功后预算键应清零，实际 %v", got.Context)
		}
		if _, exists := got.Context["_wf_attempt_b"]; exists {
			t.Errorf("节点 b 成功后预算键应清零，实际 %v", got.Context)
		}
		if _, exists := got.Context["_wf_attempt"]; exists {
			t.Errorf("旧全局预算键应退役，实际 %v", got.Context)
		}
	}
	// 注：中间失败的"尝试记录"（a/b 各一条 failed 行）是重试语义的预期产物；
	// 重试恢复链路中已完成节点可能被再次进入（恢复协议起点问题），
	// 属 FA-WF-02/06 的写点与恢复协议范围，不在本项断言。
}
