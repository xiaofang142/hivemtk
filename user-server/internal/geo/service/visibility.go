package service

import (
	"context"
	"sort"

	"hivemtk-user/internal/geo/repository"
)

// VisibilityService AI 可见性趋势分析服务
//
// 基于 geo_daily_stats 预聚合表输出每日可见率序列与环比变化，
// 对标 Peec AI / Otterly 的 Visibility Trend 能力。
type VisibilityService struct {
	dailyRepo repository.GeoDailyStatRepository
}

// NewVisibilityService 创建可见性趋势服务
func NewVisibilityService(dailyRepo repository.GeoDailyStatRepository) *VisibilityService {
	return &VisibilityService{dailyRepo: dailyRepo}
}

// TrendQuery 趋势查询参数
type TrendQuery struct {
	Engine string
	Intent string
	Days   int
}

// VisibilityTrendPoint 单日可见性指标
type VisibilityTrendPoint struct {
	Date          string  `json:"date"`
	ProbeCount    int     `json:"probe_count"`
	BrandHits     int     `json:"brand_hits"`
	Visibility    float64 `json:"visibility"`
	CitationCount int     `json:"citation_count"`
	NegativeCount int     `json:"negative_count"`
}

// VisibilityTrendResult 趋势序列 + 环比
type VisibilityTrendResult struct {
	Points         []VisibilityTrendPoint `json:"points"`
	CurrentAvg     float64                `json:"current_avg"`
	PreviousAvg    float64                `json:"previous_avg"`
	Change         float64                `json:"change"`
	ChangePct      float64                `json:"change_pct"`
	TotalProbes    int                    `json:"total_probes"`
	TotalBrandHits int                    `json:"total_brand_hits"`
}

// EngineCompareRow 引擎维度对比行（观测页「引擎对比」表）
type EngineCompareRow struct {
	Engine        string  `json:"engine"`
	ProbeCount    int     `json:"probe_count"`
	BrandHits     int     `json:"brand_hits"`
	Visibility    float64 `json:"visibility"`
	AvgCitations  float64 `json:"avg_citations"`
	NegativeCount int     `json:"negative_count"`
}

// DailyEngineBreakdown 单日按引擎拆分的品牌命中数（堆叠趋势图数据源）
type DailyEngineBreakdown struct {
	Date     string             `json:"date"`
	Probes   int                `json:"probes"`
	ByEngine map[string]float64 `json:"by_engine"`
}

// EngineCompareResult 引擎对比 + 全局概览 + 单日拆分
type EngineCompareResult struct {
	Summary VisibilityTrendResult  `json:"summary"`
	Engines []EngineCompareRow     `json:"engines"`
	Daily   []DailyEngineBreakdown `json:"daily"`
}

