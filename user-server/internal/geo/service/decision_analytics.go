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
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/reporting"
)

// GeoDecisionAnalyticsService 竞品对齐分析服务
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

// GetShareOfVoice 按意图分组计算各品牌声量占比（可见性/位置/情感三子指标）
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
		cfg, err := s.configRepo.Get(ctx)
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
	// SOV 高的排前面，前端直接可读；
	// 同分必须按品牌名兜底：out 来自 byBrand 迭代，只比 SOV 时并列品牌先后每轮随机。
	sort.Slice(out, func(i, j int) bool {
		if out[i].SOV != out[j].SOV {
			return out[i].SOV > out[j].SOV
		}
		return out[i].Brand < out[j].Brand
	})
	return out, nil
}

func safeDivF(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// ---- 内容类型归因 ----
//
// ContentFormat 之前只活在意图矩阵里（生成提示词用），没有读侧报表：
// "哪类内容带来了提及/引用"这一问没有答案。本节按 query 的意图分类，把
// 意图矩阵的 ContentFormat 挂回报表行，按内容类型分组统计提及率/引用率。
// 样本量必须同行给出——样本 <10 的分组读数不可作决策依据。

// ContentFormatAttributionRow 单内容类型（意图）的归因行。
type ContentFormatAttributionRow struct {
	Intent        string  `json:"intent"`         // 意图（疑问/对比/推荐/教程/评测/信息/未分类）
	ContentFormat string  `json:"content_format"` // 意图矩阵的内容格式（未分类意图为空）
	Queries       int64   `json:"queries"`        // 该意图下的查询数（样本量）
	Mentions      int64   `json:"mentions"`       // 品牌被提及的查询数
	MentionRate   float64 `json:"mention_rate"`   // mentions/queries（0~1）
	CitedQueries  int64   `json:"cited_queries"`  // 带引用的查询数
	CitationRate  float64 `json:"citation_rate"`  // cited/queries（0~1）
}

// ContentFormatAttributionReport 内容类型归因报告。
type ContentFormatAttributionReport struct {
	Days int                           `json:"days"`
	Rows []ContentFormatAttributionRow `json:"rows"`
}

// GetContentFormatAttribution 近 N 天按内容类型（意图）分组的提及率/引用率。
//
// 数据源：geo_query_chains（意图列 + CitedURLs）——它是唯一同时带 intent 与
// 引用记录的表；探针行没有 intent 列，join 不出来。样本量同行标注（Queries 列），
// SampleSizeHint 提示小样本分组不可下结论。
func (s *GeoDecisionAnalyticsService) GetContentFormatAttribution(ctx context.Context, days int) (*ContentFormatAttributionReport, error) {
	if days <= 0 || days > 90 {
		days = reporting.DefaultWindowDays(ctx)
	}
	if s.chainRepo == nil {
		return &ContentFormatAttributionReport{Days: days, Rows: []ContentFormatAttributionRow{}}, nil
	}
	rows, err := s.chainRepo.ListSince(ctx, time.Now().AddDate(0, 0, -days))
	if err != nil {
		return nil, err
	}
	return BuildContentFormatAttribution(days, rows), nil
}

// ContentFormatAttributionSampleFloor 样本量下限提示阈值：低于它的分组读数不可下结论。
const ContentFormatAttributionSampleFloor = 10

// BuildContentFormatAttribution 纯聚合：按内容类型分组统计提及率/引用率，样本量标注。
func BuildContentFormatAttribution(days int, chains []*model.GeoQueryChain) *ContentFormatAttributionReport {
	ctx := context.Background()
	type agg struct {
		queries  int64
		mentions int64
		cited    int64
	}
	byIntent := map[string]*agg{}
	for _, c := range chains {
		if c == nil {
			continue
		}
		intent := c.Intent
		if strings.TrimSpace(intent) == "" {
			intent = "未分类"
		}
		a, ok := byIntent[intent]
		if !ok {
			a = &agg{}
			byIntent[intent] = a
		}
		a.queries++
		if c.BrandPosition != "" && c.BrandPosition != "absent" {
			a.mentions++
		}
		if strings.TrimSpace(c.CitedURLs) != "" {
			a.cited++
		}
	}
	// 行序：样本量降序、意图名升序兜底（map 迭代序随机，报告必须可复现）
	order := make([]string, 0, len(byIntent))
	for k := range byIntent {
		order = append(order, k)
	}
	sort.Slice(order, func(i, j int) bool {
		if byIntent[order[i]].queries != byIntent[order[j]].queries {
			return byIntent[order[i]].queries > byIntent[order[j]].queries
		}
		return order[i] < order[j]
	})
	out := make([]ContentFormatAttributionRow, 0, len(order))
	for _, intent := range order {
		a := byIntent[intent]
		row := ContentFormatAttributionRow{
			Intent:       intent,
			Queries:      a.queries,
			Mentions:     a.mentions,
			MentionRate:  safeDivF(a.mentions, a.queries),
			CitedQueries: a.cited,
			CitationRate: safeDivF(a.cited, a.queries),
		}
		if st := GetIntentStrategy(ctx, intent); intent != "未分类" && st.Intent == intent {
			// 只在矩阵里有该意图时挂 ContentFormat——"未分类"回退到"信息"策略
			// 会伪造一个不存在的归因。
			row.ContentFormat = st.ContentFormat
		}
		out = append(out, row)
	}
	return &ContentFormatAttributionReport{Days: days, Rows: out}
}

// selfBrandSOV 摘要卡片里的 avg_sov：与 GET /geo/sov 同口径，
// 即"自家品牌在全部品牌提及次数中的占比"，取最近 500 条探针记录。
// 品牌名来自 geo_config.brand_name；配置缺失、读取失败、无探针数据或
// 自家品牌零提及都返回 0——宁可为 0 也不再回填写死的展示常量。
func (s *GeoDecisionAnalyticsService) selfBrandSOV(ctx context.Context) float64 {
	if s.configRepo == nil || s.probeRepo == nil {
		return 0
	}
	cfg, err := s.configRepo.Get(ctx)
	if err != nil || cfg == nil {
		return 0
	}
	brand := strings.TrimSpace(cfg.BrandName)
	if brand == "" {
		return 0
	}
	entries, err := s.GetShareOfVoiceBetween(ctx, "", time.Time{}, time.Time{})
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.Brand == brand {
			return e.SOV
		}
	}
	return 0
}

