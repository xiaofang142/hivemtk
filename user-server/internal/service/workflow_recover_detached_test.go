package service

// workflow_recover_detached_test.go FA-WF-06：受理与后台执行生命周期分离。
//
// 钉住的契约：
//  1. 受理后（执行已落库）请求 ctx 被取消，后台 Run 继续跑完——节点执行、
//     完成写都用脱钩后的 ctx（原实现首次读库即 canceled，执行永久停在 running）。
//  2. RecoverStuckExecutions 把卡死 running 用栅栏写标为明确 failed（可查询、
//     可重试的终态），不盲目重跑（无 per-node 副作用幂等前重跑=重复副作用）。
//
// 走真实测试库 + 阻塞假执行器控制取消时机。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// fa06BlockExecutor 第一个节点阻塞到 release 关闭，然后完成并推进到第二节点。
type fa06BlockExecutor struct {
	release chan struct{}
	started chan struct{}
}

func (e *fa06BlockExecutor) NodeType() string { return "fa06_block" }

func (e *fa06BlockExecutor) IsAsync() bool { return false }

func (e *fa06BlockExecutor) Execute(ctx context.Context, wctx *WorkflowExecContext) (*WorkflowNodeExecResult, error) {
	if wctx.NodeID == "n1" {
		closeOnce(e.started)
		select {
		case <-e.release:
		case <-ctx.Done():
			// 请求 ctx 被取消：原实现会在此丢掉整个执行。
			return nil, ctx.Err()
		}
	}
	return &WorkflowNodeExecResult{Status: NodeStatusCompleted, NextNodeID: defaultNextWorkflowNodeID(wctx)}, nil
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func fa06Setup(t *testing.T) (*WorkflowOrchestratorService, *fa06BlockExecutor, *model.WorkflowExecution) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)

	blocker := &fa06BlockExecutor{release: make(chan struct{}), started: make(chan struct{})}
	r := NewWorkflowNodeExecutorRegistry()
	RegisterWorkflowNodeExecutors(r, nil)
	if err := r.Register(context.Background(), blocker); err != nil {
		t.Fatalf("注册阻塞执行器失败: %v", err)
	}
	// n1 与 n2 都走阻塞执行器（n2 立即完成，但它的 DB 写用脱钩 ctx）。

	orch := NewWorkflowOrchestratorService(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
	)
	orch.SetDispatcher(NewWorkflowDispatcher(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
		r, fa01Policy(),
	))

	// Execute 需要一份已发布版本：n1（阻塞）→ n2（完成）。
	def := model.JSONMap{"nodes": []any{
		map[string]any{"id": "n1", "type": "fa06_block", "config": map[string]any{}},
		map[string]any{"id": "n2", "type": "fa06_block", "config": map[string]any{}},
	}, "edges": []any{
		map[string]any{"source": "n1", "target": "n2", "label": ""},
	}}
	v := &model.WorkflowVersion{WorkflowID: "wf-fa06", Version: 1,
		Definition: def, Status: model.WorkflowStatusPublished}
	if err := database.Create(v).Error; err != nil {
		t.Fatalf("seed version 失败: %v", err)
	}
	return orch, blocker, nil
}

// 受理后取消请求 ctx：执行必须照常跑完（节点执行与完成写不受取消影响）。
func TestWorkflowExecute_SurvivesRequestCancelAfterAccept(t *testing.T) {
	orch, blocker, _ := fa06Setup(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	var exec *model.WorkflowExecution
	go func() {
		e, err := orch.Execute(ctx, &dto.WorkflowExecuteRequest{WorkflowID: "wf-fa06"})
		exec = e
		done <- err
	}()

	<-blocker.started // n1 已开跑（Run 已过首读）
	cancel()          // 受理后取消请求 ctx（模拟客户端断开）
	close(blocker.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute 应随执行完成而返回，实际 err=%v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Execute 未在时限内返回")
	}
	_ = exec

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var row model.WorkflowExecution
		if err := db.GetDB().First(&row, exec.ID).Error; err == nil && row.Status == model.WorkflowExecCompleted {
			return // 脱钩生效：取消后仍跑完
		}
		time.Sleep(20 * time.Millisecond)
	}
	var row model.WorkflowExecution
	if err := db.GetDB().First(&row, exec.ID).Error; err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	t.Fatalf("受理后取消请求 ctx，执行必须照常完成（FA-WF-06 核心修复），实际 status=%s error=%q", row.Status, row.Error)
}

// 恢复扫描：卡死 running 用栅栏标为明确 failed；非 running 行不被触碰。
func TestRecoverStuckExecutions_MarksOnlyRunning(t *testing.T) {
	orch, _, _ := fa06Setup(t)
	ctx := context.Background()

	stale := &model.WorkflowExecution{WorkflowID: "wf-fa06", Version: 1,
		Status: model.WorkflowExecRunning, StartedAt: time.Now().Add(-time.Hour), Context: model.JSONMap{}}
	fresh := &model.WorkflowExecution{WorkflowID: "wf-fa06", Version: 1,
		Status: model.WorkflowExecRunning, StartedAt: time.Now(), Context: model.JSONMap{}}
	done := &model.WorkflowExecution{WorkflowID: "wf-fa06", Version: 1,
		Status: model.WorkflowExecCompleted, StartedAt: time.Now().Add(-time.Hour), Context: model.JSONMap{}}
	for _, e := range []*model.WorkflowExecution{stale, fresh, done} {
		if err := db.GetDB().Create(e).Error; err != nil {
			t.Fatalf("seed 失败：%v", err)
		}
	}

	marked, err := orch.RecoverStuckExecutions(ctx, time.Now().Add(-30*time.Minute), 50)
	if err != nil {
		t.Fatalf("恢复扫描失败：%v", err)
	}
	if marked != 1 {
		t.Fatalf("只有超阈 running 应被标记，实际 marked=%d", marked)
	}
	// 每次读取用新变量：gorm 会把非零 struct 字段当查询条件（复用 row 会带上
	// 上一次读取的 status/error 等，读到"上一行"的内容）。
	var staleRow model.WorkflowExecution
	db.GetDB().First(&staleRow, stale.ID)
	if staleRow.Status != model.WorkflowExecFailed || staleRow.Error == "" || !contains(staleRow.Error, "recovery") {
		t.Fatalf("卡死行应标为明确 failed 且带恢复原因，实际 status=%s error=%q", staleRow.Status, staleRow.Error)
	}
	var freshRow model.WorkflowExecution
	db.GetDB().First(&freshRow, fresh.ID)
	if freshRow.Status != model.WorkflowExecRunning {
		t.Errorf("未超阈 running 不得被标记，实际 %s", freshRow.Status)
	}
	var doneRow model.WorkflowExecution
	db.GetDB().First(&doneRow, done.ID)
	if doneRow.Status != model.WorkflowExecCompleted {
		t.Errorf("终态行不得被触碰，实际 %s", doneRow.Status)
	}
}
