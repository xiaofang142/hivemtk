package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// stubConfigRepo 只满足 GeoConfigRepository（Get/Update 两个方法），
// 用 err 字段注入"配置读不到"的降级路径。
type stubConfigRepo struct {
	cfg *model.GeoConfig
	err error
}

func (r *stubConfigRepo) Get() (*model.GeoConfig, error)  { return r.cfg, r.err }
func (r *stubConfigRepo) Update(_ *model.GeoConfig) error { return nil }

// stubProbeRepo 只喂 ListRecent（avg_sov 走零窗口 → ListRecent），
// 其余方法返回零值，被调用即暴露"口径漂移"。
type stubProbeRepo struct {
	runs []*model.GeoProbeRun
	err  error
}

func (r *stubProbeRepo) Create(_ context.Context, _ *model.GeoProbeRun) error { return nil }
func (r *stubProbeRepo) BatchCreate(_ context.Context, _ []*model.GeoProbeRun) error {
	return nil
}
func (r *stubProbeRepo) ListByEngine(_ context.Context, _ string, _, _ int) ([]*model.GeoProbeRun, int64, error) {
	return nil, 0, errors.New("stubProbeRepo.ListByEngine 未被 avg_sov 口径使用")
}
func (r *stubProbeRepo) ListRecent(_ context.Context, _ int) ([]*model.GeoProbeRun, error) {
	return r.runs, r.err
}
func (r *stubProbeRepo) ListSince(_ context.Context, _ time.Time, _ int) ([]*model.GeoProbeRun, error) {
	return nil, errors.New("stubProbeRepo.ListSince 未被 avg_sov 口径使用")
}
func (r *stubProbeRepo) ListBetween(_ context.Context, _, _ time.Time) ([]*model.GeoProbeRun, error) {
	return nil, errors.New("stubProbeRepo.ListBetween 未被 avg_sov 口径使用")
}
func (r *stubProbeRepo) ListByIntent(_ context.Context, _ string, _ time.Time) ([]*model.GeoProbeRun, error) {
	return nil, errors.New("stubProbeRepo.ListByIntent 未被 avg_sov 口径使用")
}
func (r *stubProbeRepo) DistinctEngines(_ context.Context) ([]string, error) { return nil, nil }

// stubCrawlerRepo 喂空聚合结果：GetCrawlerStats 里除 avg_sov 外的字段不是本用例的关注点。
type stubCrawlerRepo struct{}

