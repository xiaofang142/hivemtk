package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/geo/repository"
)

// Opportunity 机会项：数据驱动的优先级行动清单（对标 elmo Opportunities）
type Opportunity struct {
	Type     string `json:"type"`     // sov_drop | cited_competitor | negative_alert | low_score_content
	Priority string `json:"priority"` // high | medium | low
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Metric   string `json:"metric,omitempty"`
}

// OpportunityService 机会清单服务：聚合 SOV 趋势、引用格局、负面告警、低分内容
type OpportunityService struct {
	analytics   *GeoDecisionAnalyticsService
	citeSvc     *CitationService
	alertRepo   repository.GeoAlertRepository
	articleRepo repository.GeoArticleRepository
	configRepo  repository.GeoConfigRepository
}

// NewOpportunityService 构造机会清单服务
func NewOpportunityService(
	analytics *GeoDecisionAnalyticsService,
	citeSvc *CitationService,
	alertRepo repository.GeoAlertRepository,
	articleRepo repository.GeoArticleRepository,
	configRepo repository.GeoConfigRepository,
) *OpportunityService {
	return &OpportunityService{analytics: analytics, citeSvc: citeSvc, alertRepo: alertRepo, articleRepo: articleRepo, configRepo: configRepo}
}

// sovPoint SOV 窗口快照（纯函数输入）
type sovPoint struct {
	OwnSOV      float64
	TopRival    string
	TopRivalSOV float64
}

// OpportunityInputs 机会构建输入（纯函数可测）
type OpportunityInputs struct {
	Cur, Prev        sovPoint
	HasSOVData       bool
	CitedDomains     []CitationDomainStat
	UnreadAlerts     int64
	RecentNegAlerts  []string
	LowScoreArticles []string
}

// rankOpportunities 优先级排序：high 先行，同级按类型稳定输出
func rankOpportunities(out []Opportunity) []Opportunity {
	weight := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(out, func(i, j int) bool { return weight[out[i].Priority] < weight[out[j].Priority] })
	return out
}

// BuildOpportunities 纯构建：输入聚合数据 → 优先级行动清单
func BuildOpportunities(in OpportunityInputs) []Opportunity {
	out := []Opportunity{}
	if in.HasSOVData {
		drop := in.Prev.OwnSOV - in.Cur.OwnSOV
		if drop >= 5 {
			out = append(out, Opportunity{
				Type: "sov_drop", Priority: "high",
				Title:  fmt.Sprintf("品牌 SOV 下滑 %.1f 个百分点", drop),
				Detail: "近7天相对前7天自有声量份额下降，优先补强高意图关键词内容并复查探针引擎健康度",
				Metric: fmt.Sprintf("%.1f%% -> %.1f%%", in.Prev.OwnSOV, in.Cur.OwnSOV),
			})
		}
		if in.Cur.TopRival != "" && in.Cur.TopRivalSOV > in.Cur.OwnSOV {
			out = append(out, Opportunity{
				Type: "sov_drop", Priority: "high",
				Title:  fmt.Sprintf("竞品 %s 声量反超", in.Cur.TopRival),
				Detail: "竞品在 AI 回答中的提及份额已超过自有品牌，建议针对其高被引主题生产对标内容",
				Metric: fmt.Sprintf("竞品 %.1f%% vs 自有 %.1f%%", in.Cur.TopRivalSOV, in.Cur.OwnSOV),
			})
		}
	}
	n := 0
	for _, d := range in.CitedDomains {
		if d.Category == "own" || n >= 5 {
			continue
		}
		n++
		out = append(out, Opportunity{
			Type: "cited_competitor", Priority: "medium",
			Title:  fmt.Sprintf("%s 域被高频引用（%d 次）", d.Domain, d.Count),
			Detail: "该域在探针回答中被引擎反复引用，建议拆解其内容结构并生产可替代的自有信源页",
			Metric: fmt.Sprintf("%s/%d queries/%d engines", d.Category, d.QueryCount, d.EngineCount),
		})
	}
	if in.UnreadAlerts > 0 {
		out = append(out, Opportunity{
			Type: "negative_alert", Priority: "high",
			Title:  fmt.Sprintf("%d 条未读负面告警待处理", in.UnreadAlerts),
			Detail: "负面监控命中尚未确认：" + strings.Join(in.RecentNegAlerts, "；"),
		})
	}
	for _, t := range in.LowScoreArticles {
		out = append(out, Opportunity{
			Type: "low_score_content", Priority: "medium",
			Title:  fmt.Sprintf("低分内容待优化：《%s》", t),
			Detail: "审计评分低于 60 分，按修复清单补强引用、数据与结构化要素后重新验证",
		})
		if len(out) >= 20 {
			break
		}
	}
	return rankOpportunities(out)
}

// List 生成机会清单
func (s *OpportunityService) List(ctx context.Context) ([]Opportunity, error) {
	now := time.Now()
	in := OpportunityInputs{}
	ownBrand := "HiveMTK"
	if s.configRepo != nil {
		if cfg, err := s.configRepo.Get(); err == nil {
			if bn := strings.TrimSpace(cfg.BrandName); bn != "" {
				ownBrand = bn
			}
		}
	}
	if s.analytics != nil {
		cur, err1 := s.analytics.GetShareOfVoiceBetween(ctx, "", now.AddDate(0, 0, -7), now)
		prev, err2 := s.analytics.GetShareOfVoiceBetween(ctx, "", now.AddDate(0, 0, -14), now.AddDate(0, 0, -7))
		if err1 == nil && err2 == nil {
			in.HasSOVData = true
			in.Cur, in.Prev = summarizeSOV(cur, ownBrand), summarizeSOV(prev, ownBrand)
		}
	}
	if s.citeSvc != nil {
		if res, err := s.citeSvc.DomainStats(ctx, 14); err == nil && res != nil {
			in.CitedDomains = res.Domains
		}
	}
	if s.alertRepo != nil {
		if n, err := s.alertRepo.CountUnread(ctx); err == nil {
			in.UnreadAlerts = n
		}
		if rows, _, err := s.alertRepo.List(ctx, "negative_monitor", "", 1, 5); err == nil {
			for _, a := range rows {
				q := strings.TrimSpace(a.Query)
				if q == "" {
					q = strings.TrimSpace(a.Snippet)
				}
				in.RecentNegAlerts = append(in.RecentNegAlerts, runeTruncate(q, 60))
			}
		}
	}
	if s.articleRepo != nil {
		if rows, _, err := s.articleRepo.GetList("", "", 1, 20); err == nil {
			for _, a := range rows {
				if a.Score > 0 && a.Score < 60 {
					in.LowScoreArticles = append(in.LowScoreArticles, runeTruncate(a.Title, 60))
				}
			}
		}
	}
	return BuildOpportunities(in), nil
}

// summarizeSOV 从 SOV 条目提炼自有份额与头号竞品（按品牌名精确匹配自有）
func summarizeSOV(entries []SOVEntry, ownBrand string) sovPoint {
	var p sovPoint
	own := strings.ToLower(strings.TrimSpace(ownBrand))
	rest := []SOVEntry{}
	for _, e := range entries {
		if strings.ToLower(strings.TrimSpace(e.Brand)) == own {
			p.OwnSOV = e.SOV
			continue
		}
		rest = append(rest, e)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].SOV > rest[j].SOV })
	if len(rest) > 0 {
		p.TopRival = rest[0].Brand
		p.TopRivalSOV = rest[0].SOV
	}
	return p
}
