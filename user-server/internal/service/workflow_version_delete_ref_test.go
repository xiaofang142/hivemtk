package service

// workflow_version_delete_ref_test.go FA-36：版本删除的执行引用保护。
//
// workflow_executions 只存 workflow_id+version（无外键），dispatcher 每次启动/
// 重试都按这两个值重载定义——被引用版本的硬删会让在途恢复失败、历史失去可重放
// 定义。钉住：在途（running/waiting）与历史（completed/failed）引用一律拒绝且
// 零删除；确实未引用的版本可删。走真实测试库。

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
	"hivemtk-user/internal/repository"
)

func fa36Setup(t *testing.T) *WorkflowOrchestratorService {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.WorkflowVersion{}, &model.WorkflowExecution{}, &model.WorkflowNodeExecution{}, &model.SystemUser{},
	)
	db.SetTestDB(database)
	return NewWorkflowOrchestratorService(
		repository.NewWorkflowVersionRepository(database),
		repository.NewWorkflowExecutionRepository(database),
		repository.NewWorkflowNodeExecutionRepository(database),
	)
}

func TestDeleteVersion_BlocksOnAnyExecutionReference(t *testing.T) {
	orch := fa36Setup(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		status string
	}{{"running", model.WorkflowExecRunning}, {"waiting", model.WorkflowExecWaiting}, {"completed", model.WorkflowExecCompleted}, {"failed", model.WorkflowExecFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			// 每个子用例独立版本（上一轮的执行引用会永久残留，不清理以免混淆断言）。
			ver := &model.WorkflowVersion{WorkflowID: "wf-fa36-" + tc.name, Version: 1,
				Definition: model.JSONMap{"nodes": []any{}}, Status: model.WorkflowStatusPublished}
			if err := db.GetDB().Create(ver).Error; err != nil {
				t.Fatalf("seed 版本失败：%v", err)
			}
			exec := &model.WorkflowExecution{WorkflowID: ver.WorkflowID, Version: ver.Version,
				Status: tc.status, StartedAt: time.Now(), Context: model.JSONMap{}}
			if err := db.GetDB().Create(exec).Error; err != nil {
				t.Fatalf("seed 执行失败：%v", err)
			}

			err := orch.DeleteVersion(ctx, ver.ID)
			if !errors.Is(err, ErrVersionReferenced) {
				t.Fatalf("%s 引用必须拒绝删除，实际 %v", tc.name, err)
			}
			var count int64
			db.GetDB().Model(&model.WorkflowVersion{}).Where("id = ?", ver.ID).Count(&count)
			if count != 1 {
				t.Fatalf("被拒删除不得产生实际删除，实际 version 行数 %d", count)
			}
		})
	}
}

func TestDeleteVersion_AllowsUnreferencedVersion(t *testing.T) {
	orch := fa36Setup(t)
	ver := &model.WorkflowVersion{WorkflowID: "wf-fa36-free", Version: 1,
		Definition: model.JSONMap{"nodes": []any{}}, Status: model.WorkflowStatusPublished}
	if err := db.GetDB().Create(ver).Error; err != nil {
		t.Fatalf("seed 版本失败：%v", err)
	}

	if err := orch.DeleteVersion(context.Background(), ver.ID); err != nil {
		t.Fatalf("无引用版本应可删除：%v", err)
	}
	var count int64
	db.GetDB().Model(&model.WorkflowVersion{}).Where("id = ?", ver.ID).Count(&count)
	if count != 0 {
		t.Fatalf("无引用版本删除后应不存在，实际 %d 行", count)
	}
}

func TestDeleteVersion_NotFoundDiagnostic(t *testing.T) {
	orch := fa36Setup(t)

	err := orch.DeleteVersion(context.Background(), 99999)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不存在的版本应回报 not found（0 行删除不冒充成功），实际 %v", err)
	}
}
