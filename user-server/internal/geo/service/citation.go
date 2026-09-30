package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// CitationService 引用智能服务
//
// 对标 gego stats/urls 与 elmo 引用分析：把探针 Citations jsonb 聚合成
// 域名统计（自有/earned/竞品分类）与 keyword×domain 矩阵。
type CitationService struct {
	probeRepo      repository.GeoProbeRunRepository
	configRepo     repository.GeoConfigRepository
	competitorRepo repository.GeoCompetitorRepository
}

// NewCitationService 构造引用智能服务
func NewCitationService(
	probeRepo repository.GeoProbeRunRepository,
	configRepo repository.GeoConfigRepository,
	competitorRepo repository.GeoCompetitorRepository,
) *CitationService {
	return &CitationService{probeRepo: probeRepo, configRepo: configRepo, competitorRepo: competitorRepo}
}

// CitationDomainStat 单域名引用统计
type CitationDomainStat struct {
	Domain      string `json:"domain"`
	Category    string `json:"category"` // own | competitor | earned
	Count       int    `json:"count"`
	QueryCount  int    `json:"query_count"`
	EngineCount int    `json:"engine_count"`
}

// CitationDomainResult 域名聚合结果
type CitationDomainResult struct {
	Days    int                 `json:"days"`
	Domains []CitationDomainStat `json:"domains"`
	Total   int                 `json:"total"`
}

// CitationMatrixRow keyword×domain 矩阵行
type CitationMatrixRow struct {
	Query   string         `json:"query"`
	Domains map[string]int `json:"domains"`
	Total   int            `json:"total"`
}

// CitationMatrixResult 矩阵结果
type CitationMatrixResult struct {
	Days    int                 `json:"days"`
	Domains []string            `json:"domains"`
	Rows    []CitationMatrixRow `json:"rows"`
}

// normalizeHost 归一化 host：小写、去 www. 前缀、去端口
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "www.")
	if idx := strings.Index(h, ":"); idx >= 0 {
		h = h[:idx]
	}
	return h
}

// classifyDomain 域名分类：自有 / 竞品 / earned
func classifyDomain(host, ownDomain string, competitorDomains map[string]bool) string {
	h := normalizeHost(host)
	if h == "" {
		return ""
	}
	own := normalizeHost(ownDomain)
	if own != "" && (h == own || strings.HasSuffix(h, "."+own)) {
		return "own"
	}
	if competitorDomains[h] {
		return "competitor"
	}
	return "earned"
}

// extractCitationURLs 从探针行解析引用 URL 列表
func extractCitationURLs(run *model.GeoProbeRun) []string {
	if run == nil || len(run.Citations) == 0 {
		return nil
	}
	var cites []Citation
	if err := json.Unmarshal(run.Citations, &cites); err != nil {
		return nil
	}
	out := make([]string, 0, len(cites))
	for _, c := range cites {
		if strings.TrimSpace(c.URL) != "" {
			out = append(out, c.URL)
		}
	}
	return out
}

