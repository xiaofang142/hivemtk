package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupWorkflowDeepService(t *testing.T) (*WorkflowOrchestratorService, *gorm.DB) {
	t.Helper()
	database := setupWorkflowServiceTestDB(t)
	if database == nil {
		return nil, nil
	}
	versionRepo := repository.NewWorkflowVersionRepository(database)
	execRepo := repository.NewWorkflowExecutionRepository(database)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(database)
	return NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo), database
}

// TestWO_CreateVersion_DuplicateWorkflowID 验证同一 workflow_id 可创建多个版本，version 自增
func TestWO_CreateVersion_DuplicateWorkflowID(t *testing.T) {
	svc, _ := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const wfID = "wo-deep-dup-wf"

	v1, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: wfID,
		Name:       "v1",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 v1 失败: %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("v1 期望 version=1，得到 %d", v1.Version)
	}
	if v1.Status != model.WorkflowStatusDraft {
		t.Errorf("v1 期望 status=draft，得到 %s", v1.Status)
	}

	v2, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: wfID,
		Name:       "v2",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 v2 失败: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("v2 期望 version=2，得到 %d", v2.Version)
	}

	v3, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: wfID,
		Name:       "v3",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 v3 失败: %v", err)
	}
	if v3.Version != 3 {
		t.Errorf("v3 期望 version=3，得到 %d", v3.Version)
	}

	list, err := svc.ListVersions(ctx, wfID)
	if err != nil {
		t.Fatalf("ListVersions 失败: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("期望 3 个版本，得到 %d", len(list))
	}
}

// TestWO_GetLatestPublished_NoPublished 未发布时返回 service.ErrNoPublishedVersion sentinel error
func TestWO_GetLatestPublished_NoPublished(t *testing.T) {
	svc, _ := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const wfID = "wo-deep-no-published"

	_, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: wfID,
		Name:       "draft only",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 draft 版本失败: %v", err)
	}

	_, err = svc.GetLatestPublished(ctx, wfID)
	if err == nil {
		t.Fatal("期望返回 error，但成功返回")
	}
	if !errors.Is(err, ErrNoPublishedVersion) {
		t.Errorf("期望 service.ErrNoPublishedVersion sentinel error，得到: %v", err)
	}
}

// TestWO_Execute_NoPublishedVersion 无已发布版本时 Execute 失败
func TestWO_Execute_NoPublishedVersion(t *testing.T) {
	svc, _ := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const wfID = "wo-deep-exec-no-published"

	_, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: wfID,
		Name:       "draft",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建 draft 版本失败: %v", err)
	}

	_, err = svc.Execute(ctx, &dto.WorkflowExecuteRequest{WorkflowID: wfID})
	if err == nil {
		t.Fatal("期望 Execute 失败，但成功返回")
	}
}

// TestWO_StopExecution_NotRunning 对已终止/已完成的执行 StopExecution 应返回 error
func TestWO_StopExecution_NotRunning(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	statuses := []string{
		model.WorkflowExecCompleted,
		model.WorkflowExecFailed,
		model.WorkflowExecTerminated,
	}

	for _, st := range statuses {
		exec := &model.WorkflowExecution{
			WorkflowID: "wo-deep-stop",
			Version:    1,
			Status:     st,
		}
		if err := database.Create(exec).Error; err != nil {
			t.Fatalf("创建执行记录(status=%s)失败: %v", st, err)
		}

		err := svc.StopExecution(ctx, exec.ID)
		if err == nil {
			t.Errorf("对 status=%s 的执行 StopExecution 期望返回 error，但成功", st)
		}

		var fresh model.WorkflowExecution
		database.First(&fresh, exec.ID)
		if fresh.Status != st {
			t.Errorf("status=%s 的执行不应被修改，但变成 %s", st, fresh.Status)
		}
	}
}

// TestWO_StopExecution_AlreadyTerminatedByPreviousStop 连续两次停止应第二次失败
func TestWO_StopExecution_AlreadyTerminatedByPreviousStop(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	exec := &model.WorkflowExecution{
		WorkflowID: "wo-deep-stop-twice",
		Version:    1,
		Status:     model.WorkflowExecRunning,
	}
	if err := database.Create(exec).Error; err != nil {
		t.Fatalf("创建执行记录失败: %v", err)
	}

	if err := svc.StopExecution(ctx, exec.ID); err != nil {
		t.Fatalf("首次 StopExecution 失败: %v", err)
	}

	if err := svc.StopExecution(ctx, exec.ID); err == nil {
		t.Error("已 terminated 的执行二次 StopExecution 期望返回 error，但成功")
	}
}

