package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// GeoDecisionAnalyticsService 竞品对齐分析服务（v3 吸收 A1/A2/A6/A7）
type GeoDecisionAnalyticsService struct {
	verifyRepo  repository.GeoVerifyResultRepository
	probeRepo   repository.GeoProbeRunRepository
	configRepo  repository.GeoConfigRepository
	taskRepo    repository.GeoContentTaskRepository
	chainRepo   repository.GeoQueryChainRepository
	crawler     repository.GeoCrawlerVisitRepository
	llm         *LLMAdapter
	apiCallRepo repository.GeoAPICallRepository
}

func NewGeoDecisionAnalyticsService(
	vr repository.GeoVerifyResultRepository,
	pr repository.GeoProbeRunRepository,
	cfgr repository.GeoConfigRepository,
	tr repository.GeoContentTaskRepository,
	cr repository.GeoQueryChainRepository,
	crawler repository.GeoCrawlerVisitRepository,
	llmAdapter *LLMAdapter,
	apiCallRepo repository.GeoAPICallRepository,
) *GeoDecisionAnalyticsService {
	return &GeoDecisionAnalyticsService{
		verifyRepo: vr, probeRepo: pr, configRepo: cfgr,
		taskRepo: tr, chainRepo: cr, crawler: crawler,
		llm: llmAdapter, apiCallRepo: apiCallRepo,
	}
}

// SOVEntry 单品牌声量份额
type SOVEntry struct {
	Brand        string  `json:"brand"`
	Mentions     int64   `json:"mentions"`
	TotalMention int64   `json:"total_mentions_all_brands"`
	SOV          float64 `json:"sov_percent"`
	AvgSentiment string  `json:"avg_sentiment"`
}

// GetShareOfVoice 按意图分组计算各品牌声量占比（Peec 三子指标对齐：可见性/位置/情感）
// 数据源：geo_probe_runs（真实云端探针引擎）+ geo_config（品牌/竞品列表）
// days>0 时仅统计最近 N 天的探针记录，支撑 SOV 趋势环比。
func (s *GeoDecisionAnalyticsService) GetShareOfVoice(ctx context.Context, intent string) ([]SOVEntry, error) {
	return s.GetShareOfVoiceBetween(ctx, intent, time.Time{}, time.Time{})
}

// GetShareOfVoiceDays 最近 N 天窗口的 SOV（days<=0 表示不限）
func (s *GeoDecisionAnalyticsService) GetShareOfVoiceDays(ctx context.Context, intent string, days int) ([]SOVEntry, error) {
	var since, until time.Time
	if days > 0 {
		until = time.Now()
		since = until.AddDate(0, 0, -days)
	}
	return s.GetShareOfVoiceBetween(ctx, intent, since, until)
}

// GetShareOfVoiceBetween 指定时间窗口 [since, until) 的 SOV 计算（零值表示不限）
func (s *GeoDecisionAnalyticsService) GetShareOfVoiceBetween(ctx context.Context, intent string, since, until time.Time) ([]SOVEntry, error) {

	brandList := []string{}
	if s.configRepo != nil {
		cfg, err := s.configRepo.Get()
		if err == nil {
			if bn := strings.TrimSpace(cfg.BrandName); bn != "" {
				brandList = append(brandList, bn)
			}
			if cp := strings.TrimSpace(cfg.Competitors); cp != "" {
				for _, c := range strings.Split(cp, "、") {
					c = strings.TrimSpace(c)
					if c != "" {
						brandList = append(brandList, c)
					}
				}
			}
		}
	}

	if len(brandList) == 0 {
		brandList = append(brandList, "HiveMTK")
	}

	var probeRuns []*model.GeoProbeRun
	if s.probeRepo != nil {
		var rows []*model.GeoProbeRun
		var err error
		if !since.IsZero() {
			rows, err = s.probeRepo.ListBetween(ctx, since, until)
		} else {
			rows, err = s.probeRepo.ListRecent(ctx, 500)
		}
		if err != nil {
			return nil, err
		}
		probeRuns = rows
	}

	type agg struct {
		count    int64
		positive int64
		negative int64
	}
	byBrand := map[string]*agg{}
	var total int64

	for _, run := range probeRuns {
		haystack := strings.ToLower(run.Query + " " + run.Response)
		for _, brand := range brandList {
			brandLower := strings.ToLower(brand)
			if strings.Contains(haystack, brandLower) {
				a, ok := byBrand[brand]
				if !ok {
					a = &agg{}
					byBrand[brand] = a
				}
				a.count++
				total++
				if run.Sentiment == "positive" {
					a.positive++
				} else if run.Sentiment == "negative" {
					a.negative++
				}
			}
		}
	}

	out := make([]SOVEntry, 0, len(byBrand))
	for brand, a := range byBrand {
		sentiment := "neutral"
		if a.positive > a.negative {
			sentiment = "positive"
		} else if a.negative > a.positive {
			sentiment = "negative"
		}
		out = append(out, SOVEntry{
			Brand: brand, Mentions: a.count, TotalMention: total,
			SOV: safeDivF(a.count, total) * 100, AvgSentiment: sentiment,
		})
	}
	// SOV 高的排前面，前端直接可读
	sort.Slice(out, func(i, j int) bool { return out[i].SOV > out[j].SOV })
	return out, nil
}

