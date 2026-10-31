package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/tracing"
)

// LLMModelStat 内存中"按 provider"实时累计（用于 /stats 实时面板）
//
// 该 map 在 Dispatcher 决策日志落库后，仅保留"最近一次统计清点后的增量"。
// 真实历史数据走 llm_routing_logs 表，通过 Stats 端点聚合查询。
type LLMModelStat struct {
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	CallCount    int64     `json:"call_count"`
	SuccessCount int64     `json:"success_count"`
	FailedCount  int64     `json:"failed_count"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	AvgLatencyMs int64     `json:"avg_latency_ms"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

// LLMProviderInfo 暴露给前端的 provider 信息
type LLMProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key,omitempty"`
	APIKeySet   bool   `json:"api_key_set"`
	Enabled     bool   `json:"enabled"`
	// KeyInvalid 库里存的 API Key 用当前主密钥解不开（轮换/多实例不一致），
	// 需要用户重新保存密钥；前端据此打「密钥失效」标记，而不是显示成"未设置"。
	KeyInvalid       bool     `json:"key_invalid"`
	KeyInvalidReason string   `json:"key_invalid_reason,omitempty"`
	QualityScore     float64  `json:"quality_score"`
	MaxRPM           int      `json:"max_rpm"`
	CostPer1k        float64  `json:"cost_per_1k"`
	AvgLatencyMs     int      `json:"avg_latency_ms"`
	NoFC             bool     `json:"no_fc"`
	Vendor           string   `json:"vendor"`
	Tags             []string `json:"tags,omitempty"`

	// Source provider 来源：self=自带 Key（不计费）/ platform=平台垫资（按量计费）
	//
	// 不暴露 source，前端就没法在模型列表上区分"这个用我自己的 Key，不花钱"与
	// "这个走平台、按 token 计费"，用户只能靠猜 —— 而这正是需求五的核心区分。
	// 同一份信息还决定了该不该显示"编辑 API Key"（平台垫资的 Key 不在本地）。
	Source string `json:"source"`
	// SourceAvailable platform 通道当前是否可用（未连接平台时为 false）。
	// 与 Enabled 分开：Enabled 是"用户想不想用"，SourceAvailable 是"用不用得了"。
	SourceAvailable bool `json:"source_available"`
	// PlanCode 平台套餐号（source=platform 时有值），用于查余额与明细
	PlanCode string `json:"plan_code,omitempty"`
	// 生效后的分列单价（已回落），前端成本看板直接展示这两个值即可，
	// 不必在前端复算一遍"没配就回落"的规则 —— 两处各算一次迟早对不上。
	CostPer1kInput  float64 `json:"cost_per_1k_input"`
	CostPer1kOutput float64 `json:"cost_per_1k_output"`
	// SeparatePricing 是否真的启用了分别计价（两列都配且不相等）
	SeparatePricing bool `json:"separate_pricing"`
}

// UpdateStrategiesRequest 批量更新场景路由请求
type UpdateStrategiesRequest struct {
	Routes    []llm.ScenarioRoute `json:"routes"`
	Operator  string              `json:"operator"`
	TraceID   string              `json:"trace_id"`
	CommitMsg string              `json:"commit_msg"`
}

// UsageSummary 用量汇总（按场景维度）
type UsageSummary struct {
	TotalCalls     int64              `json:"total_calls"`
	TotalSuccess   int64              `json:"total_success"`
	TotalFailed    int64              `json:"total_failed"`
	TotalTokens    int64              `json:"total_tokens"`
	TotalCost      float64            `json:"total_cost"`
	ActiveModels   int64              `json:"active_models"`
	EnabledModels  int64              `json:"enabled_models"`
	WindowLabel    string             `json:"window_label"`
	ByScenario     []ScenarioUsage    `json:"by_scenario"`
	ByProvider     []ProviderUsage    `json:"by_provider"`
	ByScenarioProv []llm.ScenarioStat `json:"by_scenario_provider"`
}

// ScenarioUsage 场景维度用量
type ScenarioUsage struct {
	Scenario    string  `json:"scenario"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// ProviderUsage provider 维度用量
type ProviderUsage struct {
	Provider    string  `json:"provider"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// TestModelRequest 测试模型请求
type TestModelRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Timeout  int    `json:"timeout_seconds"`
}

// TestModelResult 测试模型结果
type TestModelResult struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Content     string  `json:"content"`
	LatencyMs   int64   `json:"latency_ms"`
	TotalTokens int     `json:"total_tokens"`
	Cost        float64 `json:"cost"`
	Success     bool    `json:"success"`
	Error       string  `json:"error,omitempty"`
}

