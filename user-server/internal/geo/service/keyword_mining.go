package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// KeywordMiningService 关键词蒸馏服务（GEO v2 新增）
//
// 负责：下拉词 API 抓取、模板化长尾词组合、LLM 意图分类、4 层漏斗统计。
// 是 KeywordService（LLM 造词）的补充——提供真实用户搜索数据。
type KeywordMiningService struct {
	kwRepo repository.GeoKeywordRepository
	db     *gorm.DB
	llm    *LLMAdapter
}

func NewKeywordMiningService(
	kwRepo repository.GeoKeywordRepository,
	db *gorm.DB,
	llm *LLMAdapter,
) *KeywordMiningService {
	return &KeywordMiningService{kwRepo: kwRepo, db: db, llm: llm}
}

// ────────────────────────────────────────────
// Suggest 引擎接口（5 引擎并发）
// ────────────────────────────────────────────

type SuggestEngine interface {
	Name() string
	Fetch(ctx context.Context, keyword string) ([]string, error)
}

// JSONP 剥壳正则
var jsonpRe = regexp.MustCompile(`^\w+\((.*)\);?$`)

func stripJSONP(s string) string {
	m := jsonpRe.FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return s
}

// BaiduSuggestEngine 百度下拉词
type BaiduSuggestEngine struct{ client *http.Client }

func (e *BaiduSuggestEngine) Name() string { return "baidu" }
func (e *BaiduSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://suggestion.baidu.com/su?wd=%s&cb=cb&ie=utf-8", url.QueryEscape(kw))
	return fetchJSONPArray(ctx, e.client, u, "s")
}

// BingSuggestEngine Bing 下拉词
type BingSuggestEngine struct{ client *http.Client }

func (e *BingSuggestEngine) Name() string { return "bing" }
func (e *BingSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://api.bing.com/qsonhs.aspx?q=%s&type=cb&cb=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var resp struct {
		AS struct {
			Results []struct {
				Suggests []struct{ Txt string } `json:"Suggests"`
			} `json:"Results"`
		} `json:"AS"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range resp.AS.Results {
		for _, s := range r.Suggests {
			if s.Txt != "" {
				out = append(out, s.Txt)
			}
		}
	}
	return out, nil
}

// GoogleSuggestEngine Google 下拉词
type GoogleSuggestEngine struct{ client *http.Client }

func (e *GoogleSuggestEngine) Name() string { return "google" }
func (e *GoogleSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://suggestqueries.google.com/complete/search?client=firefox&hl=zh-CN&q=%s&callback=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var arr []any
	if err := json.Unmarshal([]byte(body), &arr); err != nil {
		return nil, err
	}
	if len(arr) >= 2 {
		if items, ok := arr[1].([]any); ok {
			out := []string{}
			for _, it := range items {
				if s, ok := it.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out, nil
		}
	}
	return nil, nil
}

// So360SuggestEngine 360 下拉词
type So360SuggestEngine struct{ client *http.Client }

func (e *So360SuggestEngine) Name() string { return "360" }
func (e *So360SuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://sug.so.360.cn/suggest?format=json&word=%s&callback=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var resp struct {
		Result []struct{ Word string } `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range resp.Result {
		if r.Word != "" {
			out = append(out, r.Word)
		}
	}
	return out, nil
}

// SogouSuggestEngine 搜狗下拉词
type SogouSuggestEngine struct{ client *http.Client }

func (e *SogouSuggestEngine) Name() string { return "sogou" }
func (e *SogouSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://sor.html5.qq.com/api/getsug?key=%s", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	var arr []any
	if err := json.Unmarshal([]byte(body), &arr); err != nil {
		return nil, err
	}
	if len(arr) >= 2 {
		if items, ok := arr[1].([]any); ok {
			out := []string{}
			for _, it := range items {
				if s, ok := it.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out, nil
		}
	}
	return nil, nil
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second}
}