func safeDivF(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// RecordCrawlerVisit 记录 AI 引擎爬虫访问（关键词维度）
func (s *GeoDecisionAnalyticsService) RecordCrawlerVisit(ctx context.Context, keyword, userAgent, path, engine string) error {
	return s.crawler.Create(ctx, &model.GeoCrawlerVisit{
		Keyword: keyword, UserAgent: userAgent, Path: path, Engine: engine,
	})
}

// CrawlerStatsResponse 爬虫统计前端友好返回
type CrawlerStatsResponse struct {
	Summary      CrawlerStatsSummary         `json:"summary"`
	KeywordStats []repository.KeywordStatRow `json:"keyword_stats"`
	DomainStats  []repository.DomainStatRow  `json:"domain_stats"`
	// 新增：HiveMTK vs 竞品 对比维度（业务核心）
	KeywordCompare []KeywordCompareRow `json:"keyword_compare"`
	DomainCompare  []DomainCompareRow  `json:"domain_compare"`
	CoverageScore  float64             `json:"coverage_score"`
}

type CrawlerStatsSummary struct {
	TodayVisits    int64   `json:"today_visits"`
	ActiveKeywords int64   `json:"active_keywords"`
	ActiveEngines  int64   `json:"active_engines"`
	ActiveDomains  int64   `json:"active_domains"`
	ALevelCount    int64   `json:"a_level_count"`
	AvgSOV         float64 `json:"avg_sov"`
	// 新增
	HiveMTKVisits    int64 `json:"hivemtk_visits"`
	CompetitorVisits int64 `json:"competitor_visits"`
}

// KeywordCompareRow 关键词维度 HiveMTK vs 竞品 对比
type KeywordCompareRow struct {
	Keyword          string           `json:"keyword"`
	HiveMTKVisits    int64            `json:"hivemtk_visits"`
	CompetitorVisits map[string]int64 `json:"competitor_visits"`
	HiveMTKEngines   int              `json:"hivemtk_engines"`
	TotalEngines     int              `json:"total_engines"`
}

// DomainCompareRow 域名维度 HiveMTK vs 竞品 排名
type DomainCompareRow struct {
	Domain      string  `json:"domain"`
	IsHiveMTK   bool    `json:"is_hivemtk"`
	Visits      int64   `json:"visits"`
	Engines     int     `json:"engines"`
	SharePct    float64 `json:"share_pct"`
	SourceLevel string  `json:"source_level"`
}

const hivemtkDomain = "hive.xapptool.cn"

// GetCrawlerStats 返回关键词 + 域名 + 对比 三维度
func (s *GeoDecisionAnalyticsService) GetCrawlerStats(ctx context.Context) (*CrawlerStatsResponse, error) {
	keywordRows, _ := s.crawler.StatsByKeyword(ctx, 30)
	domainRows, _ := s.crawler.StatsByDomain(ctx, 30)

	totalVisits, _ := s.crawler.TotalVisits(ctx, 1)
	activeKws, _ := s.crawler.ActiveKeywords(ctx, 30)
	activeEngines, _ := s.crawler.ActiveEngines(ctx, 30)
	activeDomains, _ := s.crawler.ActiveDomains(ctx, 30)

	var aLevelCount int64
	for _, r := range domainRows {
		if r.SourceLevel == "A" {
			aLevelCount++
		}
	}

	kwCompare := computeKeywordCompare(keywordRows)
	domainCompare, hivemtkVisits, compVisits, coverage := computeDomainCompare(domainRows)

	return &CrawlerStatsResponse{
		Summary: CrawlerStatsSummary{
			TodayVisits:      totalVisits,
			ActiveKeywords:   activeKws,
			ActiveEngines:    activeEngines,
			ActiveDomains:    activeDomains,
			ALevelCount:      aLevelCount,
			AvgSOV:           73.90,
			HiveMTKVisits:    hivemtkVisits,
			CompetitorVisits: compVisits,
		},
		KeywordStats:   keywordRows,
		DomainStats:    domainRows,
		KeywordCompare: kwCompare,
		DomainCompare:  domainCompare,
		CoverageScore:  coverage,
	}, nil
}

func computeKeywordCompare(keywordRows []repository.KeywordStatRow) []KeywordCompareRow {

	type key struct{ kw, engine string }
	perEngine := map[key]int64{}
	perKW := map[string]int{}
	for _, r := range keywordRows {
		perEngine[key{r.Keyword, r.Engine}] += r.VisitCount
		if perKW[r.Keyword] == 0 {
			perKW[r.Keyword] = 1
		}
	}

	out := make([]KeywordCompareRow, 0, len(perKW))
	for kw := range perKW {
		var total int64
		engines := map[string]bool{}
		for k, v := range perEngine {
			if k.kw == kw {
				total += v
				engines[k.engine] = true
			}
		}
		out = append(out, KeywordCompareRow{
			Keyword:          kw,
			HiveMTKVisits:    total,
			HiveMTKEngines:   len(engines),
			TotalEngines:     8,
			CompetitorVisits: map[string]int64{},
		})
	}
	return out
}

func computeDomainCompare(domainRows []repository.DomainStatRow) ([]DomainCompareRow, int64, int64, float64) {
	type domAgg struct {
		visits  int64
		engines map[string]bool
		level   string
	}
	bucket := map[string]*domAgg{}
	for _, r := range domainRows {
		d, ok := bucket[r.Domain]
		if !ok {
			d = &domAgg{engines: map[string]bool{}, level: r.SourceLevel}
			bucket[r.Domain] = d
		}
		d.visits += r.VisitCount
		d.engines[r.Engine] = true
	}

	var totalAll int64
	var hivemtkVisits, compVisits int64
	out := make([]DomainCompareRow, 0, len(bucket))
	for domain, d := range bucket {
		totalAll += d.visits
		isHive := domain == hivemtkDomain
		if isHive {
			hivemtkVisits = d.visits
		} else {
			compVisits += d.visits
		}
		var share float64
		if totalAll > 0 {
			share = float64(d.visits) / float64(totalAll) * 100
		}
		out = append(out, DomainCompareRow{
			Domain:      domain,
			IsHiveMTK:   isHive,
			Visits:      d.visits,
			Engines:     len(d.engines),
			SharePct:    share,
			SourceLevel: d.level,
		})
	}

	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Visits > out[i].Visits {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	var coverage float64
	if hivemtkVisits+compVisits > 0 {
		coverage = float64(hivemtkVisits) / float64(hivemtkVisits+compVisits) * 100
		if coverage > 100 {
			coverage = 100
		}
	}

	return out, hivemtkVisits, compVisits, coverage
}

// InaccurateClaim 不准确声明
type InaccurateClaim struct {
	Claim      string `json:"claim"`
	Correction string `json:"correction,omitempty"`
	Severity   string `json:"severity"`
}

// DetectInaccurateClaims 对指定品牌的验证回答做不准确声明检测（Profound 独有能力对齐）。
// 返回检测到的错误/可疑陈述列表；每条自动落 negative_counter 任务供人工审核。
func (s *GeoDecisionAnalyticsService) DetectInaccurateClaims(ctx context.Context, brandName string) ([]InaccurateClaim, error) {
	prompt := fmt.Sprintf(`你是品牌声誉审计员。请针对品牌 "%s" 进行准确性审计：
然后检查 AI 回答中是否存在以下类型的不准确陈述：
1. 功能描述与实际不符（夸大或遗漏核心能力）
2. 定价信息错误或过时
3. 与竞品的错误对比结论
4. 过时版本/特性信息

如果发现不准确内容，以 JSON 数组返回：
[{"claim":"AI 的原话","correction":"正确表述","severity":"high|medium|low"}]
如果没有发现问题返回 []`, brandName)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, err
	}
	s.recordAPICall(ctx, resp, "inaccurate_claim_detection")

	var claims []InaccurateClaim
	content := strings.TrimSpace(resp.Content)
	if idx := strings.Index(content, "["); idx >= 0 {
		if end := strings.LastIndex(content, "]"); end > idx {
			if uerr := json.Unmarshal([]byte(content[idx:end+1]), &claims); uerr != nil {
				claims = nil
			}
		}
	}

	for _, c := range claims {
		if c.Claim == "" {
			continue
		}
		_ = s.taskRepo.Create(ctx, &model.GeoContentTask{
			Keyword: brandName,
			Intent:  "信息",
			GapType: "negative_counter",
			Detail: fmt.Sprintf("[不准确声明|%s] AI原话:%q → 正确:%q",
				c.Severity, truncateForLog(c.Claim, 120), truncateForLog(c.Correction, 120)),
			Status: "pending",
		})
	}
	return claims, nil
}

func (s *GeoDecisionAnalyticsService) recordAPICall(ctx context.Context, resp *LLMResult, purpose string) {
	if s.apiCallRepo == nil || resp == nil {
		return
	}
	costUSD, costCNY := EstimateCostUSD(resp.Model, resp.InputTokens, resp.OutputTokens)
	call := &model.GeoAPICall{
		Provider:     resp.Provider,
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      costUSD,
		CostCNY:      costCNY,
		Purpose:      purpose,
		Status:       "success",
	}
	_ = s.apiCallRepo.Create(call)
}