// GetTrend 可见性趋势 + 环比（周环比：各取 days/2 对半对比；不足 2 天无环比）
func (s *VisibilityService) GetTrend(ctx context.Context, q TrendQuery) (*VisibilityTrendResult, error) {
	if q.Days <= 0 || q.Days > 365 {
		q.Days = 30
	}
	stats, err := s.dailyRepo.GetTrend(ctx, q.Engine, "", q.Intent, q.Days)
	if err != nil {
		return nil, err
	}

	byDate := map[string]*VisibilityTrendPoint{}
	for _, st := range stats {
		p, ok := byDate[st.Date]
		if !ok {
			p = &VisibilityTrendPoint{Date: st.Date}
			byDate[st.Date] = p
		}
		p.ProbeCount += st.ProbeCount
		p.BrandHits += st.BrandMentionedCount
		p.CitationCount += st.CitationCount
		p.NegativeCount += st.NegativeCount
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	points := make([]VisibilityTrendPoint, 0, len(dates))
	for _, d := range dates {
		p := byDate[d]
		if p.ProbeCount == 0 {
			if p.BrandHits > 0 {
				p.ProbeCount = p.BrandHits
				p.Visibility = 1
			}
		} else {
			p.Visibility = float64(p.BrandHits) / float64(p.ProbeCount)
		}
		points = append(points, *p)
	}

	totalProbes, totalBrandHits := 0, 0
	for _, p := range points {
		totalProbes += p.ProbeCount
		totalBrandHits += p.BrandHits
	}

	res := &VisibilityTrendResult{Points: points, TotalProbes: totalProbes, TotalBrandHits: totalBrandHits}
	half := len(points) / 2
	if half > 0 {
		curSum, curN, prevSum, prevN := 0.0, 0.0, 0.0, 0.0
		for i, p := range points {
			if i < half {
				prevSum += p.Visibility
				prevN++
			} else {
				curSum += p.Visibility
				curN++
			}
		}
		if prevN > 0 {
			res.PreviousAvg = prevSum / prevN
		}
		if curN > 0 {
			res.CurrentAvg = curSum / curN
		}
		res.Change = res.CurrentAvg - res.PreviousAvg
		if res.PreviousAvg > 0 {
			res.ChangePct = res.Change / res.PreviousAvg
		}
	} else if len(points) > 0 {
		// 只有一个观测点时没有上一窗口可对比，当前可见率应等于整体可见率，
		// 否则前端概览卡会显示 0.0% 而与引擎对比表自相矛盾。
		if totalProbes > 0 {
			res.CurrentAvg = float64(totalBrandHits) / float64(totalProbes)
		}
		res.Change = 0
		res.ChangePct = 0
	}
	return res, nil
}

// GetEngineCompare 引擎维度对比：整体概览 + 每引擎可见率/负面/引用 + 单日拆分。
// 运营用它回答「哪个 AI 引擎看得见我、哪个引擎在说坏话、该优先补哪个引擎的内容」。
func (s *VisibilityService) GetEngineCompare(ctx context.Context, q TrendQuery) (*EngineCompareResult, error) {
	if q.Days <= 0 || q.Days > 365 {
		q.Days = 30
	}
	summary, err := s.GetTrend(ctx, q)
	if err != nil {
		return nil, err
	}

	// 全量（不带 engine 过滤）取回，按引擎聚合；intent 过滤保持一致
	allStats, err := s.dailyRepo.GetTrend(ctx, "", "", q.Intent, q.Days)
	if err != nil {
		return nil, err
	}

	type engineAgg struct {
		probes    int
		hits      int
		citations int
		negative  int
	}
	byEngine := map[string]*engineAgg{}
	for _, st := range allStats {
		if st.Engine == "" {
			continue
		}
		a, ok := byEngine[st.Engine]
		if !ok {
			a = &engineAgg{}
			byEngine[st.Engine] = a
		}
		a.probes += st.ProbeCount
		a.hits += st.BrandMentionedCount
		a.citations += st.CitationCount
		a.negative += st.NegativeCount
	}

	engines := make([]EngineCompareRow, 0, len(byEngine))
	for name, a := range byEngine {
		row := EngineCompareRow{
			Engine:        name,
			ProbeCount:    a.probes,
			BrandHits:     a.hits,
			NegativeCount: a.negative,
		}
		if a.probes > 0 {
			row.Visibility = float64(a.hits) / float64(a.probes)
			row.AvgCitations = float64(a.citations) / float64(a.probes)
		}
		engines = append(engines, row)
	}
	sort.Slice(engines, func(i, j int) bool {
		if engines[i].Visibility != engines[j].Visibility {
			return engines[i].Visibility > engines[j].Visibility
		}
		return engines[i].ProbeCount > engines[j].ProbeCount
	})

	// 单日拆分：日期 × 引擎 品牌命中数，供前端结合 probes 自行计算比率
	dateOrder := make([]string, 0, len(summary.Points))
	for _, p := range summary.Points {
		dateOrder = append(dateOrder, p.Date)
	}
	dateIdx := map[string]int{}
	for i, d := range dateOrder {
		dateIdx[d] = i
	}
	daily := make([]DailyEngineBreakdown, len(dateOrder))
	for i := range daily {
		daily[i] = DailyEngineBreakdown{Date: dateOrder[i], ByEngine: map[string]float64{}}
	}
	for _, st := range allStats {
		if q.Engine != "" && st.Engine != q.Engine {
			continue
		}
		idx, ok := dateIdx[st.Date]
		if !ok || st.Engine == "" || st.ProbeCount <= 0 {
			continue
		}
		daily[idx].Probes += st.ProbeCount
		daily[idx].ByEngine[st.Engine] += float64(st.BrandMentionedCount)
	}

	return &EngineCompareResult{
		Summary: *summary,
		Engines: engines,
		Daily:   daily,
	}, nil
}