// LLMRoutingService LLM 路由服务
type LLMRoutingService struct {
	dispatcher *llm.Dispatcher

	mu    sync.Mutex
	stats map[string]*LLMModelStat
}

// NewLLMRoutingService 创建 LLM 路由服务
func NewLLMRoutingService(d *llm.Dispatcher) *LLMRoutingService {
	return &LLMRoutingService{
		dispatcher: d,
		stats:      make(map[string]*LLMModelStat),
	}
}

func isLocalURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "[::1]") || strings.Contains(lower, "0.0.0.0")
}

// ListModels 列出所有 provider（含本地）
func (s *LLMRoutingService) ListModels(ctx context.Context) []LLMProviderInfo {
	if s.dispatcher == nil {
		return nil
	}
	providers := s.dispatcher.GetAllProviders()
	out := make([]LLMProviderInfo, 0, len(providers))
	for _, p := range providers {
		keyInvalidReason, keyInvalid := llm.KeyInvalidReason(p.Name)
		out = append(out, LLMProviderInfo{
			Name:             p.Name,
			DisplayName:      p.DisplayName,
			BaseURL:          p.BaseURL,
			Model:            p.Model,
			APIKeySet:        p.APIKey != "",
			Enabled:          p.Enabled,
			KeyInvalid:       keyInvalid,
			KeyInvalidReason: keyInvalidReason,
			QualityScore:     p.QualityScore,
			MaxRPM:           p.MaxRPM,
			CostPer1k:        p.CostPer1k,
			AvgLatencyMs:     p.AvgLatencyMs,
			NoFC:             p.NoFC,
			Vendor:           vendorOf(p),
			Tags:             p.Tags,

			Source:          p.ProviderSource(),
			SourceAvailable: !p.IsPlatformProvider() || llm.IsPlatformProxyConfigured(),
			PlanCode:        p.PlanCode,
			CostPer1kInput:  p.EffectiveInputCostPer1k(),
			CostPer1kOutput: p.EffectiveOutputCostPer1k(),
			SeparatePricing: p.HasSeparatePricing(),
		})
	}
	return out
}