// RecordCrawlerVisit 调用方未提供来源证据，保留 unknown，不能凭关键词或 UA 认作真实流量。
func (s *GeoDecisionAnalyticsService) RecordCrawlerVisit(ctx context.Context, keyword, userAgent, path, engine string) error {
	return s.crawler.Create(ctx, &model.GeoCrawlerVisit{
		Keyword: keyword, UserAgent: userAgent, Path: path, Engine: engine, Source: model.CrawlerSourceUnknown,
	})
}

// CrawlerStatsResponse 爬虫统计前端友好返回
type CrawlerStatsResponse struct {
	DataSource     CrawlerDataSource           `json:"data_source"`
	Summary        CrawlerStatsSummary         `json:"summary"`
	KeywordStats   []repository.KeywordStatRow `json:"keyword_stats"`
	DomainStats    []repository.DomainStatRow  `json:"domain_stats"`
	KeywordCompare []KeywordCompareRow         `json:"keyword_compare"`
	DomainCompare  []DomainCompareRow          `json:"domain_compare"`
	CoverageScore  float64                     `json:"self_site_probe_share_pct"`
}

// CrawlerDataSource 报表证据边界。UA 条目可多对一归组，组数不是 UA 数。
type CrawlerDataSource struct {
	Kind                   string `json:"kind"`
	SuccessCriteria        string `json:"success_criteria"`
	ExternalCrawlVerified  bool   `json:"external_crawl_verified"`
	IndexingVerified       bool   `json:"indexing_verified"`
	UnattributedRecords    string `json:"unattributed_records"`
	WindowDays             int    `json:"window_days"`
	ConfiguredUACount      int    `json:"configured_ua_count"`
	ConfiguredEngineGroups int    `json:"configured_engine_groups"`
}

func crawlerDataSource(ctx context.Context) CrawlerDataSource {
	uas := activeCrawlerUAs(ctx)
	groups := make(map[string]bool)
	for _, ua := range uas {
		groups[extractEngineFromUA(ua)] = true
	}
	return CrawlerDataSource{
		Kind: model.CrawlerSourceActiveUAProbe, SuccessCriteria: "http_2xx_3xx",
		UnattributedRecords: "excluded", WindowDays: 30,
		ConfiguredUACount: len(uas), ConfiguredEngineGroups: len(groups),
	}
}

type CrawlerStatsSummary struct {
	TodayVisits      int64   `json:"last_24h_probes"`
	ActiveKeywords   int64   `json:"active_keywords"`
	ActiveEngines    int64   `json:"active_engines"`
	ActiveDomains    int64   `json:"active_domains"`
	ALevelCount      int64   `json:"a_level_count"`
	AvgSOV           float64 `json:"avg_sov"`
	HiveMTKVisits    int64   `json:"self_site_probes"`
	CompetitorVisits int64   `json:"competitor_probes"`
}

// KeywordCompareRow 关键词标签的主动探测聚合，不区分本站和竞品。
type KeywordCompareRow struct {
	Keyword        string `json:"keyword"`
	HiveMTKVisits  int64  `json:"probe_count"`
	HiveMTKEngines int    `json:"probed_engine_groups"`
	TotalEngines   int    `json:"configured_engine_groups"`
}

