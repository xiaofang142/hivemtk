package service

// workflow_subflow_reconcile_test.go FA-WF-03：子流程占位标记与真实创建结果对账。
//
// 原缺陷：SubflowNodeExecutor 先写"已执行"标记再创建子流程——创建失败节点记
// failed，但标记已落库；重放见标记即返回 completed/already_executed，而子执行
// 数为 0（创建失败伪装成完成）。现：①父子链接落库（子执行 TriggerPayload 带
// _parent_exec_id/_parent_node_id）；②重放对账——按链接统计真实子执行，孤儿
// 标记清掉继续创建；③创建失败即清占位标记。走真实测试库。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func fa03Setup(t *testing.T) (*WorkflowOrchestratorService, *SubflowNodeExecutor, *model.WorkflowExecution) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)

	orch := NewWorkflowOrchestratorService(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
	)
	r := NewWorkflowNodeExecutorRegistry()
	RegisterWorkflowNodeExecutors(r, orch)
	orch.SetDispatcher(NewWorkflowDispatcher(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
		r, fa01Policy(),
	))
	executor := NewSubflowNodeExecutor(orch)

	// 真实存在的子工作流（published，单 trigger 节点）。
	child := &model.WorkflowVersion{WorkflowID: "wf-fa03-child", Version: 1,
		Definition: model.JSONMap{"nodes": []any{
			map[string]any{"id": "start", "type": "trigger", "config": map[string]any{}},
		}, "edges": []any{}}, Status: model.WorkflowStatusPublished}
	if err := database.Create(child).Error; err != nil {
		t.Fatalf("seed 子工作流失败: %v", err)
	}

	parent := &model.WorkflowExecution{WorkflowID: "wf-fa03-parent", Version: 1,
		Status: model.WorkflowExecRunning, Context: model.JSONMap{}}
	if err := database.Create(parent).Error; err != nil {
		t.Fatalf("seed 父执行失败: %v", err)
	}
	return orch, executor, parent
}

func fa03Wctx(parent *model.WorkflowExecution, subWorkflowID string) *WorkflowExecContext {
	return &WorkflowExecContext{
		Execution:  parent,
		NodeID:     "sub1",
		NodeType:   WorkflowNodeTypeSubflow,
		NodeConfig: map[string]any{"sub_workflow_id": subWorkflowID},
		Graph:      &WorkflowDefinition{},
	}
}

// 创建失败：节点 failed、占位标记被清（重放不再伪装 completed）。
func TestSubflowCreateFailureClearsPlaceholder(t *testing.T) {
	_, executor, parent := fa03Setup(t)
	ctx := context.Background()

	res, err := executor.Execute(ctx, fa03Wctx(parent, "wf-fa03-nonexistent"))
	if err != nil {
		t.Fatalf("Execute 不应返回 error：%v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Fatalf("子流程不存在应 failed，实际 %s", res.Status)
	}
	if HasWorkflowSideEffect(fa03Wctx(parent, "wf-fa03-nonexistent"), WorkflowSideEffectKey(fa03Wctx(parent, "wf-fa03-nonexistent"), "subflow:wf-fa03-nonexistent")) {
		t.Fatal("创建失败后占位标记必须被清除")
	}
	var children int64
	children, _ = orchExecRepoFA03().CountSubflowChildren(ctx, parent.ID, "sub1", "wf-fa03-nonexistent")
	if children != 0 {
		t.Fatalf("失败创建必须零子执行，实际 %d", children)
	}
}

// 孤儿标记对账：标记在、真实子执行为零 ⇒ 清标记继续创建，恰好 1 个子执行。
func TestSubflowOrphanMarkerReconcilesToRealCreation(t *testing.T) {
	orch, executor, parent := fa03Setup(t)
	ctx := context.Background()

	// 模拟"标记后崩溃"：手工把孤儿占位写进父执行 Context。
	marker := WorkflowSideEffectKey(fa03Wctx(parent, "wf-fa03-child"), "subflow:wf-fa03-child")
	parent.Context = model.JSONMap{"_side_effects": []any{marker}}
	if err := db.GetDB().Model(&model.WorkflowExecution{}).Where("id = ?", parent.ID).
		Update("context", parent.Context).Error; err != nil {
		t.Fatalf("预置孤儿标记失败：%v", err)
	}

	wctx := fa03Wctx(parent, "wf-fa03-child")
	if !HasWorkflowSideEffect(wctx, marker) {
		t.Fatal("前置条件：孤儿标记应在位")
	}

	res, err := executor.Execute(ctx, wctx)
	if err != nil {
		t.Fatalf("孤儿标记对账后应继续创建：%v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Fatalf("对账后创建应完成，实际 %s（output=%v）", res.Status, res.Output)
	}

	children, cerr := orch.execRepo.CountSubflowChildren(ctx, parent.ID, "sub1", "wf-fa03-child")
	if cerr != nil {
		t.Fatalf("统计子执行失败：%v", cerr)
	}
	if children != 1 {
		t.Fatalf("孤儿对账后应恰好创建 1 个子执行，实际 %d", children)
	}
	// 父子链接落库可查。
	var childExec model.WorkflowExecution
	if err := db.GetDB().Where("workflow_id = ? AND trigger_payload->>'_parent_exec_id' = ?",
		"wf-fa03-child", "1").First(&childExec).Error; err != nil {
		t.Fatalf("子执行应带父链接：%v", err)
	}
	if childExec.TriggerPayload == nil || childExec.TriggerPayload["_parent_node_id"] != "sub1" {
		t.Errorf("子执行 TriggerPayload 应含父节点标记，实际 %v", childExec.TriggerPayload)
	}
}

// 合法重放：标记在且有真实子执行 ⇒ 幂等返回，不再创建第二个。
func TestSubflowReplayWithRealChildIsIdempotent(t *testing.T) {
	orch, executor, parent := fa03Setup(t)
	ctx := context.Background()

	// 第一次真实创建。
	wctx := fa03Wctx(parent, "wf-fa03-child")
	if _, err := executor.Execute(ctx, wctx); err != nil {
		t.Fatalf("首次创建失败：%v", err)
	}
	// 父执行 Context（含标记）落库，模拟调度器写回后重放。
	if err := db.GetDB().Model(&model.WorkflowExecution{}).Where("id = ?", parent.ID).
		Update("context", parent.Context).Error; err != nil {
		t.Fatalf("落库 Context 失败：%v", err)
	}

	// 重放：加载落库后的父执行（标记在位）。
	var reloaded model.WorkflowExecution
	db.GetDB().First(&reloaded, parent.ID)
	res, err := executor.Execute(ctx, fa03Wctx(&reloaded, "wf-fa03-child"))
	if err != nil {
		t.Fatalf("重放失败：%v", err)
	}
	if res.Status != NodeStatusCompleted {
		t.Fatalf("重放应 completed，实际 %s", res.Status)
	}
	if already, _ := res.Output["_already_executed"].(bool); !already {
		t.Errorf("重放应走幂等分支，实际 output=%v", res.Output)
	}

	children, _ := orch.execRepo.CountSubflowChildren(ctx, parent.ID, "sub1", "wf-fa03-child")
	if children != 1 {
		t.Fatalf("合法重放不得重复创建，实际 %d 个子执行", children)
	}
}

func orchExecRepoFA03() *repository.WorkflowExecutionRepository {
	return repository.NewWorkflowExecutionRepository(db.GetDB())
}