// TestWO_FindStuckExecutions 验证 FindStuck 查询条件：-running 状态
// - started_at < threshold 的应被查出
// - completed/failed/terminated 不应被查出
// - running 但 started_at > threshold 不应被查出
func TestWO_FindStuckExecutions(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	now := time.Now()
	stuckStarted := now.Add(-30 * time.Minute)
	freshStarted := now.Add(-1 * time.Minute)

	stuckRunning := &model.WorkflowExecution{
		WorkflowID: "wo-deep-stuck-stuck",
		Version:    1,
		Status:     model.WorkflowExecRunning,
		StartedAt:  stuckStarted,
	}
	notStuckRunning := &model.WorkflowExecution{
		WorkflowID: "wo-deep-stuck-fresh",
		Version:    1,
		Status:     model.WorkflowExecRunning,
		StartedAt:  freshStarted,
	}
	notStuckCompleted := &model.WorkflowExecution{
		WorkflowID: "wo-deep-stuck-done",
		Version:    1,
		Status:     model.WorkflowExecCompleted,
		StartedAt:  stuckStarted,
	}
	notStuckFailed := &model.WorkflowExecution{
		WorkflowID: "wo-deep-stuck-failed",
		Version:    1,
		Status:     model.WorkflowExecFailed,
		StartedAt:  stuckStarted,
	}
	for _, e := range []*model.WorkflowExecution{stuckRunning, notStuckRunning, notStuckCompleted, notStuckFailed} {
		if err := database.Create(e).Error; err != nil {
			t.Fatalf("创建执行记录失败: %v", err)
		}
	}

	threshold := now.Add(-10 * time.Minute)
	list, err := svc.FindStuckExecutions(ctx, threshold, 100)
	if err != nil {
		t.Fatalf("FindStuckExecutions 失败: %v", err)
	}

	var foundStuck bool
	for i := range list {
		if list[i].ID == stuckRunning.ID {
			foundStuck = true
		}
		if list[i].ID == notStuckRunning.ID {
			t.Error("fresh running(晚于阈值)不应被 FindStuck 命中")
		}
		if list[i].ID == notStuckCompleted.ID {
			t.Error("completed 不应被 FindStuck 命中")
		}
		if list[i].ID == notStuckFailed.ID {
			t.Error("failed 不应被 FindStuck 命中")
		}
	}
	if !foundStuck {
		t.Error("running 且 started_at 早于阈值的执行应被 FindStuck 命中")
	}
}

// TestWO_ListAll_PaginationBoundary 验证 page=0, pageSize=0, pageSize=201 的边界值
// repository 对 page<1 默认 1，pageSize<1 或 >200 默认 20，预期都不报错
func TestWO_ListAll_PaginationBoundary(t *testing.T) {
	svc, _ := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const wfID = "wo-deep-pagination"

	for i := 0; i < 25; i++ {
		if _, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
			WorkflowID: wfID,
			Name:       "p",
			Definition: workflowTestDefinition(),
		}); err != nil {
			t.Fatalf("创建版本 %d 失败: %v", i, err)
		}
	}

	cases := []struct {
		name             string
		page, pageSize   int
		expectListNonNil bool
	}{
		{"page=0/pageSize=10", 0, 10, true},
		{"page=1/pageSize=0", 1, 0, true},
		{"page=0/pageSize=0", 0, 0, true},
		{"page=1/pageSize=201", 1, 201, true},
		{"page=2/pageSize=5", 2, 5, true},
	}

	for _, c := range cases {
		list, total, err := svc.ListAll(ctx, "", "", "", c.page, c.pageSize)
		if err != nil {
			t.Errorf("case=%s ListAll 失败: %v", c.name, err)
			continue
		}
		if total < 25 {
			t.Errorf("case=%s 期望 total>=25，得到 %d", c.name, total)
		}
		if list == nil {
			t.Errorf("case=%s 期望 list 非 nil", c.name)
		}
	}
}

// TestWO_UpdateVersion_NotExist 更新不存在的版本应返回 error
func TestWO_UpdateVersion_NotExist(t *testing.T) {
	svc, _ := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	err := svc.UpdateVersion(ctx, 888888, &dto.WorkflowVersionUpdateRequest{
		Name:       "not exist",
		Definition: workflowTestDefinition(),
	})
	if err == nil {
		t.Fatal("更新不存在的版本期望返回 error，但成功")
	}
}

// TestWO_ArchiveVersion_StatusTransition draft→published→archived 状态转换验证
func TestWO_ArchiveVersion_StatusTransition(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	v, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wo-deep-transition",
		Name:       "transition",
		Definition: workflowTestDefinition(),
	})
	if err != nil {
		t.Fatalf("创建版本失败: %v", err)
	}
	if v.Status != model.WorkflowStatusDraft {
		t.Fatalf("初始状态期望 draft，得到 %s", v.Status)
	}

	if err := svc.PublishVersion(ctx, v.ID); err != nil {
		t.Fatalf("PublishVersion 失败: %v", err)
	}
	var pub model.WorkflowVersion
	database.First(&pub, v.ID)
	if pub.Status != model.WorkflowStatusPublished {
		t.Errorf("发布后期望 published，得到 %s", pub.Status)
	}

	if err := svc.ArchiveVersion(ctx, v.ID); err != nil {
		t.Fatalf("ArchiveVersion 失败: %v", err)
	}
	var arch model.WorkflowVersion
	database.First(&arch, v.ID)
	if arch.Status != model.WorkflowStatusArchived {
		t.Errorf("归档后期望 archived，得到 %s", arch.Status)
	}
}