// ListCloudModels 只返回 enabled 的云端 provider（排除本地推理），GEO 模块使用
func (s *LLMRoutingService) ListCloudModels(ctx context.Context) []LLMProviderInfo {
	all := s.ListModels(ctx)
	out := make([]LLMProviderInfo, 0, len(all))
	for _, p := range all {
		if !p.Enabled {
			continue
		}
		if isLocalURL(p.BaseURL) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ResolveProviderName 将路径参数（可能是 provider Name 或 Model）解析为实际的 provider Name。
// 优先按 Name 精确匹配，其次按 Model 精确匹配。未找到返回空字符串。
// 这支撑前端 /api/llm/models/:name 端点同时接受 provider name（如 "deepseek"）和 model name（如 "gpt-4o"）。
func (s *LLMRoutingService) ResolveProviderName(name string) string {
	if s.dispatcher == nil || name == "" {
		return ""
	}
	if s.dispatcher.GetProvider(name) != nil {
		return name
	}
	for _, p := range s.dispatcher.GetAllProviders() {
		if p.Model == name {
			return p.Name
		}
	}
	return ""
}

// CreateModel 注册新 provider（内存 + 落库 llm_providers，重启不丢）
func (s *LLMRoutingService) CreateModel(ctx context.Context, info LLMProviderInfo) error {
	if s.dispatcher == nil {
		return errors.New("dispatcher not initialized")
	}
	if info.Name == "" {
		return errors.New("name is required")
	}
	if info.BaseURL == "" {
		return errors.New("base_url is required")
	}
	if info.Model == "" {
		return errors.New("model is required")
	}
	if info.QualityScore <= 0 {
		info.QualityScore = 0.8
	}
	if info.MaxRPM <= 0 {
		info.MaxRPM = 60
	}
	pc := llm.ProviderConfig{
		Name:            info.Name,
		DisplayName:     info.DisplayName,
		BaseURL:         info.BaseURL,
		Model:           info.Model,
		APIKey:          info.APIKey,
		APIType:         "openai",
		Enabled:         info.Enabled,
		QualityScore:    info.QualityScore,
		MaxRPM:          info.MaxRPM,
		CostPer1k:       info.CostPer1k,
		CostPer1kInput:  info.CostPer1kInput,
		CostPer1kOutput: info.CostPer1kOutput,
		NoFC:            info.NoFC,
		Vendor:          info.Vendor,
		Tags:            info.Tags,
		// 商户建 provider 恒为 self，**忽略入参里的 source**。
		//
		// 理由：source=platform 的 provider 是平台下发的、Key 不在商户侧。
		// 若允许商户通过这个接口声明自己是 platform，它的调用会被路由到平台
		// 代理，而平台上并没有同名 provider —— 每次调用都 404，且商户完全
		// 无法从界面上看出是自己声明错了。
		Source: "self",
	}
	s.dispatcher.AddProvider(pc)
	if err := s.dispatcher.UpsertProviderToDB(pc); err != nil {
		return fmt.Errorf("provider 落库失败: %w", err)
	}
	s.dispatcher.LogModelLifecycle(ctx,
		"create_model", info.Name, operatorFromContext(ctx), tracing.TraceIDFromContext(ctx))
	return nil
}

// APIKeyClearSentinel API Key 清空标记
//
// 前端表单"留空表示不修改"语义：传 "" 保留原 key。
// 如需真正清空（删除 provider 的 API Key），前端传此特殊字符串。
const APIKeyClearSentinel = "__CLEAR_API_KEY__"

// UpdateModel 更新 provider
//
// 注意：identifier 语义为 provider name（与前端约定保持一致）。
// 返回旧 provider 信息供前端做 before/after 对比。
// 字段语义：
//   - BaseURL/Model: 空字符串表示保留原值
//   - APIKey:        空字符串保留原值；APIKeyClearSentinel 表示清空；其他覆盖
//   - 其他字段:      按需更新
func (s *LLMRoutingService) UpdateModel(ctx context.Context, identifier string, info LLMProviderInfo) error {
	if s.dispatcher == nil {
		return errors.New("dispatcher not initialized")
	}
	if identifier == "" {
		return errors.New("name is required")
	}
	old := s.dispatcher.GetProvider(identifier)
	if old == nil {
		return fmt.Errorf("provider %q not found", identifier)
	}
	apiKey := old.APIKey
	switch {
	case info.APIKey == APIKeyClearSentinel:
		apiKey = ""
	case info.APIKey != "":
		apiKey = info.APIKey
	}
	updated := llm.ProviderConfig{
		Name:         identifier,
		DisplayName:  orDefault(info.DisplayName, old.DisplayName),
		BaseURL:      orDefault(info.BaseURL, old.BaseURL),
		Model:        orDefault(info.Model, old.Model),
		APIKey:       apiKey,
		APIType:      old.APIType,
		Enabled:      info.Enabled,
		QualityScore: nonZero(info.QualityScore, old.QualityScore),
		MaxRPM:       nonZeroInt(info.MaxRPM, old.MaxRPM),
		// 零值=保留原值：前端开关切换只发 {enabled}，不能把成本/NoFC/tags 打掉
		CostPer1k: nonZero(info.CostPer1k, old.CostPer1k),
		// 分列单价同样按「零值=保留原值」处理，与 CostPer1k 同一口径
		CostPer1kInput:  nonZero(info.CostPer1kInput, old.CostPer1kInput),
		CostPer1kOutput: nonZero(info.CostPer1kOutput, old.CostPer1kOutput),
		NoFC:            info.NoFC || old.NoFC,
		Vendor:          orDefault(info.Vendor, old.Vendor),
		Tags:            nonEmptyTags(info.Tags, old.Tags),

		// **source / planCode 一律沿用旧值，不接受入参覆盖。**
		//
		// 这不是保守，是必须的：前端开关切换只发 {enabled: true|false}，
		// 若这里回落成空串，ProviderSource() 会判成 self —— 一个平台下发、
		// 本地无 Key 的 provider 就此变成"自带 Key"，此后每次调用必然 401。
		// 而用户做的事只是点了一下"启用"，界面上没有任何异常提示。
		Source:   old.Source,
		PlanCode: old.PlanCode,
	}
	s.dispatcher.AddProvider(updated)
	if err := s.dispatcher.UpsertProviderToDB(updated); err != nil {
		return fmt.Errorf("provider 落库失败: %w", err)
	}
	return nil
}

// DeleteModel 删除 provider
//
// 行为：
//   - 注销 provider
//   - 清理内存 stats
//   - 审计：记录删除动作
func (s *LLMRoutingService) DeleteModel(ctx context.Context, identifier string) error {
	if s.dispatcher == nil {
		return errors.New("dispatcher not initialized")
	}
	if identifier == "" {
		return errors.New("name is required")
	}
	if identifier == "default" {
		return errors.New("本地默认模型 default 不可删除")
	}
	if s.dispatcher.GetProvider(identifier) == nil {
		return fmt.Errorf("provider %q not found", identifier)
	}
	if !s.dispatcher.RemoveProvider(identifier) {
		return fmt.Errorf("provider %q remove failed", identifier)
	}
	if err := s.dispatcher.DeleteProviderFromDB(identifier); err != nil {
		return fmt.Errorf("provider 落库删除失败: %w", err)
	}
	s.mu.Lock()
	delete(s.stats, identifier)
	s.mu.Unlock()
	s.dispatcher.LogModelLifecycle(ctx,
		"delete_model", identifier, operatorFromContext(ctx), tracing.TraceIDFromContext(ctx))
	return nil
}

// ListStrategies 列出所有场景路由
func (s *LLMRoutingService) ListStrategies(ctx context.Context) []llm.ScenarioRoute {
	if s.dispatcher == nil {
		return nil
	}
	return s.dispatcher.GetAllRoutes()
}

// UpdateStrategies 批量更新场景路由
//
// 走 SetRouteWithAudit：自动版本号自增 + 审计日志。
// 兼容前端 List.vue 4 种调用形式（routes 数组 / 单个 route）。
func (s *LLMRoutingService) UpdateStrategies(ctx context.Context, req UpdateStrategiesRequest) error {
	if s.dispatcher == nil {
		return errors.New("dispatcher not initialized")
	}
	operator := orDefault(req.Operator, operatorFromContext(ctx))
	traceID := orDefault(req.TraceID, tracing.TraceIDFromContext(ctx))
	if len(req.Routes) == 0 {
		return errors.New("routes is required")
	}
	for _, r := range req.Routes {
		if r.Scenario == "" {
			return errors.New("scenario is required")
		}
		if r.Provider == "" {
			return fmt.Errorf("scenario %q: provider is required", r.Scenario)
		}
		if r.Weight > 0 && r.Weight < 100 && r.CanaryRoute != nil {
			if r.CanaryRoute.Provider == "" {
				return fmt.Errorf("scenario %q: canary route provider is required", r.Scenario)
			}
		}
		s.dispatcher.SetRouteWithAudit(ctx, r, "update_strategy", operator, traceID)
	}
	return nil
}

// ListAuditHistory 查路由变更审计历史
func (s *LLMRoutingService) ListAuditHistory(ctx context.Context, scenario string, limit int) ([]map[string]any, error) {
	return llm.QueryAuditHistory(ctx, scenario, limit)
}

// Stats 返回进程内实时 provider 维度统计
func (s *LLMRoutingService) Stats(ctx context.Context) map[string]LLMModelStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]LLMModelStat, len(s.stats))
	for k, v := range s.stats {
		out[k] = *v
	}
	return out
}

