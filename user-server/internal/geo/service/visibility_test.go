package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
)

type memDailyRepo struct {
	stats []*model.GeoDailyStat
}

func (r *memDailyRepo) Upsert(_ context.Context, s *model.GeoDailyStat) error {
	r.stats = append(r.stats, s)
	return nil
}
func (r *memDailyRepo) BatchUpsert(_ context.Context, ss []*model.GeoDailyStat) error {
	r.stats = append(r.stats, ss...)
	return nil
}
func (r *memDailyRepo) List(_ context.Context, _, _, _, _ string, page, limit int) ([]*model.GeoDailyStat, int64, error) {
	return r.stats, int64(len(r.stats)), nil
}
func (r *memDailyRepo) GetTrend(_ context.Context, engine, _, _ string, _ int) ([]*model.GeoDailyStat, error) {
	var out []*model.GeoDailyStat
	for _, s := range r.stats {
		if engine == "" || s.Engine == engine {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *memDailyRepo) DeleteBefore(_ context.Context, _ time.Time) error {
	return nil
}

func TestVisibilityTrendAggregationAndCompare(t *testing.T) {
	repo := &memDailyRepo{stats: []*model.GeoDailyStat{

		{Date: "2026-09-01", Engine: "perplexity", Intent: "AI客服", ProbeCount: 4, BrandMentionedCount: 2},
		{Date: "2026-09-02", Engine: "perplexity", Intent: "AI客服", ProbeCount: 4, BrandMentionedCount: 2},
		{Date: "2026-09-03", Engine: "perplexity", Intent: "AI客服", ProbeCount: 4, BrandMentionedCount: 4},
		{Date: "2026-09-04", Engine: "perplexity", Intent: "AI客服", ProbeCount: 4, BrandMentionedCount: 4, CitationCount: 6},
	}}
	svc := NewVisibilityService(repo)
	res, err := svc.GetTrend(context.Background(), TrendQuery{Days: 30})
	if err != nil {
		t.Fatalf("GetTrend: %v", err)
	}
	if len(res.Points) != 4 {
		t.Fatalf("期望 4 个点，got %d", len(res.Points))
	}
	if res.TotalProbes != 16 || res.TotalBrandHits != 12 {
		t.Fatalf("总量异常: probes=%d hits=%d", res.TotalProbes, res.TotalBrandHits)
	}

	if res.Points[0].Visibility != 0.5 {
		t.Fatalf("首日可见率期望 0.5，got %v", res.Points[0].Visibility)
	}

	if res.PreviousAvg != 0.5 || res.CurrentAvg != 1.0 {
		t.Fatalf("环比异常: prev=%v cur=%v", res.PreviousAvg, res.CurrentAvg)
	}
	if res.Change <= 0.49 || res.Change >= 0.51 {
		t.Fatalf("change 期望 ≈0.5，got %v", res.Change)
	}
}

func TestVisibilityTrendEngineFilter(t *testing.T) {
	repo := &memDailyRepo{stats: []*model.GeoDailyStat{
		{Date: "2026-09-03", Engine: "perplexity", ProbeCount: 2, BrandMentionedCount: 1},
		{Date: "2026-09-03", Engine: "copilot", ProbeCount: 8, BrandMentionedCount: 0},
	}}
	svc := NewVisibilityService(repo)
	res, err := svc.GetTrend(context.Background(), TrendQuery{Engine: "copilot", Days: 7})
	if err != nil {
		t.Fatalf("GetTrend: %v", err)
	}
	if len(res.Points) != 1 || res.Points[0].ProbeCount != 8 {
		t.Fatalf("engine 过滤异常: points=%d", len(res.Points))
	}
	if res.Points[0].Visibility != 0 {
		t.Fatalf("copilot 可见率期望 0，got %v", res.Points[0].Visibility)
	}
}

func TestEngineCompareAggregation(t *testing.T) {
	repo := &memDailyRepo{stats: []*model.GeoDailyStat{
		{Date: "2026-09-03", Engine: "perplexity", Intent: "AI客服", ProbeCount: 4, BrandMentionedCount: 4, CitationCount: 8},
		{Date: "2026-09-03", Engine: "perplexity", Intent: "对比", ProbeCount: 2, BrandMentionedCount: 0},
		{Date: "2026-09-03", Engine: "copilot", Intent: "AI客服", ProbeCount: 10, BrandMentionedCount: 2, NegativeCount: 1},
	}}
	svc := NewVisibilityService(repo)
	res, err := svc.GetEngineCompare(context.Background(), TrendQuery{Days: 7})
	if err != nil {
		t.Fatalf("GetEngineCompare: %v", err)
	}
	if len(res.Engines) != 2 {
		t.Fatalf("期望 2 个引擎，got %d", len(res.Engines))
	}
	// perplexity: 4/6≈0.667 应排在 copilot 0.2 前
	if res.Engines[0].Engine != "perplexity" {
		t.Fatalf("期望 perplexity 排第一，got %s", res.Engines[0].Engine)
	}
	px := res.Engines[0]
	if px.ProbeCount != 6 || px.BrandHits != 4 {
		t.Fatalf("perplexity 聚合异常: probes=%d hits=%d", px.ProbeCount, px.BrandHits)
	}
	if px.AvgCitations < 1.32 || px.AvgCitations > 1.34 {
		t.Fatalf("perplexity 平均引用期望 ≈1.333，got %v", px.AvgCitations)
	}
	co := res.Engines[1]
	if co.Engine != "copilot" || co.NegativeCount != 1 {
		t.Fatalf("copilot 行异常: %+v", co)
	}
	if len(res.Daily) != 1 || res.Daily[0].Probes != 16 {
		t.Fatalf("单日拆分异常: %+v", res.Daily)
	}
}

func TestParseFanoutVariants(t *testing.T) {
	content := `前置文本 {"variants":[{"category":"direct","query":"HiveMTK 是什么？"},{"category":"compare","query":"HiveMTK 和探马SCRM 哪个好"},{"category":"negative","query":"HiveMTK 靠谱吗"},{"category":"","query":"重复问法"},{"query":"无类别默认direct"}],"extra":1} 尾部`
	variants, err := parseFanoutVariants(content)
	if err != nil {
		t.Fatalf("parseFanoutVariants: %v", err)
	}

	if len(variants) != 5 {
		t.Fatalf("期望 5 个变体，got %d: %+v", len(variants), variants)
	}
	if variants[0].Category != "direct" || variants[1].Category != "compare" || variants[2].Category != "negative" {
		t.Fatalf("类别解析异常: %+v", variants)
	}
	if variants[3].Category != "direct" || variants[4].Category != "direct" {
		t.Fatalf("空类别应降级 direct，got %+v", variants[3:])
	}
}

func TestParseFanoutVariantsPlainArray(t *testing.T) {
	content := `[{"category":"faq","query":"HiveMTK 怎么收费"}]`
	variants, err := parseFanoutVariants(content)
	if err != nil {
		t.Fatalf("parseFanoutVariants: %v", err)
	}
	if len(variants) != 1 || variants[0].Category != "faq" {
		t.Fatalf("裸数组解析异常: %+v", variants)
	}
}
