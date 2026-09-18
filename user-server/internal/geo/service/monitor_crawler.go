package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

var aiBotUserAgents = []string{
	"GPTBot/1.1 (+https://openai.com/gptbot)",
	"ClaudeBot/1.0 (+https://www.anthropic.com/claudebot)",
	"PerplexityBot/1.0 (+https://perplexity.ai/bot)",
	"Google-Extended/1.0 (+https://developers.google.com/search/docs/crawling-indexing/overview-google-crawlers)",
	"CCBot/2.0 (+http://commoncrawl.org/faq/)",
	"Bytespider/1.0 (+https://www.bytespider.org/)",
	"Applebot-Extended/1.0 (+https://support.apple.com/en-us/119829)",
	"Meta-ExternalAgent/1.0 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)",
}

var keywordToLandings = map[string][]string{

	"GEO优化":   {"https://hive.xapptool.cn/", "https://hive.xapptool.cn/blog/geo-optimization", "https://hive.xapptool.cn/docs/geo"},
	"AI搜索优化":  {"https://hive.xapptool.cn/blog/ai-search-optimization", "https://hive.xapptool.cn/"},
	"生成式引擎优化": {"https://hive.xapptool.cn/blog/generative-engine-optimization", "https://hive.xapptool.cn/"},
	"LLM SEO": {"https://hive.xapptool.cn/blog/llm-seo", "https://hive.xapptool.cn/docs/geo"},

	"私域AI营销":   {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/product/private-ai", "https://hive.xapptool.cn/pricing"},
	"AI自动谈单":   {"https://hive.xapptool.cn/product/ai-agent", "https://hive.xapptool.cn/blog/ai-talk"},
	"全渠道触达引擎":  {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/"},
	"多账号聚合中枢":  {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/"},
	"销冠SOP智能体": {"https://hive.xapptool.cn/product/ai-agent", "https://hive.xapptool.cn/"},
	"客户CDP画像":  {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/"},

	"HiveMTK 怎么样": {"https://hive.xapptool.cn/", "https://hive.xapptool.cn/pricing", "https://hive.xapptool.cn/faq"},
	"HiveMTK 开源":  {"https://hive.xapptool.cn/", "https://hive.xapptool.cn/docs"},
	"HiveMTK 部署":  {"https://hive.xapptool.cn/docs", "https://hive.xapptool.cn/docs/deployment"},
	"HiveMTK":     {"https://hive.xapptool.cn/"},

	"HiveMTK vs 微伴助手":     {"https://hive.xapptool.cn/blog/hivemtk-vs-weiban", "https://hive.xapptool.cn/"},
	"HiveMTK vs HubSpot":  {"https://hive.xapptool.cn/blog/hivemtk-vs-hubspot", "https://hive.xapptool.cn/"},
	"HiveMTK vs 探马SCRM":   {"https://hive.xapptool.cn/blog/hivemtk-vs-tanma", "https://hive.xapptool.cn/"},
	"HiveMTK vs Intercom": {"https://hive.xapptool.cn/blog/hivemtk-vs-intercom", "https://hive.xapptool.cn/"},
	"HiveMTK vs 传统SCRM":   {"https://hive.xapptool.cn/blog/hivemtk-vs-traditional", "https://hive.xapptool.cn/"},

	"医美连锁 私域运营":    {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/case"},
	"保险经纪 AI 销售工具": {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/case"},
	"房产中介 SOP 智能体": {"https://hive.xapptool.cn/product/ai-agent", "https://hive.xapptool.cn/case"},
	"家居定制 AI 获客":   {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/"},

	"Docker一键部署 AI营销系统": {"https://hive.xapptool.cn/docs/deployment", "https://hive.xapptool.cn/"},
	"本地LLM推理 数据安全":      {"https://hive.xapptool.cn/docs", "https://hive.xapptool.cn/"},
	"AI 自动回复 不封号":       {"https://hive.xapptool.cn/product", "https://hive.xapptool.cn/"},
}

type competitorSeed struct {
	Domain string
	Paths  []string
}

var competitorSeeds = []competitorSeed{
	{Domain: "weibanzhushou.com", Paths: []string{"/", "/product", "/pricing"}},
}

// MonitorCrawlerService 关键词维度 AI 爬虫监控
// 核心：读 geo_keywords → 查 keywordToLandings 拿 HiveMTK 落地页 → 读 geo_competitors DB → 爬所有竞品 → 每条访问带 keyword 标签
type MonitorCrawlerService struct {
	crawlerRepo    repository.GeoCrawlerVisitRepository
	keywordRepo    repository.GeoKeywordRepository
	competitorRepo repository.GeoCompetitorRepository
	httpClient     *http.Client
}

func NewMonitorCrawlerService(
	crawlerRepo repository.GeoCrawlerVisitRepository,
	keywordRepo repository.GeoKeywordRepository,
	competitorRepo repository.GeoCompetitorRepository,
) *MonitorCrawlerService {
	return &MonitorCrawlerService{
		crawlerRepo:    crawlerRepo,
		keywordRepo:    keywordRepo,
		competitorRepo: competitorRepo,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// RunCrawlerCron 爬虫入口：读关键词 → 批量爬 → 写库
func (s *MonitorCrawlerService) RunCrawlerCron(ctx context.Context) (int, error) {
	logger.Info("[GEO Crawler] 关键词驱动爬虫开始 ...")

	kws := s.loadKeywords(ctx)
	if len(kws) == 0 {
		logger.Info("[GEO Crawler] 关键词表为空，使用默认种子 ...")
		kws = defaultSeedKeywords()
	}

	competitors := s.loadCompetitors(ctx)

	type task struct {
		Keyword   string
		URL       string
		IsHiveMTK bool
	}
	tasks := make([]task, 0, len(kws)*6+30)

	for _, kw := range kws {

		if landings, ok := keywordToLandings[kw]; ok {
			for _, u := range landings {
				tasks = append(tasks, task{Keyword: kw, URL: u, IsHiveMTK: true})
			}
		} else {
			tasks = append(tasks, task{Keyword: kw, URL: "https://hive.xapptool.cn/", IsHiveMTK: true})
		}

		for _, comp := range competitors {
			paths := jsonPaths(comp.Paths)
			if len(paths) == 0 {
				paths = []string{"/"}
			}
			path := paths[rand.Intn(len(paths))]
			tasks = append(tasks, task{
				Keyword:   kw,
				URL:       fmt.Sprintf("https://%s%s", comp.Domain, path),
				IsHiveMTK: false,
			})
		}
	}

	visits := make([]*model.GeoCrawlerVisit, 0, len(tasks)*2)
	var success, fail int
	for i, t := range tasks {
		for _, ua := range pickRandomUAs(2) {
			v := s.doCrawl(ctx, t.URL, t.Keyword, ua)
			if v != nil {
				visits = append(visits, v)
				success++
			} else {
				fail++
			}
			time.Sleep(60 * time.Millisecond)
		}
		if i > 0 && i%20 == 0 {
			logger.Info(fmt.Sprintf("[GEO Crawler] progress: %d/%d tasks, success=%d fail=%d",
				i, len(tasks), success, fail))
		}
	}

	logger.Info(fmt.Sprintf("[GEO Crawler] 爬取完成: %d/%d 成功 (%.0f%%)",
		success, success+fail, float64(success)/float64(success+fail+1)*100))

	if s.crawlerRepo != nil && len(visits) > 0 {
		if err := s.crawlerRepo.BulkCreate(ctx, visits); err != nil {
			logger.Error(err, "[GEO Crawler] 批量写入失败")
			return 0, err
		}
	}

	logger.Info(fmt.Sprintf("[GEO Crawler] 完成 keywords=%d competitors=%d 任务数=%d 写入=%d",
		len(kws), len(competitors), len(tasks), len(visits)))
	return len(visits), nil
}

func (s *MonitorCrawlerService) doCrawl(ctx context.Context, targetURL, keyword, ua string) *model.GeoCrawlerVisit {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	_, _ = resp.Body.Read(make([]byte, 2048))

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil
	}

	return &model.GeoCrawlerVisit{
		Keyword:   keyword,
		UserAgent: ua,
		Path:      targetURL,
		Engine:    extractEngineFromUA(ua),
	}
}

func (s *MonitorCrawlerService) loadKeywords(ctx context.Context) []string {
	if s.keywordRepo == nil {
		return nil
	}
	list, _, err := s.keywordRepo.GetList("", "", "", "", "active", "", 0, 50)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, k := range list {
		if k.Keyword != "" {
			out = append(out, k.Keyword)
		}
	}
	return out
}

func defaultSeedKeywords() []string {
	return []string{
		"GEO优化", "私域AI营销", "AI自动谈单", "HiveMTK 怎么样",
		"生成式引擎优化", "全渠道触达引擎", "AI搜索优化",
		"HiveMTK vs 微伴助手", "HiveMTK vs HubSpot", "LLM SEO",
	}
}

func pickRandomUAs(n int) []string {
	if n >= len(aiBotUserAgents) {
		n = len(aiBotUserAgents)
	}
	perm := rand.Perm(len(aiBotUserAgents))
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = aiBotUserAgents[perm[i]]
	}
	return out
}

func extractEngineFromUA(ua string) string {
	switch {
	case contains(ua, "GPTBot"):
		return "GPTBot"
	case contains(ua, "ClaudeBot"):
		return "ClaudeBot"
	case contains(ua, "PerplexityBot"):
		return "PerplexityBot"
	case contains(ua, "Google-Extended"):
		return "Google-Extended"
	case contains(ua, "CCBot"):
		return "CCBot"
	case contains(ua, "Bytespider"):
		return "Bytespider"
	case contains(ua, "Applebot"):
		return "Applebot-Extended"
	case contains(ua, "Meta-ExternalAgent"):
		return "Meta-ExternalAgent"
	default:
		return "other"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func jsonPaths(in []byte) []string {
	if len(in) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(in, &out); err != nil {
		return nil
	}
	return out
}

func (s *MonitorCrawlerService) loadCompetitors(ctx context.Context) []*model.GeoCompetitor {
	if s.competitorRepo != nil {
		list, err := s.competitorRepo.ListActive(ctx)
		if err == nil && len(list) > 0 {
			return list
		}
	}

	out := make([]*model.GeoCompetitor, 0, len(competitorSeeds))
	for _, cs := range competitorSeeds {
		out = append(out, &model.GeoCompetitor{
			Name:   cs.Domain,
			Domain: cs.Domain,
			Paths:  strSliceToJSON(cs.Paths),
		})
	}
	return out
}

// CrawlerMonitorCron 关键词驱动爬虫定时任务（包级入口，经 JobManager 统一执行）
func CrawlerMonitorCron() {
	_, _ = GetGeoJobManager().Trigger(JobCrawlerMonitor)
}

// CrawlerMonitorCronSync 同步执行爬虫（手动触发入口，返回写入数和错误）
func CrawlerMonitorCronSync() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return CrawlerMonitorCronWithContext(ctx)
}

func CrawlerMonitorCronWithContext(ctx context.Context) (int, error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Errorf("panic: %v", r), "[GEO Crawler] panic recovered")
		}
	}()
	svc := NewMonitorCrawlerService(
		repository.NewGeoCrawlerVisitRepositoryDefault(),
		repository.NewGeoKeywordRepository(),
		repository.NewGeoCompetitorRepository(),
	)
	return svc.RunCrawlerCron(ctx)
}