func fetchRaw(ctx context.Context, client *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func fetchJSONPArray(ctx context.Context, client *http.Client, u, field string) ([]string, error) {
	body, err := fetchRaw(ctx, client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return nil, err
	}
	val, ok := m[field]
	if !ok {
		return nil, nil
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, nil
	}
	out := []string{}
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// ────────────────────────────────────────────
// 公开方法
// ────────────────────────────────────────────

// allEngines 返回全部 5 个引擎实现
func (s *KeywordMiningService) allEngines() []SuggestEngine {
	return []SuggestEngine{
		&BaiduSuggestEngine{client: newHTTPClient()},
		&BingSuggestEngine{client: newHTTPClient()},
		&GoogleSuggestEngine{client: newHTTPClient()},
		&So360SuggestEngine{client: newHTTPClient()},
		&SogouSuggestEngine{client: newHTTPClient()},
	}
}

// CrawlSuggest 并发抓取 5 引擎下拉词
// 每个种子词 × 每个引擎 = 一次请求
// 结果去重合并，source 标记为 suggest_<engine>
func (s *KeywordMiningService) CrawlSuggest(ctx context.Context, seedWords []string, engines []string) ([]*model.GeoKeyword, error) {
	engineMap := map[string]SuggestEngine{}
	for _, e := range s.allEngines() {
		engineMap[e.Name()] = e
	}

	targetEngines := []SuggestEngine{}
	for _, name := range engines {
		if e, ok := engineMap[name]; ok {
			targetEngines = append(targetEngines, e)
		}
	}
	if len(targetEngines) == 0 {
		targetEngines = s.allEngines()
	}

	type result struct {
		keyword string
		source  string
		engine  string
		err     error
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []result
	)

	for _, seed := range seedWords {
		for _, eng := range targetEngines {
			wg.Add(1)
			go func(seed string, eng SuggestEngine) {
				defer wg.Done()
				suggests, err := eng.Fetch(ctx, seed)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					results = append(results, result{err: fmt.Errorf("[%s] %s: %w", eng.Name(), seed, err)})
					return
				}
				for _, kw := range suggests {
					results = append(results, result{
						keyword: kw, source: "suggest_" + eng.Name(), engine: eng.Name(),
					})
				}
			}(seed, eng)
		}
	}
	wg.Wait()

	// 去重
	seen := map[string]*model.GeoKeyword{}
	for _, r := range results {
		if r.err != nil {
			logger.Warnf("suggest 抓取错误: %v", r.err)
			continue
		}
		kw := strings.TrimSpace(r.keyword)
		if kw == "" {
			continue
		}
		if existing, ok := seen[kw]; ok {
			// 合并 engines
			var engs []string
			_ = json.Unmarshal([]byte(existing.SuggestEngines), &engs)
			engs = append(engs, r.engine)
			b, _ := json.Marshal(engs)
			existing.SuggestEngines = string(b)
			existing.SuggestCount++
			continue
		}
		engs, _ := json.Marshal([]string{r.engine})
		seen[kw] = &model.GeoKeyword{
			Keyword:        kw,
			Source:         r.source,
			Category:       "suggest",
			Layer:          "suggest",
			Intent:         "info",
			FunnelStage:    "cognitive",
			Status:         "active",
			SuggestEngines: string(engs),
			SuggestCount:   1,
		}
	}

	out := make([]*model.GeoKeyword, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, nil
}

// LongtailTemplate 长尾词模板
type LongtailTemplate struct {
	Template    string // 含 {seed} 占位符
	QueryIntent string // 映射到 GeoKeyword.QueryIntent
	FunnelStage string // 映射到 GeoKeyword.FunnelStage
}

// DefaultLongtailTemplates 默认 36 个模板（6 意图 × 6 场景）
var DefaultLongtailTemplates = []LongtailTemplate{
	// how_to
	{"如何选择{seed}系统", "how_to", "cognitive"},
	{"{seed}系统怎么选", "how_to", "cognitive"},
	{"{seed}选型指南", "how_to", "evaluation"},
	{"中小企业怎么选{seed}", "how_to", "evaluation"},
	// comparison
	{"{seed}免费版和付费版区别", "comparison", "evaluation"},
	{"{seed}和XX对比哪个好", "comparison", "evaluation"},
	{"{seed}优缺点分析", "comparison", "evaluation"},
	{"2026年{seed}对比评测", "comparison", "evaluation"},
	// recommendation
	{"2026年{seed}推荐", "recommendation", "evaluation"},
	{"{seed}系统排名TOP10", "recommendation", "evaluation"},
	{"中小企业{seed}选型指南2026", "recommendation", "decision"},
	{"免费{seed}推荐", "recommendation", "decision"},
	// problem
	{"{seed}实施失败的原因", "problem", "cognitive"},
	{"{seed}常见问题解答", "problem", "cognitive"},
	{"{seed}使用痛点", "problem", "cognitive"},
	{"{seed}避坑指南", "problem", "cognitive"},
	// pricing
	{"{seed}多少钱一年", "pricing", "decision"},
	{"{seed}价格对比", "pricing", "decision"},
	{"{seed}免费吗", "pricing", "decision"},
	{"{seed}计费方式", "pricing", "decision"},
	// case_study
	{"{seed}实施案例", "case_study", "retention"},
	{"{seed}成功案例", "case_study", "retention"},
	{"某企业{seed}落地实践", "case_study", "retention"},
}

// CombineLongtail 模板化长尾词组合
// seedWords: 种子词列表
// templates: 不传则用 DefaultLongtailTemplates
func (s *KeywordMiningService) CombineLongtail(ctx context.Context, seedWords []string, templates []LongtailTemplate) ([]*model.GeoKeyword, error) {
	if len(templates) == 0 {
		templates = DefaultLongtailTemplates
	}

	seen := map[string]*model.GeoKeyword{}
	for _, seed := range seedWords {
		for _, tpl := range templates {
			kw := strings.ReplaceAll(tpl.Template, "{seed}", seed)
			if kw == "" {
				continue
			}
			if _, exists := seen[kw]; exists {
				continue
			}
			seen[kw] = &model.GeoKeyword{
				Keyword:     kw,
				Source:      "template_combined",
				Category:    "longtail",
				Layer:       "longtail",
				ParentID:    "", // 后续可关联
				QueryIntent: tpl.QueryIntent,
				FunnelStage: tpl.FunnelStage,
				Intent:      tpl.QueryIntent,
				Status:      "active",
			}
		}
	}

	out := make([]*model.GeoKeyword, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, nil
}

// ClassifyIntent LLM 分类 query_intent
// 批量处理，返回 keyword → intent 映射
func (s *KeywordMiningService) ClassifyIntent(ctx context.Context, keywords []string) (map[string]string, error) {
	result := map[string]string{}
	// 简单启发式分类（零 LLM 成本，效果足够好）
	intentRules := []struct {
		intent string
		words  []string
	}{
		{"pricing", []string{"多少钱", "价格", "费用", "计费", "免费吗", "多少钱一年", "报价"}},
		{"comparison", []string{"对比", "区别", "哪个好", "vs", "和.*比"}},
		{"recommendation", []string{"推荐", "排行", "TOP", "排名", "选型指南"}},
		{"problem", []string{"失败", "问题", "痛点", "坑", "误区", "常见"}},
		{"how_to", []string{"如何", "怎么", "怎样", "方法", "步骤", "指南"}},
		{"case_study", []string{"案例", "实施", "落地", "实践", "成功"}},
	}
	for _, kw := range keywords {
		assigned := "recommendation" // 默认
		for _, rule := range intentRules {
			for _, w := range rule.words {
				if strings.Contains(kw, w) {
					assigned = rule.intent
					break
				}
			}
			if assigned != "recommendation" {
				break
			}
		}
		result[kw] = assigned
	}
	return result, nil
}

// KeywordFunnel 漏斗统计
type KeywordFunnel struct {
	SeedCount      int            `json:"seed_count"`
	RelatedCount   int            `json:"related_count"`
	SuggestCount   int            `json:"suggest_count"`
	LongtailCount  int            `json:"longtail_count"`
	Total          int            `json:"total"`
	Intents        map[string]int `json:"intents"`
	FunnelStages   map[string]int `json:"funnel_stages"`
	SuggestEngines map[string]int `json:"suggest_engines"`
}

// BuildFunnel 从 DB 构建 4 层漏斗统计
func (s *KeywordMiningService) BuildFunnel(ctx context.Context) (*KeywordFunnel, error) {
	f := &KeywordFunnel{
		Intents:        map[string]int{},
		FunnelStages:   map[string]int{},
		SuggestEngines: map[string]int{},
	}

	// 按 layer 分组
	type row struct {
		Layer string `gorm:"column:layer"`
		Cnt   int64  `gorm:"column:cnt"`
	}
	var rows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(layer,'seed') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(layer,'seed')").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		switch r.Layer {
		case "seed":
			f.SeedCount = int(r.Cnt)
		case "related":
			f.RelatedCount = int(r.Cnt)
		case "suggest":
			f.SuggestCount = int(r.Cnt)
		case "longtail":
			f.LongtailCount = int(r.Cnt)
		}
		f.Total += int(r.Cnt)
	}

	// 按 query_intent 分组
	var irows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(query_intent,'unknown') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(query_intent,'unknown')").
		Scan(&irows).Error; err != nil {
		return nil, err
	}
	for _, r := range irows {
		f.Intents[r.Layer] = int(r.Cnt)
	}

	// 按 funnel_stage 分组
	var srows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(funnel_stage,'unknown') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(funnel_stage,'unknown')").
		Scan(&srows).Error; err != nil {
		return nil, err
	}
	for _, r := range srows {
		f.FunnelStages[r.Layer] = int(r.Cnt)
	}

	return f, nil
}