func (stubCrawlerRepo) Create(_ context.Context, _ *model.GeoCrawlerVisit) error { return nil }
func (stubCrawlerRepo) BulkCreate(_ context.Context, _ []*model.GeoCrawlerVisit) error {
	return nil
}
func (stubCrawlerRepo) StatsByEngine(_ context.Context, _ int) (map[string]int64, error) {
	return nil, nil
}
func (stubCrawlerRepo) StatsByDomain(_ context.Context, _ int) ([]repository.DomainStatRow, error) {
	return nil, nil
}
func (stubCrawlerRepo) StatsByKeyword(_ context.Context, _ int) ([]repository.KeywordStatRow, error) {
	return nil, nil
}
func (stubCrawlerRepo) TotalVisits(_ context.Context, _ int) (int64, error) { return 0, nil }
func (stubCrawlerRepo) ActiveDomains(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (stubCrawlerRepo) ActiveKeywords(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (stubCrawlerRepo) ActiveEngines(_ context.Context, _ int) (int64, error) { return 0, nil }
func (stubCrawlerRepo) Clean(_ context.Context) error                         { return nil }

func probeRun(query, response, sentiment string) *model.GeoProbeRun {
	return &model.GeoProbeRun{Query: query, Response: response, Sentiment: sentiment}
}

// TestSelfBrandSOV_与GetShareOfVoice同口径 自家品牌占比必须等于 SOV 列表里同名的那一行，
// 且 numerically 等于「自家提及数 / 全部品牌提及数」。
func TestSelfBrandSOV_与GetShareOfVoice同口径(t *testing.T) {
	svc := &GeoDecisionAnalyticsService{
		configRepo: &stubConfigRepo{cfg: &model.GeoConfig{
			BrandName: "蜂巢", Competitors: "竞品A、竞品B",
		}},
		probeRepo: &stubProbeRepo{runs: []*model.GeoProbeRun{
			// 4 条探针、6 次品牌提及，其中"蜂巢"2 次 → 33.333…
			probeRun("蜂巢和竞品A哪个好", "蜂巢更合适，竞品B也可以", "positive"),
			probeRun("蜂巢怎么样", "未提及其他品牌", "neutral"),
			probeRun("竞品A怎么样", "竞品A表现不错", "neutral"),
			probeRun("竞品B如何", "竞品B一般", "negative"),
		}},
		crawler: stubCrawlerRepo{},
	}

	got := svc.selfBrandSOV(context.Background())
	const want = 33.333333
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("selfBrandSOV = %v，期望 ≈ %v", got, want)
	}

	entries, err := svc.GetShareOfVoice(context.Background(), "")
	if err != nil {
		t.Fatalf("GetShareOfVoice: %v", err)
	}
	var self *SOVEntry
	for i := range entries {
		if entries[i].Brand == "蜂巢" {
			self = &entries[i]
		}
	}
	if self == nil {
		t.Fatal("SOV 结果里没有自家品牌，同口径断言无法成立")
	}
	if self.SOV != got {
		t.Fatalf("avg_sov=%v 与 GET /geo/sov 的 %v 不同口径", got, self.SOV)
	}

	stats, err := svc.GetCrawlerStats(context.Background())
	if err != nil {
		t.Fatalf("GetCrawlerStats: %v", err)
	}
	if stats.Summary.AvgSOV != got {
		t.Fatalf("summary.avg_sov=%v，未取 selfBrandSOV=%v", stats.Summary.AvgSOV, got)
	}
}

// TestSelfBrandSOV_取不到时为0 每一条取不到数据的分支都退到 0，
// 不允许回落到曾经写死的 73.90。
//
// 每个用例自带探针行：能被变异打红的行必须真的"数据齐全、只差被考察的那道判空"，
// 否则变异后仍然返回 0，用例只是陪跑（判据无牙）。
func TestSelfBrandSOV_取不到时为0(t *testing.T) {
	// 探针行同时提到配置的"蜂巢"与下游兜底品牌名 "HiveMTK"：
	// 坏配置用例一旦被去掉判空，它会算出非零值，变异才有牙。
	runsSelf := []*model.GeoProbeRun{probeRun("蜂巢 HiveMTK 怎么样", "蜂巢、HiveMTK 都值得试", "positive")}
	// 自家未被提及的探针行：走到"品牌不在 SOV 结果里"的兜底
	runsRival := []*model.GeoProbeRun{probeRun("竞品A如何", "竞品A很好", "neutral")}
	brand := &model.GeoConfig{BrandName: "蜂巢"}

	newSvc := func(cfg *stubConfigRepo, runs []*model.GeoProbeRun, probeErr error) *GeoDecisionAnalyticsService {
		return &GeoDecisionAnalyticsService{
			configRepo: cfg,
			probeRepo:  &stubProbeRepo{runs: runs, err: probeErr},
			crawler:    stubCrawlerRepo{},
		}
	}

	cases := []struct {
		name string
		svc  *GeoDecisionAnalyticsService
	}{
		{"configRepo 为 nil", &GeoDecisionAnalyticsService{probeRepo: &stubProbeRepo{runs: runsSelf}, crawler: stubCrawlerRepo{}}},
		{"probeRepo 为 nil", newSvc(&stubConfigRepo{cfg: brand}, nil, nil)},
		// 品牌名必须正好是下游 brandList 兜底用的 "HiveMTK"：
		// 否则"配置读取失败"这格即使去掉 err 判空也算不出非零值，用例只会在变异存活后显得无辜。
		{"配置读取失败", newSvc(&stubConfigRepo{cfg: &model.GeoConfig{BrandName: "HiveMTK"}, err: errors.New("db down")}, runsSelf, nil)},
		{"配置行为 nil", newSvc(&stubConfigRepo{}, runsSelf, nil)},
		{"品牌名为空", newSvc(&stubConfigRepo{cfg: &model.GeoConfig{BrandName: "   ", Competitors: "竞品A"}}, runsSelf, nil)},
		{"探针读取失败", newSvc(&stubConfigRepo{cfg: brand}, runsSelf, errors.New("probe down"))},
		{"自家零提及", newSvc(&stubConfigRepo{cfg: brand}, runsRival, nil)},
	}

	for _, tc := range cases {
		if got := tc.svc.selfBrandSOV(context.Background()); got != 0 {
			t.Errorf("%s：selfBrandSOV = %v，期望 0", tc.name, got)
		}
		stats, err := tc.svc.GetCrawlerStats(context.Background())
		if err != nil {
			t.Fatalf("%s：GetCrawlerStats: %v", tc.name, err)
		}
		if stats.Summary.AvgSOV != 0 {
			t.Errorf("%s：summary.avg_sov = %v，期望 0（不得回落写死常量）", tc.name, stats.Summary.AvgSOV)
		}
	}
}
