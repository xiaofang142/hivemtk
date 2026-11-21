package service

// workflow_orchestrator_stop_fence_test.go FA-WF-04：已确认的人工停止不得被
// 在途节点的完成/失败写覆盖（状态栅栏），停止本身用 CAS 原子迁移。
// 走真实测试库。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func fa04Setup(t *testing.T) (*WorkflowOrchestratorService, *WorkflowDispatcher, uint) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)
	orch := &WorkflowOrchestratorService{
		versionRepo: repository.NewWorkflowVersionRepository(database),
		execRepo:    repository.NewWorkflowExecutionRepository(database),
	}
	d := NewWorkflowDispatcher(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
		nil, nil,
	)
	exec := &model.WorkflowExecution{WorkflowID: "wf-fa04", Version: 1,
		Status: model.WorkflowExecRunning, Context: model.JSONMap{}}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("seed 执行失败: %v", err)
	}
	return orch, d, exec.ID
}

func TestStopExecution_CASMakesDoubleStopFail(t *testing.T) {
	orch, _, execID := fa04Setup(t)
	ctx := context.Background()

	if err := orch.StopExecution(ctx, execID); err != nil {
		t.Fatalf("首次停止应成功：%v", err)
	}
	if err := orch.StopExecution(ctx, execID); err == nil {
		t.Fatal("重复停止必须回报 ErrExecutionNotRunning")
	}
	var row model.WorkflowExecution
	if err := db.GetDB().First(&row, execID).Error; err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if row.Status != model.WorkflowExecTerminated || row.Error != "manual stopped" {
		t.Fatalf("停止原因必须保留，实际 status=%s error=%q", row.Status, row.Error)
	}
}

func TestConfirmedStopNotOverwrittenByCompletionOrFailure(t *testing.T) {
	orch, d, execID := fa04Setup(t)
	ctx := context.Background()

	// 人工确认停止成功后，dispatcher 手里还拿着旧内存对象（status=running）。
	if err := orch.StopExecution(ctx, execID); err != nil {
		t.Fatalf("停止失败：%v", err)
	}
	stale := &model.WorkflowExecution{ID: execID, WorkflowID: "wf-fa04", Version: 1, Status: model.WorkflowExecRunning}

	// 在途节点先完成：完成写必须被栅栏拦下，行保持 terminated + 停止原因。
	d.completeExecution(ctx, stale)
	var row model.WorkflowExecution
	db.GetDB().First(&row, execID)
	if row.Status != model.WorkflowExecTerminated {
		t.Fatalf("已确认停止不得被完成写改写，实际 %s", row.Status)
	}
	if row.Error != "manual stopped" {
		t.Fatalf("停止原因被清掉：error=%q", row.Error)
	}

	// 失败写同样被栅栏拦下。
	d.failExecution(ctx, stale, "boom")
	db.GetDB().First(&row, execID)
	if row.Status != model.WorkflowExecTerminated || row.Error != "manual stopped" {
		t.Fatalf("已确认停止不得被失败写改写，实际 status=%s error=%q", row.Status, row.Error)
	}
}

func TestCompleteAndFailStillWriteWhenRunning(t *testing.T) {
	_, d, execID := fa04Setup(t)
	ctx := context.Background()

	// 对照组：running 执行的正常完成/失败写不受栅栏影响。
	stale := &model.WorkflowExecution{ID: execID, WorkflowID: "wf-fa04", Version: 1, Status: model.WorkflowExecRunning}
	d.completeExecution(ctx, stale)
	var row model.WorkflowExecution
	db.GetDB().First(&row, execID)
	if row.Status != model.WorkflowExecCompleted {
		t.Fatalf("running 执行应正常完成，实际 %s", row.Status)
	}

	// 已 completed 后再 fail：栅栏只豁免 terminated，completed→failed 保持既有语义
	//（失败写会覆盖 completed——这是既有迁移图，不在本项收窄）。
	d.failExecution(ctx, stale, "late failure")
	db.GetDB().First(&row, execID)
	if row.Status != model.WorkflowExecFailed {
		t.Fatalf("completed→failed 既有迁移应保持，实际 %s", row.Status)
	}
}
