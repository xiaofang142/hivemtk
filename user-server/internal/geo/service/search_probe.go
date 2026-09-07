package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	hivemodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

var urlRegexp = regexp.MustCompile(`https?://[^\s"'\)\]\}\>,;]+`)

// Citation 单条被引信源
type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// ProbeResult 单次探针结果
type ProbeResult struct {
	Engine    string     `json:"engine"`
	Query     string     `json:"query"`
	Response  string     `json:"response"`
	Citations []Citation `json:"citations"`
	LatencyMs int64      `json:"latency_ms"`
	Simulated bool       `json:"simulated"`
	Error     string     `json:"error,omitempty"`
	BrandHit  bool       `json:"brand_hit"`
	Sentiment string     `json:"sentiment"`
}

// SearchProbe 探针接口
type SearchProbe interface {
	Name() string
	Probe(ctx context.Context, query string) (*ProbeResult, error)
}

var probeHTTPClient = &http.Client{Timeout: 60 * time.Second}

func doJSON(ctx context.Context, endpoint, authHeader string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(rb))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rb, out)
}

type llmEndpointProbe struct {
	name      string
	endpoint  string
	model     string
	apiKey    string
	brandHint string
}

func (p *llmEndpointProbe) Name() string {
	if p.name != "" {
		return p.name
	}
	return "local-llm"
}

var sentimentRegexp = regexp.MustCompile(`(?m)^\s*\[SENTIMENT:\s*(positive|neutral|negative)\]\s*$`)

func (p *llmEndpointProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	start := time.Now()
	brandCtx := ""
	if p.brandHint != "" {
		brandCtx = fmt.Sprintf("\n【品牌上下文】%s。回答中涉及此品牌时请以此描述为准，不要和同名的其他项目混淆。", p.brandHint)
	}
	systemPrompt := `你是 AI 搜索答案专家。请以事实为依据回答用户的搜索查询。
要求：
1. 直接给出有用的回答（200-400字），不要说"作为AI我无法"之类的套话
2. 尽量引用具体信息源（网站、博客、文档链接等），如果不确定就说"据公开资料"
3. 如果涉及具体产品/品牌，客观陈述其定位和特点
4. 输出中自然出现"信息来源"或"参考链接"小节，列出 1-3 条你所知的可靠链接
5. 回答最后单独输出一行情感标签，格式严格为 [SENTIMENT: positive] 或 [SENTIMENT: neutral] 或 [SENTIMENT: negative]，三选一。判断标准：你的回答整体态度——如果在为品牌辩护、说没负面、没证据=positive；只是客观陈述=neutral；确认有真实差评/投诉/负面新闻=negative` + brandCtx
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	auth := ""
	if p.apiKey != "" && !strings.EqualFold(p.apiKey, "local") {
		auth = "Bearer " + p.apiKey
	}
	err := doJSON(ctx,
		p.endpoint+"/chat/completions",
		auth,
		map[string]any{
			"model":       p.model,
			"messages":    []map[string]any{{"role": "system", "content": systemPrompt}, {"role": "user", "content": query}},
			"temperature": 0.3,
			"max_tokens":  1024,
		},
		&out,
	)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty response", p.Name())
	}
	content := out.Choices[0].Message.Content

	sentiment := "neutral"
	if m := sentimentRegexp.FindStringSubmatch(content); len(m) == 2 {
		sentiment = m[1]
		content = sentimentRegexp.ReplaceAllString(content, "")
		content = strings.TrimSpace(content)
	}
	cites := extractCitationsFromText(content)
	return &ProbeResult{
		Engine: p.Name(), Query: query, Response: content,
		Citations: cites, LatencyMs: latency, Sentiment: sentiment,
	}, nil
}

func extractCitationsFromText(text string) []Citation {
	var cites []Citation
	for _, m := range urlRegexp.FindAllString(text, -1) {
		cites = append(cites, Citation{URL: m})
	}
	return cites
}

// NewEngineProbes 装配所有可用真实引擎探针（统一从 DB 配置读取）。
// 优先级：DB 中 enabled=true 的 provider 在前，本地 LLM 兜底在后。
// 环境变量仅作为补充（兼容老部署），不再硬编码任何 endpoint/model。
func NewEngineProbes() []SearchProbe {
	return NewEngineProbesFromDB(db.GetDB())
}

