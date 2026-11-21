package service

// workflow_persistence_unconfirmed_test.go FA-WF-02：持久化未确认不得报成功。
//
// 原缺陷：节点结果写与上下文/进度写的错误被丢弃（`_ =`）——隔离 PG 注入节点
// 结果 UPDATE 失败，实际执行 completed、节点行仍 running。现两处写失败 ⇒
// 执行以明确 failed 收口（带可诊断原因），绝不冒充成功。
//
// 注入方式：GORM update 回调在 UPDATE workflow_executions 的 dest 里探测到
// 标记键时注入错误（真实 DB 行为路径，不改产品代码）。走真实测试库。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// fa02Executor 输出可控标记的执行器：completed 且把 inject 键写进 Output
//（Output 会被合入 exec.Context，从而出现在上下文写的 dest 里）。
type fa02Executor struct {
	inject bool
}

func (e *fa02Executor) NodeType() string { return "fa02_probe" }

func (e *fa02Executor) IsAsync() bool { return false }

func (e *fa02Executor) Execute(_ context.Context, _ *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	out := model.JSONMap{}
	if e.inject {
		out["_fa02_inject"] = true
	}
	return &WorkflowNodeExecResult{Status: NodeStatusCompleted, Output: out}, nil
}

// registerFA02Injector 注册故障注入回调：UPDATE workflow_executions 的 dest
// map 含 _fa02_inject 时注入错误。
func registerFA02Injector(database *gorm.DB) {
	_ = database.Callback().Update().Before("gorm:update").Register("fa02_inject", func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "workflow_executions" {
			return
		}
		if m, ok := tx.Statement.Dest.(map[string]any); ok {
			if v, ok := m["context"].(model.JSONMap); ok {
				if _, hit := v["_fa02_inject"]; hit {
					_ = tx.AddError(errors.New("injected context write failure"))
				}
			}
		}
	})
}

func fa02Setup(t *testing.T, inject bool) (*WorkflowDispatcher, uint) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)
	registerFA02Injector(database)

	r := NewWorkflowNodeExecutorRegistry()
	RegisterWorkflowNodeExecutors(r, nil)
	if err := r.Register(context.Background(), &fa02Executor{inject: inject}); err != nil {
		t.Fatalf("注册执行器失败: %v", err)
	}

	d := NewWorkflowDispatcher(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
		r, fa01Policy(),
	)

	v := &model.WorkflowVersion{WorkflowID: "wf-fa02", Version: 1,
		Definition: model.JSONMap{"nodes": []any{
			map[string]any{"id": "n1", "type": "fa02_probe", "config": map[string]any{}},
		}, "edges": []any{}}, Status: model.WorkflowStatusPublished}
	if err := database.Create(v).Error; err != nil {
		t.Fatalf("seed version 失败: %v", err)
	}
	exec := &model.WorkflowExecution{WorkflowID: v.WorkflowID, Version: 1,
		Status: model.WorkflowExecRunning, Context: model.JSONMap{}}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("seed execution 失败: %v", err)
	}
	return d, exec.ID
}

// 上下文写失败：执行必须 failed（persistence unconfirmed），不得 completed。
func TestWorkflowDispatcher_ContextWriteFailureFailsExecution(t *testing.T) {
	d, execID := fa02Setup(t, true)
	d.runExecution(context.Background(), execID, "fa02-inject")

	got := waitForTerminal(t, execID)
	if got.Status != model.WorkflowExecFailed {
		t.Fatalf("持久化未确认必须 failed（不得报成功），实际 %s", got.Status)
	}
	if !strings.Contains(got.Error, "context write failed") {
		t.Fatalf("失败原因应可诊断（context write failed），实际 %q", got.Error)
	}
}

// 对照组：无注入标记时同一工作流正常完成。
func TestWorkflowDispatcher_NormalPathStillCompletes(t *testing.T) {
	d, execID := fa02Setup(t, false)
	d.runExecution(context.Background(), execID, "fa02-control")

	got := waitForTerminal(t, execID)
	if got.Status != model.WorkflowExecCompleted {
		t.Fatalf("无注入时执行应正常完成，实际 %s（%s）", got.Status, got.Error)
	}
}
