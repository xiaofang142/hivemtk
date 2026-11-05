// audit_test.go 审计仓储的租户与事务契约。
//
// 审计历史是租户表：同一 URL 在 A/B 两个租户各有一条独立历史；
// run 与 item 必须同事务落库，item 写失败时 run 不能留下“半条体检”。
package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	auditTenantA = uint(381)
	auditTenantB = uint(382)
)

func auditSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoAuditRun{}, &model.GeoAuditItem{})
	if db == nil {
		t.Fatal("测试库不可达：审计租户契约判不了")
	}
	return db
}

func newAuditRun(url, version string) *model.GeoAuditRun {
	return &model.GeoAuditRun{
		Kind:          model.GeoAuditKindSite,
		Scope:         model.GeoAuditScopePage,
		URL:           url,
		Brand:         "HiveMtk",
		RubricVersion: version,
		Score:         72,
		MaxScore:      100,
		Grade:         "良好",
		Categories:    datatypes.JSON([]byte(`[{"category":"robots"}]`)),
		Fixes:         datatypes.JSON([]byte(`[]`)),
		Summary:       "通过 20/25 项",
		Source:        model.GeoAuditSourceManual,
		PageCount:     1,
	}
}

func newAuditItems() []*model.GeoAuditItem {
	return []*model.GeoAuditItem{
		{Category: "robots", CategoryLabel: "AI 爬虫访问", Check: "ROBOTS-EXISTS", Score: 4, Max: 4, Pass: true, SortOrder: 0},
		{Category: "schema", CategoryLabel: "结构化数据", Check: "SCHEMA-ORG", Score: 0, Max: 4, Pass: false, Detail: "缺少 Organization", Fix: "补 Organization JSON-LD", SortOrder: 1},
	}
}

func TestGeoAuditRepository_RoundTripAndTenantScoped(t *testing.T) {
	db := auditSetupDB(t)
	repo := NewGeoAuditRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), auditTenantA)
	ctxB := tenant.WithTenantID(context.Background(), auditTenantB)

	run := newAuditRun("https://example.com/pricing", "v2")
	if err := repo.Create(ctxA, run, newAuditItems()); err != nil {
		t.Fatalf("创建审计失败: %v", err)
	}
	if run.ID == "" {
		t.Fatal("run ID 应由 BeforeCreate 生成")
	}

	gotRun, items, err := repo.GetByID(ctxA, run.ID)
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	if gotRun.Score != 72 || gotRun.RubricVersion != "v2" {
		t.Fatalf("run 字段漂移: %+v", gotRun)
	}
	if len(items) != 2 {
		t.Fatalf("item 数应为 2，实际 %d", len(items))
	}
	if items[0].Check != "ROBOTS-EXISTS" || items[0].RunID != run.ID {
		t.Fatalf("item 顺序或归属错误: %+v", items[0])
	}
	if items[0].TenantID != auditTenantA {
		t.Fatalf("item 未按 ctx 填租户: %d", items[0].TenantID)
	}

	// 跨租户不可见：GetByID 返回不存在。
	if _, _, err := repo.GetByID(ctxB, run.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户读取应报不存在，实际 %v", err)
	}
	listB, totalB, err := repo.List(ctxB, AuditRunQuery{Kind: model.GeoAuditKindSite})
	if err != nil {
		t.Fatalf("租户 B 列表失败: %v", err)
	}
	if totalB != 0 || len(listB) != 0 {
		t.Fatalf("租户 B 列表越界: total=%d len=%d", totalB, len(listB))
	}

	// 无租户 ctx 不允许写租户表。
	if err := repo.Create(context.Background(), newAuditRun("https://example.com/x", "v2"), nil); err == nil {
		t.Fatal("无租户 ctx 写入应失败，实际成功")
	}
}

func TestGeoAuditRepository_ListFiltersAndPrevious(t *testing.T) {
	db := auditSetupDB(t)
	repo := NewGeoAuditRepositoryWithDB(db)
	ctx := tenant.WithTenantID(context.Background(), auditTenantA)
	url := "https://example.com/blog"

	older := newAuditRun(url, "v2")
	older.CreatedAt = time.Now().Add(-48 * time.Hour).Truncate(time.Microsecond)
	older.Score = 60
	if err := repo.Create(ctx, older, newAuditItems()); err != nil {
		t.Fatalf("写入旧审计失败: %v", err)
	}

	newer := newAuditRun(url, "v2")
	newer.CreatedAt = time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond)
	newer.Score = 80
	if err := repo.Create(ctx, newer, newAuditItems()); err != nil {
		t.Fatalf("写入新审计失败: %v", err)
	}

	// 不同 rubric 版本不参与同 URL 的 previous 链。
	other := newAuditRun(url, "v3")
	other.CreatedAt = time.Now().Truncate(time.Microsecond)
	if err := repo.Create(ctx, other, newAuditItems()); err != nil {
		t.Fatalf("写入 v3 审计失败: %v", err)
	}

	list, total, err := repo.List(ctx, AuditRunQuery{URL: url, RubricVersion: "v2"})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("v2 历史应为 2 条，实际 total=%d len=%d", total, len(list))
	}
	if list[0].ID != newer.ID {
		t.Fatalf("列表应按时间倒序，首条=%s 期望=%s", list[0].ID, newer.ID)
	}

	prev, err := repo.Previous(ctx, newer)
	if err != nil {
		t.Fatalf("取上一次失败: %v", err)
	}
	if prev.ID != older.ID {
		t.Fatalf("previous 命中了错误版本或错误行: %s", prev.ID)
	}
	if _, err := repo.Previous(ctx, older); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("最早一条应无 previous，实际 %v", err)
	}

	latest, err := repo.LatestByURL(ctx, model.GeoAuditKindSite, url, "v2")
	if err != nil {
		t.Fatalf("取最新失败: %v", err)
	}
	if latest.ID != newer.ID {
		t.Fatalf("latest 应为 %s，实际 %s", newer.ID, latest.ID)
	}
}

func TestGeoAuditRepository_ItemFailureRollsBackRun(t *testing.T) {
	db := auditSetupDB(t)
	repo := NewGeoAuditRepositoryWithDB(db)
	ctx := tenant.WithTenantID(context.Background(), auditTenantA)

	run := newAuditRun("https://example.com/rollback", "v2")
	bad := newAuditItems()
	// check 列是 varchar(120)，500 字符必然被 PG 拒绝。
	bad[0].Check = strings.Repeat("x", 500)
	if err := repo.Create(ctx, run, bad); err == nil {
		t.Fatal("超长 check 应写入失败")
	}

	if _, total, err := repo.List(ctx, AuditRunQuery{URL: run.URL}); err != nil {
		t.Fatalf("回查失败: %v", err)
	} else if total != 0 {
		t.Fatalf("item 写失败后 run 未回滚，残留 %d 条", total)
	}
}
