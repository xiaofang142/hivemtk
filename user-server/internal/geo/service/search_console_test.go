// search_console_test.go GSC CSV 解析与双端报表口径。
//
// 解析要兼容 GSC 导出的两种表头；双端报表的 AI/Google 两栏分开计数，
// 不能把“品牌提及”和“点击”混成一个指标。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const scTenant = uint(393)

func scSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoSearchQuery{}, &model.GeoProbeRun{})
	if db == nil {
		t.Fatal("测试库不可达：双端报表契约判不了")
	}
	return db
}

func TestParseSearchConsoleCSV_UndatedQueries(t *testing.T) {
	csvText := "Top queries,Clicks,Impressions,CTR,Position\nGEO 优化,\"1,200\",34000,3.5%,8.2\nAI 客服,80,2100,3.8%,6.1\n"
	rows, errs, err := ParseSearchConsoleCSV(csvText)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(errs) != 0 || len(rows) != 2 {
		t.Fatalf("行数/错误数不对: rows=%d errs=%v", len(rows), errs)
	}
	if rows[0].Query != "GEO 优化" || rows[0].Clicks != 1200 || rows[0].Impressions != 34000 {
		t.Fatalf("千分位或列映射不对: %+v", rows[0])
	}
	if rows[0].CTR < 0.0349 || rows[0].CTR > 0.0351 || rows[0].Position != 8.2 {
		t.Fatalf("CTR/排名解析不对: %+v", rows[0])
	}
	if rows[0].Source != "gsc_csv_undated" {
		t.Fatalf("无日期行必须标注来源，实际 %q", rows[0].Source)
	}
}

func TestParseSearchConsoleCSV_DatedPages(t *testing.T) {
	csvText := "Date,Top pages,Clicks,Impressions,CTR,Average position\n2026-01-02,https://example.com/geo,12,300,4%,5.5\n"
	rows, _, err := ParseSearchConsoleCSV(csvText)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Query != "" || rows[0].Page == "" {
		t.Fatalf("Top pages 映射不对: %+v", rows)
	}
	if rows[0].Source != "gsc_csv" || rows[0].Date != "2026-01-02" {
		t.Fatalf("带日期行来源/日期不对: %+v", rows[0])
	}
}

func TestSearchConsoleService_ImportAndDualEngine(t *testing.T) {
	db := scSetupDB(t)
	ctx := tenant.WithTenantID(context.Background(), scTenant)
	searchRepo := repository.NewGeoSearchQueryRepositoryWithDB(db)
	probeRepo := repository.NewGeoProbeRunRepositoryWithDB(db)
	svc := NewSearchConsoleService(searchRepo, probeRepo, nil)

	rows, _, err := ParseSearchConsoleCSV("Top queries,Clicks,Impressions,CTR,Position\nGEO 优化,120,3400,3.5%,8.2\nAI 客服,80,2100,3.8%,6.1\n")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if imported, skipped, errs, err := svc.Import(ctx, rows); err != nil || imported != 2 || skipped != 0 || len(errs) != 0 {
		t.Fatalf("首次导入失败: imported=%d skipped=%d errs=%v err=%v", imported, skipped, errs, err)
	}
	// 同批重复导入是幂等 upsert，不新增行。
	if imported, _, _, err := svc.Import(ctx, rows); err != nil || imported != 2 {
		t.Fatalf("重复导入失败: imported=%d err=%v", imported, err)
	}

	if err := probeRepo.Create(ctx, &model.GeoProbeRun{
		Engine: "perplexity", Mode: ProbeModeReal, Query: "geo 优化",
		BrandMentioned: true,
		Citations:      datatypes.JSON([]byte(`[{"url":"https://a.example"},{"url":"https://b.example"}]`)),
	}); err != nil {
		t.Fatalf("写真实探针失败: %v", err)
	}
	if err := probeRepo.Create(ctx, &model.GeoProbeRun{
		Engine: "kimi", Mode: ProbeModeSimulated, Query: "geo 优化",
		BrandMentioned: true,
	}); err != nil {
		t.Fatalf("写模拟探针失败: %v", err)
	}
	if err := probeRepo.Create(ctx, &model.GeoProbeRun{
		Engine: "perplexity", Mode: ProbeModeManual, Query: "geo 优化",
	}); err != nil {
		t.Fatalf("写人工探针失败: %v", err)
	}

	report, err := svc.DualEngine(ctx, 30)
	if err != nil {
		t.Fatalf("双端报表失败: %v", err)
	}
	if report.AI.ProbeRuns != 3 || report.AI.BrandMentions != 2 || report.AI.CitationCount != 2 {
		t.Fatalf("AI 侧聚合不对: %+v", report.AI)
	}
	if report.AI.Modes[ProbeModeReal] != 1 || report.AI.Modes[ProbeModeSimulated] != 1 || report.AI.Modes[ProbeModeManual] != 1 {
		t.Fatalf("mode 分布不对: %+v", report.AI.Modes)
	}
	if len(report.AI.Engines) != 2 {
		t.Fatalf("引擎明细不对: %+v", report.AI.Engines)
	}

	if report.Google.Rows != 2 || report.Google.Clicks != 200 || report.Google.Impressions != 5500 {
		t.Fatalf("Google 侧汇总不对: %+v", report.Google)
	}
	if len(report.Google.TopQueries) != 2 || report.Google.TopQueries[0].Query != "GEO 优化" {
		t.Fatalf("Google TopQueries 不对: %+v", report.Google.TopQueries)
	}
	if report.Google.From == "" || report.Google.To == "" {
		t.Fatalf("Google 窗口缺失: %+v", report.Google)
	}
}

func TestSearchConsoleService_ImportSkipsInvalid(t *testing.T) {
	db := scSetupDB(t)
	ctx := tenant.WithTenantID(context.Background(), scTenant)
	svc := NewSearchConsoleService(repository.NewGeoSearchQueryRepositoryWithDB(db), nil, nil)

	imported, skipped, errs, err := svc.Import(ctx, []SearchConsoleRow{
		{},
		{Query: "正常", Clicks: 1, Impressions: 2},
		{Query: "负值", Clicks: -1, Impressions: 2},
		{Query: "坏日期", Date: "2026-13-99", Clicks: 1, Impressions: 2},
	})
	if err != nil {
		t.Fatalf("导入应部分成功: %v", err)
	}
	if imported != 1 || skipped != 3 || len(errs) != 3 {
		t.Fatalf("非法行未按预期跳过: imported=%d skipped=%d errs=%v", imported, skipped, errs)
	}
}
