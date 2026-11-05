// search_query_test.go GSC 性能数据仓储的租户与幂等契约。
//
// 同一天、同一 query/page 在不同租户下必须各存一行（row_key 不含租户，
// 唯一性由 tenant_id+row_key 复合索引保证）；重复导入只更新指标不新增行。
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	gsqTenantA = uint(391)
	gsqTenantB = uint(392)
)

func gsqSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoSearchQuery{})
	if db == nil {
		t.Fatal("测试库不可达：GSC 租户契约判不了")
	}
	return db
}

func gsqRow(date time.Time, query, page string, clicks, impressions int) *model.GeoSearchQuery {
	return &model.GeoSearchQuery{
		RowKey:      fmt.Sprintf("%s|%s|%s", date.Format("2006-01-02"), query, page),
		StatDate:    date,
		Query:       query,
		Page:        page,
		Clicks:      clicks,
		Impressions: impressions,
		CTR:         float64(clicks) / float64(impressions),
		Position:    6.5,
		Source:      "gsc_csv",
	}
}

func TestGeoSearchQueryRepository_TenantAndUpsert(t *testing.T) {
	db := gsqSetupDB(t)
	repo := NewGeoSearchQueryRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gsqTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gsqTenantB)
	day := time.Now().AddDate(0, 0, -1)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)

	if n, err := repo.UpsertBatch(ctxA, []*model.GeoSearchQuery{gsqRow(day, "geo 优化", "/geo", 120, 3400)}); err != nil || n != 1 {
		t.Fatalf("租户 A 首次导入失败: n=%d err=%v", n, err)
	}
	// 同一 row_key 在另一个租户下必须能独立存在。
	if _, err := repo.UpsertBatch(ctxB, []*model.GeoSearchQuery{gsqRow(day, "geo 优化", "/geo", 9, 100)}); err != nil {
		t.Fatalf("租户 B 同 row_key 写入失败（复合唯一索引缺失?）: %v", err)
	}

	// 重复导入：指标被更新，行数不增长。
	if _, err := repo.UpsertBatch(ctxA, []*model.GeoSearchQuery{gsqRow(day, "geo 优化", "/geo", 150, 4000)}); err != nil {
		t.Fatalf("租户 A 重复导入失败: %v", err)
	}
	summaryA, err := repo.Summary(ctxA, day, day)
	if err != nil {
		t.Fatalf("租户 A 汇总失败: %v", err)
	}
	if summaryA.RowCount != 1 || summaryA.Clicks != 150 || summaryA.Impressions != 4000 {
		t.Fatalf("租户 A 幂等导入结果不对: %+v", summaryA)
	}
	if summaryA.CTR <= 0 || summaryA.Position != 6.5 {
		t.Fatalf("租户 A 比率聚合不对: %+v", summaryA)
	}

	summaryB, err := repo.Summary(ctxB, day, day)
	if err != nil {
		t.Fatalf("租户 B 汇总失败: %v", err)
	}
	if summaryB.RowCount != 1 || summaryB.Clicks != 9 {
		t.Fatalf("租户 B 看到了别人的数据: %+v", summaryB)
	}
}

func TestGeoSearchQueryRepository_TopQueriesWindow(t *testing.T) {
	db := gsqSetupDB(t)
	repo := NewGeoSearchQueryRepositoryWithDB(db)
	ctx := tenant.WithTenantID(context.Background(), gsqTenantA)
	today := time.Now()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	old := today.AddDate(0, 0, -40)

	rows := []*model.GeoSearchQuery{
		gsqRow(today, "ai 客服", "/a", 80, 2100),
		gsqRow(today.AddDate(0, 0, -1), "ai 客服", "/b", 20, 300),
		gsqRow(today, "geo 优化", "/geo", 40, 900),
		gsqRow(old, "过期查询", "/old", 999, 9999),
	}
	if _, err := repo.UpsertBatch(ctx, rows); err != nil {
		t.Fatalf("批量导入失败: %v", err)
	}

	top, err := repo.TopQueries(ctx, today.AddDate(0, 0, -7), today, 10)
	if err != nil {
		t.Fatalf("TopQueries 失败: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("窗口外数据未过滤: %+v", top)
	}
	if top[0].Query != "ai 客服" || top[0].Clicks != 100 || top[0].Impressions != 2400 {
		t.Fatalf("按 query 聚合/排序不对: %+v", top[0])
	}
	if top[1].Query != "geo 优化" || top[1].Clicks != 40 {
		t.Fatalf("第二名不对: %+v", top[1])
	}
}