// AggregateCitationDomains 纯聚合：runs → 按域名统计（含分类）
// ownDomain 为品牌官网域，competitorDomains 为竞品域集合（均做归一化）。
func AggregateCitationDomains(runs []*model.GeoProbeRun, ownDomain string, competitorDomains []string) []CitationDomainStat {
	compSet := make(map[string]bool, len(competitorDomains))
	for _, d := range competitorDomains {
		if h := normalizeHost(d); h != "" {
			compSet[h] = true
		}
	}
	type bucket struct {
		count   int
		queries map[string]bool
		engines map[string]bool
		cat     string
	}
	buckets := map[string]*bucket{}
	for _, run := range runs {
		for _, rawURL := range extractCitationURLs(run) {
			host := normalizeHost(ExtractDomain(rawURL))
			if host == "" {
				continue
			}
			b, ok := buckets[host]
			if !ok {
				b = &bucket{queries: map[string]bool{}, engines: map[string]bool{}, cat: classifyDomain(host, ownDomain, compSet)}
				buckets[host] = b
			}
			b.count++
			b.queries[run.Query] = true
			b.engines[run.Engine] = true
		}
	}
	out := make([]CitationDomainStat, 0, len(buckets))
	for host, b := range buckets {
		out = append(out, CitationDomainStat{
			Domain: host, Category: b.cat, Count: b.count,
			QueryCount: len(b.queries), EngineCount: len(b.engines),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// AggregateKeywordDomainMatrix 纯聚合：top 关键词 × top 域名矩阵
func AggregateKeywordDomainMatrix(runs []*model.GeoProbeRun, topQueries, topDomains int) ([]string, []CitationMatrixRow) {
	if topQueries <= 0 {
		topQueries = 20
	}
	if topDomains <= 0 {
		topDomains = 10
	}
	qCount := map[string]int{}
	dCount := map[string]int{}
	cells := map[string]map[string]int{}
	for _, run := range runs {
		urls := extractCitationURLs(run)
		if len(urls) == 0 {
			continue
		}
		qCount[run.Query]++
		for _, rawURL := range urls {
			host := normalizeHost(ExtractDomain(rawURL))
			if host == "" {
				continue
			}
			dCount[host]++
			m, ok := cells[run.Query]
			if !ok {
				m = map[string]int{}
				cells[run.Query] = m
			}
			m[host]++
		}
	}
	queries := topKeys(qCount, topQueries)
	domains := topKeys(dCount, topDomains)
	rows := make([]CitationMatrixRow, 0, len(queries))
	for _, q := range queries {
		row := CitationMatrixRow{Query: q, Domains: map[string]int{}}
		for _, d := range domains {
			if v := cells[q][d]; v > 0 {
				row.Domains[d] = v
				row.Total += v
			}
		}
		rows = append(rows, row)
	}
	return domains, rows
}

func topKeys(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	all := make([]kv, 0, len(m))
	for k, v := range m {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, 0, len(all))
	for _, kv := range all {
		out = append(out, kv.k)
	}
	return out
}

// loadClassifyContext 读取分类上下文：官网域 + 竞品域（配置 Competitors 文本 + 竞品表）
func (s *CitationService) loadClassifyContext(ctx context.Context) (string, []string) {
	var own string
	var comp []string
	if cfg, err := s.configRepo.Get(); err == nil && cfg != nil {
		own = ExtractDomain(cfg.Domain)
		for _, c := range strings.Split(cfg.Competitors, ",") {
			if d := ExtractDomain(strings.TrimSpace(c)); d != "" {
				comp = append(comp, d)
			}
		}
	}
	if comps, err := s.competitorRepo.ListActive(ctx); err == nil {
		for _, c := range comps {
			if d := normalizeHost(c.Domain); d != "" {
				comp = append(comp, d)
			}
		}
	}
	return own, comp
}

// DomainStats 聚合近 N 天探针引用域名
func (s *CitationService) DomainStats(ctx context.Context, days int) (*CitationDomainResult, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	runs, err := s.probeRepo.ListSince(ctx, time.Now().AddDate(0, 0, -days), 20000)
	if err != nil {
		return nil, err
	}
	own, comp := s.loadClassifyContext(ctx)
	stats := AggregateCitationDomains(runs, own, comp)
	total := 0
	for _, st := range stats {
		total += st.Count
	}
	return &CitationDomainResult{Days: days, Domains: stats, Total: total}, nil
}

// KeywordDomainMatrix 近 N 天 keyword×domain 矩阵
func (s *CitationService) KeywordDomainMatrix(ctx context.Context, days, topQueries, topDomains int) (*CitationMatrixResult, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	runs, err := s.probeRepo.ListSince(ctx, time.Now().AddDate(0, 0, -days), 20000)
	if err != nil {
		return nil, err
	}
	domains, rows := AggregateKeywordDomainMatrix(runs, topQueries, topDomains)
	return &CitationMatrixResult{Days: days, Domains: domains, Rows: rows}, nil
}
