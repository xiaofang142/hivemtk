// crawler_tenant_test.go 爬虫访问记录的租户契约。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	gcvTenantA = uint(201)
	gcvTenantB = uint(202)
)

func gcvSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoCrawlerVisit{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoCrawlerVisitRepository_BulkCreateFillsTenantAndStatsScoped(t *testing.T) {
	db := gcvSetupDB(t)
	repo := &geoCrawlerVisitRepo{db: db}
	ctxA := tenant.WithTenantID(context.Background(), gcvTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gcvTenantB)

	if err := repo.BulkCreate(ctxA, []*model.GeoCrawlerVisit{
		{Keyword: "a", Engine: "GPTBot", Path: "/a", Source: model.CrawlerSourceActiveUAProbe},
		{Keyword: "a", Engine: "ClaudeBot", Path: "/a", Source: model.CrawlerSourceActiveUAProbe},
	}); err != nil {
		t.Fatalf("租户 A 批量写入失败: %v", err)
	}
	if err := repo.BulkCreate(ctxB, []*model.GeoCrawlerVisit{
		{Keyword: "b", Engine: "GPTBot", Path: "/b", Source: model.CrawlerSourceActiveUAProbe},
	}); err != nil {
		t.Fatalf("租户 B 批量写入失败: %v", err)
	}

	var total int64
	if err := db.WithContext(ctxA).Model(&model.GeoCrawlerVisit{}).Where("tenant_id = ?", gcvTenantA).Count(&total).Error; err != nil {
		t.Fatalf("核对租户归属失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("租户 A 应有 2 条访问，实际 %d", total)
	}

	visits, err := repo.TotalVisits(ctxA, 1)
	if err != nil {
		t.Fatalf("TotalVisits 失败: %v", err)
	}
	if visits != 2 {
		t.Fatalf("租户 A 总访问应为 2，实际 %d", visits)
	}

	rows, err := repo.StatsByKeyword(ctxA, 1)
	if err != nil {
		t.Fatalf("StatsByKeyword 失败: %v", err)
	}
	for _, row := range rows {
		if row.Keyword == "b" {
			t.Fatalf("租户 A 统计到他人关键词: %+v", rows)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("租户 A 应有 2 条关键词×引擎聚合，实际 %+v", rows)
	}
}