// DomainCompareRow 域名维度 自家站 vs 竞品 排名
//
// IsSelfSite 来自当前租户官网基址与路径边界；is_hivemtk 仅为旧字段别名。
type DomainCompareRow struct {
	Domain      string  `json:"domain"`
	IsSelfSite  bool    `json:"is_self_site"`
	IsHiveMTK   bool    `json:"is_hivemtk"`
	Visits      int64   `json:"probe_count"`
	Engines     int     `json:"engines"`
	SharePct    float64 `json:"probe_share_pct"`
	SourceLevel string  `json:"source_level"`
}

// GetCrawlerStats 返回关键词 + 域名 + 对比 三维度
func (s *GeoDecisionAnalyticsService) GetCrawlerStats(ctx context.Context) (*CrawlerStatsResponse, error) {
	var err error
	ctx, err = crawlerTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	// 信源站点等级 DB 优先（geo_dicts/source_levels.map），缺行自动播种；
	// 失败时不注入，repository 回退内置表（fail-open）。
	var levelMap map[string]string
	if err := DictJSON(ctx, model.DictCategorySourceLevels, "map", `{"weibanzhushou.com":"B","tanmascrm.com":"C","fengchenscrm.com":"C","hubspot.com":"A","producthunt.com":"A","techcrunch.com":"A","intercom.com":"A","baidu.com":"A","google.com":"A","bing.com":"A"}`, &levelMap); err == nil && len(levelMap) > 0 {
		repository.SetDomainSourceLevel(levelMap)
	}
	keywordRows, err := s.crawler.StatsByKeyword(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("读取关键词探测统计: %w", err)
	}
	domainRows, err := s.crawler.StatsByDomain(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("读取站点探测统计: %w", err)
	}
	totalVisits, err := s.crawler.TotalVisits(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("读取近期探测数: %w", err)
	}
	activeKws, err := s.crawler.ActiveKeywords(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("读取探测关键词数: %w", err)
	}
	activeEngines, err := s.crawler.ActiveEngines(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("读取探测分组数: %w", err)
	}
	activeDomains, err := s.crawler.ActiveDomains(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("读取探测站点数: %w", err)
	}

	var aLevelCount int64
	for _, r := range domainRows {
		if r.SourceLevel == "A" {
			aLevelCount++
		}
	}

	source := crawlerDataSource(ctx)
	kwCompare := computeKeywordCompare(keywordRows, source.ConfiguredEngineGroups)
	domainCompare, hivemtkVisits, compVisits, coverage := computeDomainCompare(domainRows)
	avgSOV := s.selfBrandSOV(ctx)

	return &CrawlerStatsResponse{
		DataSource: source,
		Summary: CrawlerStatsSummary{
			TodayVisits:      totalVisits,
			ActiveKeywords:   activeKws,
			ActiveEngines:    activeEngines,
			ActiveDomains:    activeDomains,
			ALevelCount:      aLevelCount,
			AvgSOV:           avgSOV,
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

func computeKeywordCompare(keywordRows []repository.KeywordStatRow, configuredGroups int) []KeywordCompareRow {

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
			Keyword:        kw,
			HiveMTKVisits:  total,
			HiveMTKEngines: len(engines),
			TotalEngines:   configuredGroups,
		})
	}
	return out
}

func computeDomainCompare(domainRows []repository.DomainStatRow) ([]DomainCompareRow, int64, int64, float64) {
	type domAgg struct {
		visits  int64
		engines map[string]bool
		level   string
		self    bool
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
		d.self = d.self || r.IsSelfSite
	}

	var totalAll int64
	for _, d := range bucket {
		totalAll += d.visits
	}

	var hivemtkVisits, compVisits int64
	out := make([]DomainCompareRow, 0, len(bucket))
	for domain, d := range bucket {
		isHive := d.self
		if isHive {
			hivemtkVisits += d.visits
		} else {
			compVisits += d.visits
		}
		var share float64
		if totalAll > 0 {
			share = float64(d.visits) / float64(totalAll) * 100
		}
		out = append(out, DomainCompareRow{
			Domain:      domain,
			IsSelfSite:  isHive,
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

// DetectInaccurateClaims 对指定品牌的验证回答做不准确声明检测。
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
	costUSD, costCNY := EstimateCostUSD(ctx, resp.Model, resp.InputTokens, resp.OutputTokens)
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
	// 成本记录失败必须出声：静默丢会让成本报表系统性少计。
	if err := s.apiCallRepo.Create(ctx, call); err != nil {
		logger.Warnf("[GEO 成本] API 调用记录写入失败 purpose=%s model=%s: %v", purpose, call.Model, err)
	}
}