// NewEngineProbesFromDB 从 llm_providers 表装配探针引擎。
//
// 这是唯一的探针装配入口。所有 provider 配置（endpoint/model/api_key）
// 都从 DB 读取，不再硬编码。运维在后台改 llm_providers 表即可热切换探针引擎。
//
// 注意：探针引擎只使用云端 provider（排除 localhost / 127.0.0.1 本地 LLM），
// 避免本地 LLM 速度慢、质量低影响 GEO 验证准确性。本地 LLM 仅在 LLMAdapter
// 的 fallback 链中作为兜底。
func NewEngineProbesFromDB(g *gorm.DB) []SearchProbe {
	probes := []SearchProbe{}
	seen := make(map[string]bool)

	brandHint := ""
	if g != nil {
		var cfg struct {
			BrandName        string
			BrandDescription string
		}
		if err := g.Table("geo_config").Select("brand_name", "brand_description").First(&cfg).Error; err == nil {
			if cfg.BrandName != "" && cfg.BrandDescription != "" {
				brandHint = cfg.BrandName + "：" + cfg.BrandDescription
			} else if cfg.BrandName != "" {
				brandHint = cfg.BrandName
			}
		}
	}

	if g != nil {
		var rows []hivemodel.LLMProvider
		if err := g.Where("enabled = ?", true).Order("sort_order ASC").Find(&rows).Error; err == nil {
			for _, row := range rows {
				if row.BaseURL == "" || row.Model == "" {
					continue
				}

				isLocal := strings.HasPrefix(row.BaseURL, "http://127.0.0.1") ||
					strings.HasPrefix(row.BaseURL, "http://localhost")
				isFreeAPIProxy := strings.Contains(row.BaseURL, ":8787")
				if isLocal && !isFreeAPIProxy {
					continue
				}
				if seen[row.Name] {
					continue
				}
				seen[row.Name] = true
				probes = append(probes, &llmEndpointProbe{
					name:      row.Name,
					endpoint:  row.BaseURL,
					model:     row.Model,
					apiKey:    row.APIKey,
					brandHint: brandHint,
				})
			}
		}
	}
	return probes
}

func checkProbeHealth(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	if err != nil {
		return err
	}
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return nil
	}
	return fmt.Errorf("status %d", resp.StatusCode)
}

func detectLocalLLMModel(endpoint string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		return ""
	}
	if len(out.Data) > 0 && out.Data[0].ID != "" {
		return out.Data[0].ID
	}
	return ""
}

// MultiEngineProbe 将多个 SearchProbe 包装成一个 SearchProbe：顺序尝试，首个成功即返回
type MultiEngineProbe struct{ probes []SearchProbe }

func (m *MultiEngineProbe) Name() string { return "multi-engine" }