// Usage 跨进程历史统计（按 window 维度走 llm_routing_logs 聚合）
//
// window: today / week / month / all
func (s *LLMRoutingService) Usage(ctx context.Context, window string) (*UsageSummary, error) {
	if window == "" {
		window = "all"
	}
	enabledProviders := llm.GetGlobalDispatcher().GetEnabledProviderNames()
	stats, err := llm.QueryScenarioStats(ctx, window, 200, enabledProviders)
	if err != nil {
		return nil, err
	}
	summary := &UsageSummary{
		WindowLabel:    window,
		ByScenarioProv: stats,
	}
	scenarioMap := make(map[string]*ScenarioUsage)
	providerMap := make(map[string]*ProviderUsage)
	providerEnabled := make(map[string]bool)
	for _, st := range stats {
		summary.TotalCalls += st.CallCount
		summary.TotalSuccess += st.SuccessCount
		summary.TotalFailed += st.FailedCount
		summary.TotalTokens += st.TotalTokens
		summary.TotalCost += st.TotalCost

		if _, ok := scenarioMap[st.Scenario]; !ok {
			scenarioMap[st.Scenario] = &ScenarioUsage{Scenario: st.Scenario}
		}
		sc := scenarioMap[st.Scenario]
		sc.CallCount += st.CallCount
		sc.TotalTokens += st.TotalTokens
		sc.TotalCost += st.TotalCost

		if _, ok := providerMap[st.Provider]; !ok {
			providerMap[st.Provider] = &ProviderUsage{Provider: st.Provider}
		}
		pr := providerMap[st.Provider]
		pr.CallCount += st.CallCount
		pr.TotalTokens += st.TotalTokens
		pr.TotalCost += st.TotalCost
		providerEnabled[st.Provider] = true
	}
	for _, sc := range scenarioMap {
		summary.ByScenario = append(summary.ByScenario, *sc)
	}
	for _, pr := range providerMap {
		summary.ByProvider = append(summary.ByProvider, *pr)
	}
	if s.dispatcher != nil {
		providers := s.dispatcher.GetAllProviders()
		for _, p := range providers {
			if p.Enabled {
				summary.EnabledModels++
			}
		}
		summary.ActiveModels = int64(len(providers))
	}
	return summary, nil
}

