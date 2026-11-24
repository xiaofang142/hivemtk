package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupWorkflowServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDBOrSkip(t,
		&model.WorkflowVersion{},
		&model.WorkflowExecution{},
		&model.WorkflowNodeExecution{},
	)
	if database == nil {
		t.Skip("PostgreSQL 测试库不可达，跳过测试")
		return nil
	}
	return database
}

// workflowTestDefinition 满足服务端校验闸（FA-02）的最小合法定义：trigger→action(log)。
func workflowTestDefinition() map[string]any {
	return map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "type": "trigger", "name": "start", "config": map[string]any{}},
			map[string]any{"id": "n2", "type": "action", "name": "log step", "config": map[string]any{"action_type": "log", "message": "ok"}},
		},
		"edges": []any{
			map[string]any{"source": "n1", "target": "n2", "label": ""},
		},
	}
}

func TestWorkflowOrchestratorService_CreateVersion(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()
	req := &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-svc-test",
		Name:       "Service Test Version",
		Definition: workflowTestDefinition(),
		CreatedBy:  "svc-tester",
	}

	version, err := svc.CreateVersion(ctx, req)
	if err != nil {
		t.Fatalf("CreateVersion 失败: %v", err)
	}
	if version.Version != 1 {
		t.Errorf("期望版本号为 1，得到 %d", version.Version)
	}
	if version.Status != "draft" {
		t.Errorf("期望状态为 draft，得到 %s", version.Status)
	}
}

func TestWorkflowOrchestratorService_ListAll(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
			WorkflowID: "wf-listall",
			Name:       "ListAll Test",
			Definition: workflowTestDefinition(),
		})
	}

	list, total, err := svc.ListAll(ctx, "", "", "", 1, 10)
	if err != nil {
		t.Fatalf("ListAll 失败: %v", err)
	}
	if total < 5 {
		t.Errorf("期望至少 5 条记录，得到 %d", total)
	}
	if len(list) < 5 {
		t.Errorf("期望至少 5 条记录，得到 %d", len(list))
	}
}

func TestWorkflowOrchestratorService_FindStuckExecutions(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()

	exec := &model.WorkflowExecution{
		WorkflowID: "wf-stuck-test",
		Version:    1,
		Status:     "running",
		StartedAt:  time.Now().Add(-30 * time.Minute),
	}
	database.Create(exec)

	threshold := time.Now().Add(-10 * time.Minute)
	stuck, err := svc.FindStuckExecutions(ctx, threshold, 10)
	if err != nil {
		t.Fatalf("FindStuckExecutions 失败: %v", err)
	}
	if len(stuck) < 1 {
		t.Errorf("期望至少 1 条卡死记录，得到 %d", len(stuck))
	}
}

func TestWorkflowOrchestratorService_StopExecution_AlreadyCompleted(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()
	exec := &model.WorkflowExecution{
		WorkflowID: "wf-stop-completed",
		Version:    1,
		Status:     "completed",
	}
	database.Create(exec)

	err := svc.StopExecution(ctx, exec.ID)
	if err == nil {
		t.Error("期望返回错误，但成功了")
	}
}

func TestWorkflowOrchestratorService_Execute_WithoutPublishedVersion(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()
	req := &dto.WorkflowExecuteRequest{
		WorkflowID: "wf-no-published-version",
	}

	_, err := svc.Execute(ctx, req)
	if err == nil {
		t.Error("期望返回错误，但成功了")
	}
}

func TestWorkflowOrchestratorService_GetNodeExecutions(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()
	exec := &model.WorkflowExecution{
		WorkflowID: "wf-node-list",
		Version:    1,
		Status:     "running",
	}
	database.Create(exec)

	database.Create(&model.WorkflowNodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n1",
		NodeType:    "trigger",
		Status:      "completed",
	})
	database.Create(&model.WorkflowNodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n2",
		NodeType:    "action",
		Status:      "failed",
	})

	nodes, err := svc.GetNodeExecutions(ctx, exec.ID)
	if err != nil {
		t.Fatalf("GetNodeExecutions 失败: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("期望 2 条节点记录，得到 %d", len(nodes))
	}
}

func TestWorkflowOrchestratorService_DuplicateVersionCreation(t *testing.T) {
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	svc := NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	ctx := context.Background()

	v1, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-dup-test",
		Name:       "Version 1",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 v1 失败: %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("期望版本号 1，得到 %d", v1.Version)
	}

	v2, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf-dup-test",
		Name:       "Version 2",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 v2 失败: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("期望版本号 2，得到 %d", v2.Version)
	}

	t.Log("版本自增测试通过")
}
