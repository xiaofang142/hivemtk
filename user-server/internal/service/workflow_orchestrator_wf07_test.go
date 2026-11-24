package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// FA-WF-07：发布与草稿更新必须用状态/时间 CAS 串行化，旧草稿不得覆盖已发布定义。
func TestFAWF07_CASPreventsStaleDraftOverwrite(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	repo := repository.NewWorkflowVersionRepository(database)

	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf07-flow", Name: "草稿", Definition: workflowTestDefinition(),
	})
	if err != nil || created == nil {
		t.Fatalf("CreateVersion: %v %+v", err, created)
	}
	id := created.ID
	staleRevision := created.Revision

	// 用旧 Revision 直接调仓储 CAS：应 0 行（模拟发布前的陈旧读在发布后提交）。
	stale := *created
	stale.Name = "旧草稿覆盖"
	rows, err := repo.UpdateDraft(ctx, &stale, staleRevision)
	if err != nil || rows != 1 {
		t.Fatalf("首次 UpdateDraft 应成功 1 行: rows=%d err=%v", rows, err)
	}
	// 再次用同一旧 Revision：revision 已 +1，应 0 行。
	rows, err = repo.UpdateDraft(ctx, &stale, staleRevision)
	if err != nil || rows != 0 {
		t.Fatalf("陈旧 Revision UpdateDraft 应 0 行: rows=%d err=%v", rows, err)
	}
	// 发布（status CAS draft→published），应成功。
	rows, err = repo.TransitionStatus(ctx, id, model.WorkflowStatusDraft, model.WorkflowStatusPublished)
	if err != nil || rows != 1 {
		t.Fatalf("TransitionStatus(draft→published) 应 1 行: rows=%d err=%v", rows, err)
	}
	// 发布后再用旧 Revision 改草稿：应 0 行（status 与 revision 双重失配）。
	rows, err = repo.UpdateDraft(ctx, &stale, staleRevision)
	if err != nil || rows != 0 {
		t.Fatalf("发布后陈旧草稿覆盖应 0 行: rows=%d err=%v", rows, err)
	}
	// 发布态再发布：status CAS 失配 0 行。
	rows, err = repo.TransitionStatus(ctx, id, model.WorkflowStatusDraft, model.WorkflowStatusPublished)
	if err != nil || rows != 0 {
		t.Fatalf("重复发布应 0 行: rows=%d err=%v", rows, err)
	}
}

// 服务层：发布后提交不同 definition 应被拒（不可变快照）；
// 提交相同 definition 的元数据更新在 published 态经 revision CAS 成功（合法操作）。
// 真正要拦的是"读到 draft→并发发布→旧草稿整行覆盖"——由 UpdateDraft 的 status=draft CAS 保证，
// 见 TestFAWF07_CASPreventsStaleDraftOverwrite。
func TestFAWF07_ServiceRejectsStaleUpdateAfterPublish(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf07-flow2", Name: "草稿", Definition: workflowTestDefinition(),
	})
	if err != nil || created == nil {
		t.Fatalf("CreateVersion: %v %+v", err, created)
	}
	if err := svc.PublishVersion(ctx, created.ID); err != nil {
		t.Fatalf("PublishVersion: %v", err)
	}
	// 不同 definition：不可变快照守护。
	if err := svc.UpdateVersion(ctx, created.ID, &dto.WorkflowVersionUpdateRequest{
		Name: "x", Definition: model.JSONMap{"nodes": []any{}, "edges": []any{}},
	}); !errors.Is(err, ErrPublishedVersionImmutable) {
		t.Fatalf("发布后改 definition 应 ErrPublishedVersionImmutable，实际 %v", err)
	}
	// 相同 definition（内容一致）的元数据更新：合法，应成功（走 revision CAS）。
	if err := svc.UpdateVersion(ctx, created.ID, &dto.WorkflowVersionUpdateRequest{
		Name: "v1-meta", Description: "meta only", Definition: workflowTestDefinition(),
	}); err != nil {
		t.Fatalf("发布后同定义元数据更新应成功，实际 %v", err)
	}
	stored, err := svc.versionRepo.GetByID(ctx, created.ID)
	if err != nil || stored.Name != "v1-meta" {
		t.Fatalf("元数据应已更新: name=%v err=%v", stored.Name, err)
	}
}

// 两个并发更新（各自读到同一 old Revision）经屏障同步提交：恰一个成功，另一个 ErrVersionConflict。
func TestFAWF07_TwoConcurrentUpdatesOneConflict(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf07-flow3", Name: "草稿", Definition: workflowTestDefinition(),
	})
	if err != nil || created == nil {
		t.Fatalf("CreateVersion: %v %+v", err, created)
	}
	repo := repository.NewWorkflowVersionRepository(database)
	staleRevision := created.Revision // 初始 0：两 goroutine 用同一陈旧期望版本

	start := make(chan struct{})
	rowsOut := make([]int64, 2)
	errsOut := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			stale := *created
			stale.Name = string(rune('A' + i))
			rowsOut[i], errsOut[i] = repo.UpdateDraft(ctx, &stale, staleRevision)
		}(i)
	}
	close(start)
	wg.Wait()

	okCount, conflictCount := 0, 0
	for i := 0; i < 2; i++ {
		if errsOut[i] != nil {
			t.Fatalf("非预期错误: %v", errsOut[i])
		}
		switch {
		case rowsOut[i] == 1:
			okCount++
		case rowsOut[i] == 0:
			conflictCount++
		default:
			t.Fatalf("异常 rows=%d", rowsOut[i])
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("两相同 revision CAS 应恰 1 成功 1 落空, ok=%d conflict=%d", okCount, conflictCount)
	}
}

// 归档交错：发布后归档应成功；再次归档（status 已 archived）应 ErrVersionConflict。
func TestFAWF07_ArchiveTransitionCAS(t *testing.T) {
	svc, database := setupWorkflowDeepService(t)
	if svc == nil || database == nil {
		t.Skip("测试库不可用")
	}
	ctx := context.Background()
	created, err := svc.CreateVersion(ctx, &dto.WorkflowVersionCreateRequest{
		WorkflowID: "wf07-flow4", Name: "草稿", Definition: workflowTestDefinition(),
	})
	if err != nil || created == nil {
		t.Fatalf("CreateVersion: %v %+v", err, created)
	}
	if err := svc.ArchiveVersion(ctx, created.ID); err != nil {
		t.Fatalf("首次归档: %v", err)
	}
	if err := svc.ArchiveVersion(ctx, created.ID); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("重复归档应 ErrVersionConflict，实际 %v", err)
	}
}
