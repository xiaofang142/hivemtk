package llm

import (
	"context"

	"strings"

	"hivemtk-user/internal/config"

	"hivemtk-user/internal/pkg/utils/logger"
)

func newDispatcherBase(llmService *LLMService) *Dispatcher {
	return &Dispatcher{
		providers:  make(map[string]*ProviderConfig),
		routes:     make(map[DispatchScenario]*ScenarioRoute),
		llmService: llmService,
		rpmCounter: make(map[string]*rpmBucket),
	}
}

func (d *Dispatcher) getReActAdapter() *ReActAdapter {
	d.reactAdapterMu.Do(func() {
		d.reactAdapter = NewReActAdapter()
	})
	return d.reactAdapter
}

func (d *Dispatcher) registerLocalProvider(llmCfg config.InferenceLLMConfig) {
	if llmCfg.Model == "" {
		panic("inference.llm.model 必须由 config 层提供（config.yaml 或 DefaultInferenceConfig），dispatcher 不再兜底")
	}
	if llmCfg.BaseURL == "" {
		panic("inference.llm.base_url 必须由 config 层提供（config.yaml 或 DefaultInferenceConfig），dispatcher 不再兜底")
	}
	d.providers["default"] = &ProviderConfig{
		Name:         "default",
		APIKey:       llmCfg.APIKey,
		BaseURL:      llmCfg.BaseURL,
		APIType:      "openai",
		Model:        llmCfg.Model,
		CostPer1k:    0,
		AvgLatencyMs: 800,

		QualityScore: 0.99,
		MaxRPM:       0,
		Enabled:      true,

		NoFC: resolveNoFC(llmCfg),
	}
}

func resolveNoFC(llmCfg config.InferenceLLMConfig) bool {

	if llmCfg.NoFC != nil {
		return *llmCfg.NoFC
	}

	base := strings.ToLower(llmCfg.BaseURL)
	return base == "" || strings.Contains(base, "127.0.0.1") || strings.Contains(base, "localhost") || strings.Contains(base, "mtk-llm")
}

func defaultCloudProviderFactories() []ProviderConfig {
	return []ProviderConfig{
		{
			Name:         "deepseek",
			BaseURL:      "https://api.deepseek.com",
			APIType:      "openai",
			Model:        "deepseek-chat",
			CostPer1k:    0.001,
			AvgLatencyMs: 1500,
			QualityScore: 0.85,
			MaxRPM:       60,
			Enabled:      false,
		},
	}
}

func (d *Dispatcher) registerCloudProvidersFromConfig(llmCfg config.InferenceLLMConfig) {

	for _, p := range defaultCloudProviderFactories() {
		d.providers[p.Name] = &p
	}

	src := llmCfg.CloudProviders
	for _, p := range src {
		enabled := p.Enabled && p.APIKey != ""
		d.providers[p.Name] = &ProviderConfig{
			Name:         p.Name,
			APIKey:       p.APIKey,
			BaseURL:      p.BaseURL,
			APIType:      p.APIType,
			Model:        p.Model,
			CostPer1k:    0.01,
			AvgLatencyMs: 2000,

			QualityScore: 0.96,
			MaxRPM:       60,
			Enabled:      enabled,
		}
	}
}

func (d *Dispatcher) registerLocalFirstRoutes(primary string, maxLatencyMs int) {
	if maxLatencyMs <= 0 {
		maxLatencyMs = 180000
	}

	prim := "default"
	fallback := []string{}
	if primary != "" && primary != "default" {
		prim = primary
		fallback = []string{"default"}
	}
	routes := []*ScenarioRoute{
		{Scenario: ScenarioIntentRecognize, Provider: prim, Fallbacks: fallback, CostWeight: 5, MaxLatency: maxLatencyMs, MinQuality: 0.8},
		{Scenario: ScenarioSOPReply, Provider: prim, Fallbacks: fallback, CostWeight: 2, MaxLatency: maxLatencyMs, MinQuality: 0.9},
		{Scenario: ScenarioObjection, Provider: prim, Fallbacks: fallback, CostWeight: 1, MaxLatency: maxLatencyMs, MinQuality: 0.92},
		{Scenario: ScenarioFriendlyChat, Provider: prim, Fallbacks: fallback, CostWeight: 10, MaxLatency: maxLatencyMs, MinQuality: 0.8},
		{Scenario: ScenarioLongSummary, Provider: prim, Fallbacks: fallback, CostWeight: 3, MaxLatency: maxLatencyMs, MinQuality: 0.85},
		{Scenario: ScenarioHighQuality, Provider: prim, Fallbacks: fallback, CostWeight: 1, MaxLatency: maxLatencyMs, MinQuality: 0.95},
		{Scenario: ScenarioLowCost, Provider: prim, Fallbacks: fallback, CostWeight: 15, MaxLatency: maxLatencyMs, MinQuality: 0.7},
		{Scenario: ScenarioJevChoice, Provider: prim, Fallbacks: fallback, CostWeight: 5, MaxLatency: 5000, MinQuality: 0.7},
	}
	for _, r := range routes {
		d.routes[r.Scenario] = r
	}
}

