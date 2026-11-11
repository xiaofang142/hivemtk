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

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	hivemodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/platform"
	mainrepo "hivemtk-user/internal/repository"
)

var urlRegexp = regexp.MustCompile(`https?://[^\s"'\)\]\}\>,;]+`)

// Citation 单条被引信源
type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// 探针数据来源。UI 与报告必须按此标注，禁止把 simulated 写成 real。
const (
	ProbeModeSimulated = "simulated"
	ProbeModeReal      = "real"
	ProbeModeManual    = "manual"
)

// ProbeResult 单次探针结果
type ProbeResult struct {
	Engine    string     `json:"engine"`
	Mode      string     `json:"mode"`
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
	defer func() { _ = resp.Body.Close() }()
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

func probeSystemPrompt(brandHint string) string {
	brandCtx := ""
	if brandHint != "" {
		brandCtx = fmt.Sprintf("\n【品牌上下文】%s。回答中涉及此品牌时请以此描述为准，不要和同名的其他项目混淆。", brandHint)
	}
	return `你是 AI 搜索答案专家。请以事实为依据回答用户的搜索查询。
要求：
1. 直接给出有用的回答（200-400字），不要说"作为AI我无法"之类的套话
2. 尽量引用具体信息源（网站、博客、文档链接等），如果不确定就说"据公开资料"
3. 如果涉及具体产品/品牌，客观陈述其定位和特点
4. 输出中自然出现"信息来源"或"参考链接"小节，列出 1-3 条你所知的可靠链接
5. 回答最后单独输出一行情感标签，格式严格为 [SENTIMENT: positive] 或 [SENTIMENT: neutral] 或 [SENTIMENT: negative]，三选一。判断标准：你的回答整体态度——如果在为品牌辩护、说没负面、没证据=positive；只是客观陈述=neutral；确认有真实差评/投诉/负面新闻=negative` + brandCtx
}

func (p *llmEndpointProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	start := time.Now()
	systemPrompt := probeSystemPrompt(p.brandHint)
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
		Engine: p.Name(), Mode: ProbeModeSimulated, Query: query, Response: content,
		Citations: cites, LatencyMs: latency, Simulated: true, Sentiment: sentiment,
	}, nil
}

func extractCitationsFromText(text string) []Citation {
	var cites []Citation
	for _, m := range urlRegexp.FindAllString(text, -1) {
		cites = append(cites, Citation{URL: m})
	}
	return cites
}

// platformProxyProbe 通过平台 LLM 代理调用平台垫资模型。
//
// 平台 provider 的 api_key 永不下发，BaseURL 也为空，不能走 llmEndpointProbe；
// 这条路径让 GEO 探针在自有 Key 不可用时仍能使用平台余额，扣费记在当前租户。
type platformProxyProbe struct {
	name      string
	model     string
	brandHint string
}

func (p *platformProxyProbe) Name() string {
	if p.name != "" {
		return p.name
	}
	return "platform-llm"
}