func (m *MultiEngineProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	probes := m.probes
	if len(probes) == 0 {
		probes = NewEngineProbes()
	}
	if len(probes) == 0 {
		return nil, fmt.Errorf("no search probe engine configured: add provider to llm_providers table or set GEO_LOCAL_LLM_URL")
	}
	var lastErr error
	for _, p := range probes {
		r, err := p.Probe(ctx, query)
		if err == nil {
			return r, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all engines failed: %w", lastErr)
}

// NewDefaultSearchProbe 创建默认探针（全部从 DB 配置读取）
func NewDefaultSearchProbe() SearchProbe {
	return &MultiEngineProbe{probes: NewEngineProbes()}
}

// ProbeService 持有 []SearchProbe + ProbeRepository 的聚合服务
type ProbeService struct {
	probes []SearchProbe
	repo   repository.GeoProbeRunRepository
}

// NewProbeService 创建 ProbeService
func NewProbeService(probes []SearchProbe, repo repository.GeoProbeRunRepository) *ProbeService {
	return &ProbeService{probes: probes, repo: repo}
}

// ProbeAllEngines 遍历所有引擎，逐个调用，结果写入 geo_probe_runs
func (s *ProbeService) ProbeAllEngines(ctx context.Context, query string) ([]*model.GeoProbeRun, []error) {
	probes := s.probes
	if len(probes) == 0 {
		probes = NewEngineProbes()
	}
	runs := make([]*model.GeoProbeRun, 0, len(probes))
	errs := []error{}
	for _, p := range probes {
		pr, err := p.Probe(ctx, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("probe %s: %w", p.Name(), err))
			continue
		}
		run := s.buildProbeRun(ctx, p, pr)
		if err := s.repo.Create(ctx, run); err != nil {
			errs = append(errs, fmt.Errorf("persist probe %s: %w", p.Name(), err))
			continue
		}
		runs = append(runs, run)
	}
	return runs, errs
}

// ProbeAllEnginesConcurrent 并发调用所有引擎（慢引擎不再阻塞整轮），结果写入 geo_probe_runs。
// 每个引擎独立失败互不影响；返回值语义与 ProbeAllEngines 一致。
func (s *ProbeService) ProbeAllEnginesConcurrent(ctx context.Context, query string) ([]*model.GeoProbeRun, []error) {
	probes := s.probes
	if len(probes) == 0 {
		probes = NewEngineProbes()
	}
	type probeOutcome struct {
		run *model.GeoProbeRun
		err error
	}
	outcomes := make([]probeOutcome, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p SearchProbe) {
			defer wg.Done()
			pr, err := p.Probe(ctx, query)
			if err != nil {
				outcomes[i] = probeOutcome{err: fmt.Errorf("probe %s: %w", p.Name(), err)}
				return
			}
			outcomes[i] = probeOutcome{run: s.buildProbeRun(ctx, p, pr)}
		}(i, p)
	}
	wg.Wait()

	runs := make([]*model.GeoProbeRun, 0, len(probes))
	errs := []error{}
	for _, oc := range outcomes {
		if oc.err != nil {
			errs = append(errs, oc.err)
			continue
		}
		if err := s.repo.Create(ctx, oc.run); err != nil {
			errs = append(errs, fmt.Errorf("persist probe %s: %w", oc.run.Engine, err))
			continue
		}
		runs = append(runs, oc.run)
	}
	return runs, errs
}

func (s *ProbeService) buildProbeRun(ctx context.Context, p SearchProbe, pr *ProbeResult) *model.GeoProbeRun {
	brandName := s.getBrandName(ctx)
	brandHit := brandName != "" && strings.Contains(strings.ToLower(pr.Response), strings.ToLower(brandName))
	run := &model.GeoProbeRun{
		Engine:         pr.Engine,
		Query:          pr.Query,
		Response:       pr.Response,
		LatencyMs:      pr.LatencyMs,
		Sentiment:      pr.Sentiment,
		BrandMentioned: brandHit,
	}
	if len(pr.Citations) > 0 {
		b, _ := json.Marshal(pr.Citations)
		run.Citations = b
	}
	return run
}

// TestSingle 测试单个引擎
func (s *ProbeService) TestSingle(ctx context.Context, engineName, query string) (*ProbeResult, error) {
	for _, p := range s.probes {
		if engineName == "" || p.Name() == engineName {
			pr, err := p.Probe(ctx, query)
			if err != nil {
				return nil, err
			}
			brandName := s.getBrandName(ctx)
			pr.BrandHit = brandName != "" && strings.Contains(strings.ToLower(pr.Response), strings.ToLower(brandName))

			return pr, nil
		}
	}
	return nil, fmt.Errorf("engine %s not found (available: %s)", engineName, availableEngineNames(s.probes))
}

// ListRuns 返回最近 N 条探针运行记录
func (s *ProbeService) ListRuns(ctx context.Context, limit int) ([]*model.GeoProbeRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListRecent(ctx, limit)
}

// ListRunsByEngine 按引擎分页查询探针运行记录（观测页明细）
func (s *ProbeService) ListRunsByEngine(ctx context.Context, engine string, page, limit int) ([]*model.GeoProbeRun, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if page <= 0 {
		page = 1
	}
	return s.repo.ListByEngine(ctx, engine, page, limit)
}

func availableEngineNames(probes []SearchProbe) string {
	names := make([]string, 0, len(probes))
	for _, p := range probes {
		names = append(names, p.Name())
	}
	return strings.Join(names, ",")
}

func (s *ProbeService) getBrandName(ctx context.Context) string {
	cfgRepo := repository.NewGeoConfigRepository()
	cfg, err := cfgRepo.Get()
	if err != nil || cfg.BrandName == "" {
		return ""
	}
	return cfg.BrandName
}
