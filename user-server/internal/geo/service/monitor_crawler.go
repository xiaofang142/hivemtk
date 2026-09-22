package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"hivemtk-user/internal/config"
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

// keywordToLandings 关键词 → 官网落地路径。
//
// 这里刻意只存**路径**不存完整 URL：官网基址是运行期配置（GEO_SITE_BASE_URL，
// 默认见 config.DefaultWebsiteBaseURL），域名换了改一处配置即可，不必再回来动这张表。
// 路径必须全部落在官网真实路由上（/ /features /toolchain /workflow /docs /faq /deploy），
// 由 site_base_test.go 逐条把门——爬虫投给 AI 引擎的每个落地页都得是活页。
var keywordToLandings = map[string][]string{
	"GEO优化":   {"/", "/features", "/docs"},
	"AI搜索优化":  {"/features", "/"},
	"生成式引擎优化": {"/features", "/"},
	"LLM SEO": {"/features", "/docs"},

	"私域AI营销":   {"/features", "/workflow"},
	"AI自动谈单":   {"/workflow", "/features"},
	"全渠道触达引擎":  {"/features", "/"},
	"多账号聚合中枢":  {"/features", "/"},
	"销冠SOP智能体": {"/workflow", "/"},
	"客户CDP画像":  {"/features", "/"},

	"HiveMTK 怎么样": {"/", "/faq", "/features"},
	"HiveMTK 开源":  {"/", "/docs"},
	"HiveMTK 部署":  {"/docs", "/deploy"},
	"HiveMTK":     {"/"},

	"HiveMTK vs 微伴助手":     {"/features", "/"},
	"HiveMTK vs HubSpot":  {"/features", "/"},
	"HiveMTK vs 探马SCRM":   {"/features", "/"},
	"HiveMTK vs Intercom": {"/features", "/"},
	"HiveMTK vs 传统SCRM":   {"/features", "/"},

	"医美连锁 私域运营":    {"/features", "/workflow"},
	"保险经纪 AI 销售工具": {"/features", "/workflow"},
	"房产中介 SOP 智能体": {"/workflow", "/features"},
	"家居定制 AI 获客":   {"/features", "/"},

	"Docker一键部署 AI营销系统": {"/deploy", "/"},
	"本地LLM推理 数据安全":      {"/toolchain", "/"},
	"AI 自动回复 不封号":       {"/features", "/workflow"},
}

// landingURLs 把关键词翻译成要爬的绝对 URL；没配过关键词的兜底是官网首页。
func landingURLs(kw string) []string {
	base := config.WebsiteBaseURL()
	paths, ok := keywordToLandings[kw]
	if !ok {
		paths = []string{"/"}
	}
	urls := make([]string, 0, len(paths))
	for _, p := range paths {
		urls = append(urls, base+p)
	}
	return urls
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

		for _, u := range landingURLs(kw) {
			tasks = append(tasks, task{Keyword: kw, URL: u, IsHiveMTK: true})
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
