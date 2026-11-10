// workflow_update_test.go 工作流更新的响应口径。
//
// 回归背景（2026-11-10 双模式 API 验证）：PUT /api/geo/workflow/workflows/{id}
// 的响应体里 updated_at 是**更新前**的旧值，而紧接着 GET 同一 id 拿到的是新值。
// 根因是仓层用 map 直写 updated_at（绕过 GORM autoUpdateTime 回填），
// 服务层 CU 之后没有回填内存对象的 UpdatedAt。
// 与 SiteConfigService.Update / SchemaTemplateService.Update 同一口径：CU 后回填。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/repository"
)

func TestWorkflowService_UpdateRefreshesUpdatedAtInResponse(t *testing.T) {
	db := geoTestDB(t)
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepositoryWithDB(db),
		repository.NewGeoContentTaskRepositoryWithDB(db),
		nil,
	)
	ctx := context.Background()

	created, err := svc.Create(ctx, &dto.SaveWorkflowRequest{
		Name:    "更新口径回归",
		Steps:   []dto.WorkflowStep{{Name: "s1", Type: "noop"}},
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	before := created.UpdatedAt

	// 仓层时间精度是微秒，睡过一微秒保证新旧可比；PG now() 精度足够。
	time.Sleep(2 * time.Millisecond)

	updated, err := svc.Update(ctx, created.ID, &dto.SaveWorkflowRequest{
		Name:    "更新口径回归-改",
		Steps:   []dto.WorkflowStep{{Name: "s1", Type: "noop"}, {Name: "s2", Type: "noop"}},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	if !updated.UpdatedAt.After(before) {
		t.Errorf("PUT 响应里的 updated_at 未刷新：before=%v after=%v（回归：仓层 map 直写不回填内存对象）",
			before, updated.UpdatedAt)
	}

	// 与随后 GET 的口径必须一致（这是本用例的核心判据）
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.UpdatedAt.Sub(updated.UpdatedAt) > time.Second || updated.UpdatedAt.Sub(got.UpdatedAt) > time.Second {
		t.Errorf("PUT 响应 updated_at(%v) 与 GET 读回 updated_at(%v) 不一致",
			updated.UpdatedAt, got.UpdatedAt)
	}
}
