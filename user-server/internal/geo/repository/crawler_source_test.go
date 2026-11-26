package repository

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 来源不能从 UA/IP/关键词猜测；全部聚合只接受明确记录的主动探测，且按租户隔离。
func TestCrawlerSource_AllStatsExcludeUnattributedAndOtherTenant(t *testing.T) {
	database := gcvSetupDB(t)
	repo := NewGeoCrawlerVisitRepository(database)
	ctxA := tenant.WithTenantID(context.Background(), gcvTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gcvTenantB)
	var rows []*model.GeoCrawlerVisit
	if err := json.Unmarshal([]byte(`[
		{"keyword":"probe","engine":"GPTBot","path":"https://probe.example/page","source":"active_ua_probe"},
		{"keyword":"legacy","engine":"ClaudeBot","path":"https://legacy.example/page"},
		{"keyword":"observed","engine":"OtherBot","path":"https://observed.example/page","source":"ua_observation"}
	]`), &rows); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkCreate(ctxA, rows); err != nil {
		t.Fatal(err)
	}
	var other model.GeoCrawlerVisit
	_ = json.Unmarshal([]byte(`{"keyword":"other-tenant","engine":"ClaudeBot","path":"https://other.example/","source":"active_ua_probe"}`), &other)
	if err := repo.Create(ctxB, &other); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		fn   func(context.Context, int) (int64, error)
	}{{"总探测数", repo.TotalVisits}, {"站点数", repo.ActiveDomains}, {"关键词数", repo.ActiveKeywords}, {"引擎组数", repo.ActiveEngines}}
	for _, check := range checks {
		if n, err := check.fn(ctxA, 30); err != nil || n != 1 {
			t.Errorf("%s 只应含本租户 1 条明确探测，实际 %d，错误 %v", check.name, n, err)
		}
		if _, err := check.fn(context.Background(), 30); err == nil {
			t.Errorf("%s 缺租户必须 fail-closed", check.name)
		}
	}
	kw, err := repo.StatsByKeyword(ctxA, 30)
	if err != nil || len(kw) != 1 || kw[0].Keyword != "probe" || kw[0].VisitCount != 1 {
		t.Errorf("关键词聚合混入其他来源: %+v，错误 %v", kw, err)
	}
	domains, err := repo.StatsByDomain(ctxA, 30)
	if err != nil || len(domains) != 1 || domains[0].Domain != "probe.example" || domains[0].VisitCount != 1 {
		t.Errorf("域名聚合混入其他来源: %+v，错误 %v", domains, err)
	}
	if _, err := repo.StatsByKeyword(context.Background(), 30); err == nil {
		t.Error("关键词聚合缺租户必须 fail-closed")
	}
	if _, err := repo.StatsByDomain(context.Background(), 30); err == nil {
		t.Error("域名聚合缺租户必须 fail-closed")
	}
}

type crawlerBeforeSource struct {
	ID       uint `gorm:"primaryKey"`
	TenantID uint `gorm:"not null;default:1"`
	Keyword  string
}

func (crawlerBeforeSource) TableName() string { return "geo_crawler_visits" }

// 现有启动 AutoMigrate 两次执行后，存量行仍为 unknown，绝不推断其为主动探测或真实流量。
func TestCrawlerSource_AutoMigrateIsIdempotentAndPreservesUnknown(t *testing.T) {
	database := testutil.NewTestDB(t, &crawlerBeforeSource{})
	ctx := tenant.ContextWithDefault(context.Background())
	if err := database.WithContext(ctx).Create(&crawlerBeforeSource{Keyword: "old"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.GeoCrawlerVisit{}); err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.GeoCrawlerVisit{}); err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := database.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).Where("keyword = ?", "old").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row["source"] != "unknown" {
		t.Fatalf("旧记录必须保留未知来源，实际 %v", row["source"])
	}
}