// TestModel 测试模型连通性（走独立路径，不污染全局路由/告警/熔断）
//
// 关键设计：
//   - 不动 routes map（避免破坏并发安全）
//   - 不调 dispatcher.Dispatch（避免触发告警、熔断、健康度累计）
//   - 直接通过 dispatcher.callProvider 走单次 provider 调用
//   - 用专属 timeout（默认 60s），避免被 route.MaxLatency 截断
//
// 返回：TestModelResult 含 Content/LatencyMs/Tokens/Cost/Success/Error
func (s *LLMRoutingService) TestModel(ctx context.Context, req TestModelRequest) (*TestModelResult, error) {
	if s.dispatcher == nil {
		return nil, errors.New("dispatcher not initialized")
	}
	if req.Provider == "" {
		return nil, errors.New("provider is required")
	}
	provider := s.dispatcher.GetProvider(req.Provider)
	if provider == nil {
		return nil, fmt.Errorf("provider %q not found", req.Provider)
	}
	if req.Prompt == "" {
		req.Prompt = "你好，请用一句话自我介绍。"
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	testRoute := &llm.ScenarioRoute{
		Scenario:   llm.ScenarioLowCost,
		Provider:   req.Provider,
		MaxLatency: timeout * 1000,
		MinQuality: 0,
	}
	dreq := llm.DispatchRequest{
		Scenario:    llm.ScenarioLowCost,
		Prompt:      req.Prompt,
		MaxTokens:   256,
		Temperature: 0.7,
		CanaryKey:   "_test_only_" + tracing.TraceIDFromContext(ctx),
	}
	result, err := s.dispatcher.CallProviderForTest(tCtx, provider, dreq, testRoute)
	res := &TestModelResult{
		Provider: req.Provider,
		Model:    provider.Model,
	}
	if err != nil {
		res.Success = false
		res.Error = err.Error()
		return res, nil
	}
	res.Content = result.Content
	res.LatencyMs = int64(result.LatencyMs)
	res.TotalTokens = result.Usage.TotalTokens
	res.Cost = result.Cost
	res.Success = true
	return res, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func nonZero(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

func nonZeroInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// nonEmptyTags 空 tags=保留原值（前端开关切换只发 {enabled}，不得把 tags 打掉）
func nonEmptyTags(v, def []string) []string {
	if len(v) == 0 {
		return def
	}
	return v
}

func vendorOf(p llm.ProviderConfig) string {
	if p.Vendor != "" {
		return p.Vendor
	}
	return vendorFromBaseURL(p.BaseURL)
}

func vendorFromBaseURL(base string) string {
	low := strings.ToLower(base)
	switch {
	case strings.Contains(low, "deepseek"):
		return "deepseek"
	case strings.Contains(low, "dashscope"), strings.Contains(low, "qwen"):
		return "qwen"
	case strings.Contains(low, "openai"), strings.Contains(low, "gpt"):
		return "openai"
	case strings.Contains(low, "zhipu"), strings.Contains(low, "glm"):
		return "zhipu"
	case strings.Contains(low, "moonshot"), strings.Contains(low, "kimi"):
		return "moonshot"
	case strings.Contains(low, "127.0.0.1"), strings.Contains(low, "localhost"), strings.Contains(low, "mtk-llm"):
		return "local"
	default:
		return "other"
	}
}

func operatorFromContext(ctx context.Context) string {
	if v := ctx.Value("operator"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v := ctx.Value("user_id"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "system"
}

// DailyUsage 按日用量聚合（P1-4 模型管理面）。透传 llm.QueryDailyUsage，
// window/groupBy 口径见该函数注释。
func (s *LLMRoutingService) DailyUsage(ctx context.Context, window, groupBy string) ([]llm.DailyUsagePoint, error) {
	return llm.QueryDailyUsage(ctx, window, groupBy, 120)
}