func (d *Dispatcher) registerDefaultProviders() {

	defaults := []ProviderConfig{
		{
			Name:         "deepseek",
			BaseURL:      "https://api.deepseek.com",
			APIType:      "openai",
			Model:        "deepseek-chat",
			CostPer1k:    0.001,
			AvgLatencyMs: 1500,
			QualityScore: 0.85,
			MaxRPM:       60,
			Enabled:      true,
		},
		{
			Name:         "qwen-turbo",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode",
			APIType:      "openai",
			Model:        "qwen-turbo",
			CostPer1k:    0.003,
			AvgLatencyMs: 1200,
			QualityScore: 0.82,
			MaxRPM:       60,
			Enabled:      true,
		},
		{
			Name:         "qwen-max",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode",
			APIType:      "openai",
			Model:        "qwen-max",
			CostPer1k:    0.020,
			AvgLatencyMs: 2500,
			QualityScore: 0.92,
			MaxRPM:       60,
			Enabled:      true,
		},
		{
			Name:         "gpt-4o",
			BaseURL:      "https://api.openai.com",
			APIType:      "openai",
			Model:        "gpt-4o",
			CostPer1k:    0.030,
			AvgLatencyMs: 3000,
			QualityScore: 0.95,
			MaxRPM:       60,
			Enabled:      true,
		},
		{
			Name:         "glm-4",
			BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
			APIType:      "openai",
			Model:        "glm-4-plus",
			CostPer1k:    0.050,
			AvgLatencyMs: 2800,
			QualityScore: 0.91,
			MaxRPM:       60,
			Enabled:      true,
		},
		{
			Name:         "kimi",
			BaseURL:      "https://api.moonshot.cn",
			APIType:      "openai",
			Model:        "moonshot-v1-8k",
			CostPer1k:    0.012,
			AvgLatencyMs: 2200,
			QualityScore: 0.88,
			MaxRPM:       60,
			Enabled:      true,
		},
	}
	for i := range defaults {
		p := defaults[i]
		d.providers[p.Name] = &p
	}
}

func (d *Dispatcher) registerDefaultRoutes() {
	routes := []*ScenarioRoute{
		{Scenario: ScenarioIntentRecognize, Provider: "deepseek", Fallbacks: []string{"qwen-turbo"}, CostWeight: 5, MaxLatency: 3000, MinQuality: 0.8},
		{Scenario: ScenarioSOPReply, Provider: "qwen-max", Fallbacks: []string{"gpt-4o", "glm-4"}, CostWeight: 2, MaxLatency: 5000, MinQuality: 0.9},
		{Scenario: ScenarioObjection, Provider: "gpt-4o", Fallbacks: []string{"glm-4", "qwen-max"}, CostWeight: 1, MaxLatency: 5000, MinQuality: 0.92},
		{Scenario: ScenarioFriendlyChat, Provider: "qwen-turbo", Fallbacks: []string{"deepseek"}, CostWeight: 4, MaxLatency: 3000, MinQuality: 0.8},
		{Scenario: ScenarioLongSummary, Provider: "kimi", Fallbacks: []string{"qwen-max"}, CostWeight: 3, MaxLatency: 6000, MinQuality: 0.85},
		{Scenario: ScenarioHighQuality, Provider: "gpt-4o", Fallbacks: []string{"glm-4"}, CostWeight: 1, MaxLatency: 5000, MinQuality: 0.95},
		{Scenario: ScenarioLowCost, Provider: "deepseek", Fallbacks: []string{"qwen-turbo"}, CostWeight: 5, MaxLatency: 4000, MinQuality: 0.7},
		{Scenario: ScenarioJevChoice, Provider: "deepseek", Fallbacks: []string{"qwen-turbo"}, CostWeight: 5, MaxLatency: 5000, MinQuality: 0.7},
	}
	for _, r := range routes {
		d.routes[r.Scenario] = r
	}
}

func (d *Dispatcher) SetRouteWithAudit(ctx context.Context, r ScenarioRoute, action, operator, traceID string) ScenarioRoute {
	prev := d.GetRoute(r.Scenario)
	applied := d.SetRoute(r)

	if err := d.UpsertRouteToDB(applied); err != nil {
		logger.Errorf("[LLM] SetRouteWithAudit 路由落库失败 scenario=%s: %v", r.Scenario, err)
	}
	d.writeAuditLog(ctx, r.Scenario, prev, &applied, action, operator, traceID)
	return applied
}

func (d *Dispatcher) LogModelLifecycle(ctx context.Context, action, provider, operator, traceID string) {
	db := getAuditDB()
	if db == nil {
		return
	}
	row := map[string]any{
		"scenario":       "model_lifecycle",
		"version":        0,
		"prev_provider":  "",
		"new_provider":   provider,
		"prev_fallbacks": "",
		"new_fallbacks":  "",
		"action":         action,
		"operator":       operator,
		"trace_id":       traceID,
	}
	if err := db.WithContext(ctx).Table("llm_routing_audit").Create(row).Error; err != nil {
		logger.Warnf("[LLM] write model lifecycle audit failed: %v", err)
	}
}

func (d *Dispatcher) writeAuditLog(ctx context.Context, scenario DispatchScenario, prev, next *ScenarioRoute, action, operator, traceID string) {
	db := getAuditDB()
	if db == nil {
		return
	}
	var prevProvider, newProvider, prevFallbacks, newFallbacks string
	if prev != nil {
		prevProvider = prev.Provider
		prevFallbacks = strings.Join(prev.Fallbacks, ",")
	}
	if next != nil {
		newProvider = next.Provider
		newFallbacks = strings.Join(next.Fallbacks, ",")
	}
	version := 0
	if next != nil {
		version = next.Version
	}
	row := map[string]any{
		"scenario":       string(scenario),
		"version":        version,
		"prev_provider":  prevProvider,
		"new_provider":   newProvider,
		"prev_fallbacks": prevFallbacks,
		"new_fallbacks":  newFallbacks,
		"action":         action,
		"operator":       operator,
		"trace_id":       traceID,
	}
	if err := db.WithContext(ctx).Table("llm_routing_audit").Create(row).Error; err != nil {
		logger.Warnf("[LLM] write routing audit failed: %v", err)
	}
}