func (p *platformProxyProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	start := time.Now()
	client := platform.NewLLMProxyClient()
	if !client.Available() {
		return nil, fmt.Errorf("%s: 平台代理未配置", p.Name())
	}
	body, err := json.Marshal(map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "system", "content": probeSystemPrompt(p.brandHint)},
			{"role": "user", "content": query},
		},
		"temperature": 0.3,
		"max_tokens":  1024,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", p.Name(), err)
	}
	resp, err := client.ChatCompletions(ctx, p.name, body, platform.LLMCallMeta{
		Scenario:   "geo_probe",
		TenantCode: platform.TenantCodeFromContext(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: 平台代理 status=%d: %s", p.Name(), resp.StatusCode, truncateForProbeError(resp.Body))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", p.Name(), err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty response", p.Name())
	}
	content := out.Choices[0].Message.Content
	sentiment := "neutral"
	if m := sentimentRegexp.FindStringSubmatch(content); len(m) == 2 {
		sentiment = m[1]
		content = strings.TrimSpace(sentimentRegexp.ReplaceAllString(content, ""))
	}
	return &ProbeResult{
		Engine: p.Name(), Mode: ProbeModeSimulated, Query: query, Response: content,
		Citations: extractCitationsFromText(content), LatencyMs: time.Since(start).Milliseconds(),
		Simulated: true, Sentiment: sentiment,
	}, nil
}

func truncateForProbeError(body []byte) string {
	const max = 300
	s := string(body)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// NewEngineProbes 装配所有可用真实引擎探针（统一从 DB 配置读取）。
// 优先级：DB 中 enabled=true 的 provider 在前，本地 LLM 兜底在后。
// 环境变量仅作为补充（兼容老部署），不再硬编码任何 endpoint/model。
func NewEngineProbes() []SearchProbe {
	// 兼容旧调用：无租户上下文时按平台级读全量（system scope）。新代码必须用
	// NewEngineProbesForContext(ctx)（带请求/任务租户），否则会把 A 的
	// provider 与品牌提示词用在 B 的探针上。
	return NewEngineProbesFromDB(tenant.WithSystemScope(context.Background()),
		mainrepo.NewLLMProviderRepository(), repository.NewGeoConfigRepository())
}

// NewEngineProbesForContext 按 ctx 绑定的租户装配探针引擎。
// ctx 无租户（多租户模式）时返回空清单：宁可无探针，也不跨租户取凭据。
func NewEngineProbesForContext(ctx context.Context) []SearchProbe {
	if _, ok := tenant.FromContext(ctx); !ok && tenant.IsMultiTenant() {
		return nil
	}
	return NewEngineProbesFromDB(ctx, mainrepo.NewLLMProviderRepository(), repository.NewGeoConfigRepository())
}

// ProbeResolver 在调用时按租户解析探针清单。
type ProbeResolver func(context.Context) []SearchProbe

// NewEngineProbesFromDB 从 llm_providers 表装配探针引擎。
//
// 这是唯一的探针装配入口。所有 provider 配置（endpoint/model/api_key）
// 都从 DB 读取，不再硬编码。运维在后台改 llm_providers 表即可热切换探针引擎。
//
// AV-04c 批次3：原 *gorm.DB 形参改仓储注入——llm_providers 名单经
// mainrepo.LLMProviderRepository.ListEnabled，品牌提示词两列投影经
// GeoConfigRepository.GetBrandProfile（查询契约逐项保持，见两方法注释）。
//
// 注意：探针引擎只使用云端 provider（排除 localhost / 127.0.0.1 本地 LLM），
// 避免本地 LLM 速度慢、质量低影响 GEO 验证准确性。本地 LLM 仅在 LLMAdapter
// 的 fallback 链中作为兜底。
func NewEngineProbesFromDB(ctx context.Context, providerRepo *mainrepo.LLMProviderRepository, cfgRepo repository.GeoConfigRepository) []SearchProbe {
	probes := []SearchProbe{}
	seen := make(map[string]bool)

	brandHint := ""
	if cfgRepo != nil {
		if cfg, err := cfgRepo.GetBrandProfile(ctx); err == nil && cfg != nil {
			if cfg.BrandName != "" && cfg.BrandDescription != "" {
				brandHint = cfg.BrandName + "：" + cfg.BrandDescription
			} else if cfg.BrandName != "" {
				brandHint = cfg.BrandName
			}
		}
	}

	if providerRepo != nil {
		rows, err := providerRepo.ListEnabled(ctx)
		if err == nil {
			for _, row := range rows {
				if row.Model == "" {
					continue
				}
				if row.Source == hivemodel.LLMSourcePlatform {
					if seen[row.Name] || !platform.NewLLMProxyClient().Available() {
						continue
					}
					seen[row.Name] = true
					probes = append(probes, &platformProxyProbe{
						name:      row.Name,
						model:     row.Model,
						brandHint: brandHint,
					})
					continue
				}
				if row.BaseURL == "" {
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
				apiKey, invalid := llm.DecryptProviderAPIKeyForUse(row.Name, row.APIKey)
				if invalid {
					// 密文解不开时跳过，避免把 enc:v1:... 当 Bearer 发出去再收一个 401。
					continue
				}
				seen[row.Name] = true
				lowerName := strings.ToLower(row.Name)
				lowerURL := strings.ToLower(row.BaseURL)
				if strings.Contains(lowerName, "perplexity") || strings.Contains(lowerURL, "perplexity") {
					probes = append(probes, &perplexityProbe{
						name:      row.Name,
						endpoint:  row.BaseURL,
						model:     row.Model,
						apiKey:    apiKey,
						brandHint: brandHint,
					})
					continue
				}
				probes = append(probes, &llmEndpointProbe{
					name:      row.Name,
					endpoint:  row.BaseURL,
					model:     row.Model,
					apiKey:    apiKey,
					brandHint: brandHint,
				})
			}
		}
	}
	return probes
}

// MultiEngineProbe 将多个 SearchProbe 包装成一个 SearchProbe：顺序尝试，首个成功即返回
type MultiEngineProbe struct {
	probes   []SearchProbe
	resolver ProbeResolver
}

func (m *MultiEngineProbe) Name() string { return "multi-engine" }

func (m *MultiEngineProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	probes := m.probes
	if m.resolver != nil {
		probes = m.resolver(ctx)
	} else if len(probes) == 0 {
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
	return &MultiEngineProbe{resolver: NewEngineProbesForContext}
}

// ProbeService 持有 []SearchProbe + ProbeRepository 的聚合服务
type ProbeService struct {
	probes   []SearchProbe
	resolver ProbeResolver
	repo     repository.GeoProbeRunRepository
}

// probeAttemptBackoff 探针重试退避间隔（指数退避：2s、4s）
var probeRetryBackoffs = []time.Duration{2 * time.Second, 4 * time.Second}

// nonRetryableProbeError 凭证类错误（401/403）不重试，重试无意义
func nonRetryableProbeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403")
}

// probeWithRetry 带重试的单引擎探针（对标 gego 失败3次重试）
// 最多尝试 1+len(backoffs) 次；偶发网络/网关错误自动重试，凭证错误直接返回。
func probeWithRetry(ctx context.Context, p SearchProbe, query string) (*ProbeResult, error) {
	pr, err := p.Probe(ctx, query)
	if err == nil || nonRetryableProbeError(err) {
		return pr, err
	}
	for i, wait := range probeRetryBackoffs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		pr, err = p.Probe(ctx, query)
		if err == nil {
			return pr, nil
		}
		if nonRetryableProbeError(err) {
			return nil, err
		}
		_ = i
	}
	return nil, err
}

// matchBrandNames 响应命中任一品牌名/别名即算提及（大小写不敏感），
// HiveMtk/微蜂/HiveMTK 等写法统一识别。
func matchBrandNames(response string, names []string) bool {
	lr := strings.ToLower(response)
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.Contains(lr, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// NewProbeService 创建 ProbeService
func NewProbeService(probes []SearchProbe, repo repository.GeoProbeRunRepository) *ProbeService {
	return &ProbeService{probes: probes, repo: repo}
}

// SetProbeResolver 装配期注入按租户解析器（生产路径）。未注入时退回静态清单，
// 保持测试与单租户离线模式的兼容。
func (s *ProbeService) SetProbeResolver(r ProbeResolver) { s.resolver = r }

func (s *ProbeService) probesFor(ctx context.Context) []SearchProbe {
	if s.resolver != nil {
		return s.resolver(ctx)
	}
	if len(s.probes) > 0 {
		return s.probes
	}
	return NewEngineProbesForContext(ctx)
}

// AvailableEngines 当前租户可用的引擎清单（供 controller 展示）。
func (s *ProbeService) AvailableEngines(ctx context.Context) []SearchProbe {
	return s.probesFor(ctx)
}

// MaxProbeSampleRounds 单次探针最多采样轮数（对标 Auriti citations --runs 置信区间；
// 上限 5 轮防止 LLM 成本爆炸，默认 1 轮保持现有行为）
const MaxProbeSampleRounds = 5

// SampleSummary 多轮采样汇总：提及率 + 样本数即置信展示
type SampleSummary struct {
	Rounds           int     `json:"rounds"`
	TotalRuns        int     `json:"total_runs"`
	BrandHits        int     `json:"brand_hits"`
	BrandMentionRate float64 `json:"brand_mention_rate"`
}

// ProbeAllEnginesSampled 多轮采样：同 query 跑 rounds 轮，返回全部 runs 与汇总。
// rounds 越界时钳制到 [1, MaxProbeSampleRounds]。
func (s *ProbeService) ProbeAllEnginesSampled(ctx context.Context, query string, rounds int) ([]*model.GeoProbeRun, []error, SampleSummary) {
	if rounds < 1 {
		rounds = 1
	}
	if rounds > MaxProbeSampleRounds {
		rounds = MaxProbeSampleRounds
	}
	allRuns := []*model.GeoProbeRun{}
	allErrs := []error{}
	for r := 0; r < rounds; r++ {
		if err := ctx.Err(); err != nil {
			allErrs = append(allErrs, err)
			break
		}
		runs, errs := s.ProbeAllEnginesConcurrent(ctx, query)
		allRuns = append(allRuns, runs...)
		allErrs = append(allErrs, errs...)
	}
	hits := 0
	for _, run := range allRuns {
		if run.BrandMentioned {
			hits++
		}
	}
	summary := SampleSummary{Rounds: rounds, TotalRuns: len(allRuns), BrandHits: hits}
	if len(allRuns) > 0 {
		summary.BrandMentionRate = float64(hits) / float64(len(allRuns))
	}
	return allRuns, allErrs, summary
}

// ProbeAllEngines 遍历所有引擎，逐个调用，结果写入 geo_probe_runs
func (s *ProbeService) ProbeAllEngines(ctx context.Context, query string) ([]*model.GeoProbeRun, []error) {
	probes := s.probesFor(ctx)
	runs := make([]*model.GeoProbeRun, 0, len(probes))
	errs := []error{}
	for _, p := range probes {
		pr, err := probeWithRetry(ctx, p, query)
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
	probes := s.probesFor(ctx)
	type probeOutcome struct {
		run *model.GeoProbeRun
		err error
	}
	outcomes := make([]probeOutcome, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		// utils.SafeGo 收口（Go 1.22+ 循环变量逐迭代语义下闭包捕获与原参数传递等价）：
		// 探针重试/落库链 panic 不击穿进程
		utils.SafeGo(ctx, "geo.searchProbe.run", func(ctx context.Context) {
			defer wg.Done()
			pr, err := probeWithRetry(ctx, p, query)
			if err != nil {
				outcomes[i] = probeOutcome{err: fmt.Errorf("probe %s: %w", p.Name(), err)}
				return
			}
			outcomes[i] = probeOutcome{run: s.buildProbeRun(ctx, p, pr)}
		})
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
	brandHit := matchBrandNames(pr.Response, s.getBrandNames(ctx))
	run := &model.GeoProbeRun{
		Engine:         pr.Engine,
		Mode:           probeModeOrDefault(pr.Mode),
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

func probeModeOrDefault(mode string) string {
	switch mode {
	case ProbeModeReal, ProbeModeManual:
		return mode
	default:
		return ProbeModeSimulated
	}
}

// TestSingle 测试单个引擎
func (s *ProbeService) TestSingle(ctx context.Context, engineName, query string) (*ProbeResult, error) {
	probes := s.probesFor(ctx)
	for _, p := range probes {
		if engineName == "" || p.Name() == engineName {
			pr, err := probeWithRetry(ctx, p, query)
			if err != nil {
				return nil, err
			}
			pr.BrandHit = matchBrandNames(pr.Response, s.getBrandNames(ctx))

			return pr, nil
		}
	}
	return nil, fmt.Errorf("engine %s not found (available: %s)", engineName, availableEngineNames(probes))
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
	cfg, err := cfgRepo.Get(ctx)
	if err != nil || cfg.BrandName == "" {
		return ""
	}
	return cfg.BrandName
}

// getBrandNames 品牌名全集：配置主名 + 实体别名（对标 elmo/gego 品牌别名归一）
// mention 判定用全集匹配，"微蜂/HiveMTK" 等别名写法同样识别为提及。
func (s *ProbeService) getBrandNames(ctx context.Context) []string {
	names := []string{}
	seen := map[string]bool{}
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		k := strings.ToLower(n)
		if !seen[k] {
			seen[k] = true
			names = append(names, n)
		}
	}
	add(s.getBrandName(ctx))
	entRepo := repository.NewGeoEntityRepository()
	ents, _, err := entRepo.List(ctx, "", "", 1, 500)
	if err != nil {
		return names
	}
	for _, e := range ents {
		add(e.Name)
		var aliases []string
		if len(e.Aliases) > 0 {
			if err := json.Unmarshal(e.Aliases, &aliases); err == nil {
				for _, a := range aliases {
					add(a)
				}
			}
		}
	}
	return names
}
