package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"hivemtk-user/internal/cache"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// ProviderStatus Provider 健康状态
type ProviderStatus string

const (
	ProviderStatusUp       ProviderStatus = "up"
	ProviderStatusDown     ProviderStatus = "down"
	ProviderStatusDegraded ProviderStatus = "degraded"
)

// 熔断器默认参数
//
// 这四个值同时是「参数中心」的兜底：运维在 config_params 的 agent_llm 组改了
// default_health_check_interval / default_failure_threshold /
// default_circuit_open_duration / default_health_check_timeout 之后，
// 由 internal/app 的装配层经下面四个 SetXxxProvider 注入，实际生效点走
// HealthCheckInterval() / FailureThreshold() / CircuitOpenDuration() /
// HealthCheckTimeout()，而不是直接读常量。
//
// 分层口径：降级策略整体住在参数中心的 agent_llm 组——单条旋钮（default_*）给全局默认，
// 整份 JSON（provider_failover_policy）是**按部署的覆盖**，JSON 里 >0/非空的字段优先，
// 缺省才回落到下面的常量。
const (
	DefaultHealthCheckInterval = 30 * time.Second
	DefaultFailureThreshold    = 5
	DefaultCircuitOpenDuration = 60 * time.Second
	DefaultHealthCheckTimeout  = 5 * time.Second
)

var (
	healthCheckIntervalProvider func() time.Duration
	failureThresholdProvider    func() int
	circuitOpenDurationProvider func() time.Duration
	healthCheckTimeoutProvider  func() time.Duration
	failoverPolicyProvider      func(context.Context) string
)

// SetHealthCheckIntervalProvider 注入健康检查周期；传 nil 视为不注入，回落到 DefaultHealthCheckInterval。
func SetHealthCheckIntervalProvider(fn func() time.Duration) {
	healthCheckIntervalProvider = fn
}

// SetFailureThresholdProvider 注入熔断失败阈值；传 nil 视为不注入，回落到 DefaultFailureThreshold。
func SetFailureThresholdProvider(fn func() int) {
	failureThresholdProvider = fn
}

// SetCircuitOpenDurationProvider 注入熔断保持时长；传 nil 视为不注入，回落到 DefaultCircuitOpenDuration。
func SetCircuitOpenDurationProvider(fn func() time.Duration) {
	circuitOpenDurationProvider = fn
}

// SetHealthCheckTimeoutProvider 注入健康检查超时；传 nil 视为不注入，回落到 DefaultHealthCheckTimeout。
func SetHealthCheckTimeoutProvider(fn func() time.Duration) {
	healthCheckTimeoutProvider = fn
}

// SetFailoverPolicyProvider 注入降级策略 JSON 的读取口（参数中心 agent_llm.provider_failover_policy）。
// 传 nil 视为不注入，LoadPolicy 回落到 DefaultFailoverPolicy。
//
// 迁走的是原先对 system_kv_config 的裸 SQL 直查（D12 配置层统一）：那张表既没有缓存也没有
// 管理端写路径，改一次策略要手工写 SQL，且健康检查循环每个 tick 都打一次 DB。
func SetFailoverPolicyProvider(fn func(context.Context) string) {
	failoverPolicyProvider = fn
}

// HealthCheckInterval 生效的健康检查周期。非正值一律回落兜底——0 或负数会让健康检查退化成
// 每个 tick 都跑（或根本不跑），把 provider 全部标成 down。
func HealthCheckInterval() time.Duration {
	if p := healthCheckIntervalProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultHealthCheckInterval
}

// FailureThreshold 生效的连续失败熔断阈值。非正值一律回落兜底——阈值 0 会让任何一次失败
// （含一次网络抖动）立刻熔断整个 provider。
func FailureThreshold() int {
	if p := failureThresholdProvider; p != nil {
		if n := p(); n > 0 {
			return n
		}
	}
	return DefaultFailureThreshold
}

// CircuitOpenDuration 生效的熔断保持时长。非正值一律回落兜底——0 等于不熔断，
// 熔断器形同虚设。
func CircuitOpenDuration() time.Duration {
	if p := circuitOpenDurationProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultCircuitOpenDuration
}

// HealthCheckTimeout 生效的健康检查请求超时。非正值一律回落兜底——0 会让
// context.WithTimeout 立刻超时，等于把所有 provider 判死。
func HealthCheckTimeout() time.Duration {
	if p := healthCheckTimeoutProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultHealthCheckTimeout
}

// ProviderHealth Provider 健康状态记录（运行期数据 + 可选 DB 持久化）
type ProviderHealth struct {
	ProviderName        string         `json:"provider_name"`
	Status              ProviderStatus `json:"status"`
	LastCheck           time.Time      `json:"last_check"`
	LastError           string         `json:"last_error,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	CircuitOpenUntil    time.Time      `json:"circuit_open_until,omitempty"`
	LatencyP95Ms        int64          `json:"latency_p95_ms,omitempty"`
}

// FailoverConfig 降级策略配置（读自参数中心 agent_llm.provider_failover_policy 的 JSON）
type FailoverConfig struct {
	HealthCheckInterval   int    `json:"health_check_interval"`
	FailureThreshold      int    `json:"failure_threshold"`
	CircuitOpenDuration   int    `json:"circuit_open_duration"`
	DegradedLatencyMs     int64  `json:"degraded_latency_ms"`
	LocalFallbackProvider string `json:"local_fallback_provider"`
	TemplateReply         string `json:"template_reply"`
	HealthCheckPath       string `json:"health_check_path"`
}

// DefaultFailoverConfig 默认降级策略
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		HealthCheckInterval:   int(HealthCheckInterval() / time.Second),
		FailureThreshold:      FailureThreshold(),
		CircuitOpenDuration:   int(CircuitOpenDuration() / time.Second),
		DegradedLatencyMs:     3000,
		LocalFallbackProvider: "default",
		TemplateReply:         "抱歉，当前服务暂时繁忙，请稍后再试或联系人工客服。",
		HealthCheckPath:       "/health",
	}
}

// FailoverPolicy 降级策略（参数中心里那份完整 JSON）
type FailoverPolicy struct {
	Config    FailoverConfig      `json:"config"`
	Scenarios map[string][]string `json:"scenarios"`
}

// DefaultFailoverPolicy 默认降级策略（参数中心读不到值时的兜底）
func DefaultFailoverPolicy() FailoverPolicy {
	return FailoverPolicy{
		Config: DefaultFailoverConfig(),
		Scenarios: map[string][]string{
			"intent_recognize": {"default"},
			"sop_reply":        {"default"},
			"objection":        {"default"},
			"friendly_chat":    {"default"},
			"long_summary":     {"default"},
			"high_quality":     {"default"},
			"low_cost":         {"default"},
		},
	}
}

// HealthChecker Provider 健康检查器
type HealthChecker interface {
	Ping(ctx context.Context, provider *ProviderConfig, config FailoverConfig) (int64, error)
}

// HTTPHealthChecker 基于 HTTP 的健康检查器
type HTTPHealthChecker struct {
	httpClient *http.Client
}

// NewHTTPHealthChecker 创建 HTTP 健康检查器
func NewHTTPHealthChecker() *HTTPHealthChecker {
	return &HTTPHealthChecker{
		httpClient: &http.Client{Timeout: HealthCheckTimeout()},
	}
}

// Ping 实现 HealthChecker 接口
// 优先 GET {BaseURL}{HealthCheckPath}，失败时退化为轻量 chat completion 请求
func (h *HTTPHealthChecker) Ping(ctx context.Context, provider *ProviderConfig, config FailoverConfig) (int64, error) {
	if provider == nil {
		return 0, fmt.Errorf("provider is nil")
	}
	if provider.BaseURL == "" {
		return 0, nil
	}
	path := config.HealthCheckPath
	if path == "" {
		path = "/health"
	}
	url := strings.TrimRight(provider.BaseURL, "/") + path

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := h.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return latency, fmt.Errorf("health check %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return latency, fmt.Errorf("health check %s returned status %d", url, resp.StatusCode)
	}
	return latency, nil
}

// ProviderFailover 多 Provider 降级管理器
type ProviderFailover struct {
	mu         sync.RWMutex
	dispatcher *Dispatcher
	checker    HealthChecker
	health     map[string]*ProviderHealth
	config     FailoverConfig
	stopCh     chan struct{}
	stopped    atomic.Bool
}

// NewProviderFailover 创建降级管理器
func NewProviderFailover(dispatcher *Dispatcher) *ProviderFailover {
	return &ProviderFailover{
		dispatcher: dispatcher,
		checker:    NewHTTPHealthChecker(),
		health:     make(map[string]*ProviderHealth),
		config:     DefaultFailoverConfig(),
		stopCh:     make(chan struct{}),
	}
}

// SetHealthChecker 注入自定义 HealthChecker（测试用）
func (f *ProviderFailover) SetHealthChecker(checker HealthChecker) {
	if checker != nil {
		f.checker = checker
	}
}

// LoadPolicy 从参数中心加载策略（agent_llm.provider_failover_policy）；
// 未注入读取口、值为空或 JSON 非法时使用默认策略。
//
// 两条合并规则不对称，都在这里生效：
//   - config 逐字段合并，只有 >0 / 非空的字段才覆盖默认，所以可以只写想改的那一项；
//   - scenarios 整表替换，只要非空就以 JSON 为准。少写一个场景不会报错，那个场景会
//     改由调度路由与本地兜底提供候选（buildCandidates），降级层级随之变少。
func (f *ProviderFailover) LoadPolicy(ctx context.Context) FailoverPolicy {
	policy := DefaultFailoverPolicy()
	raw := ""
	if p := failoverPolicyProvider; p != nil {
		raw = p(ctx)
	}
	if raw == "" {
		return policy
	}
	var loaded FailoverPolicy
	if err := json.Unmarshal([]byte(raw), &loaded); err != nil {
		logger.Warnf("[ProviderFailover] 解析参数中心 agent_llm.provider_failover_policy 失败: %v", err)
		return policy
	}
	if loaded.Config.HealthCheckInterval > 0 {
		policy.Config.HealthCheckInterval = loaded.Config.HealthCheckInterval
	}
	if loaded.Config.FailureThreshold > 0 {
		policy.Config.FailureThreshold = loaded.Config.FailureThreshold
	}
	if loaded.Config.CircuitOpenDuration > 0 {
		policy.Config.CircuitOpenDuration = loaded.Config.CircuitOpenDuration
	}
	if loaded.Config.DegradedLatencyMs > 0 {
		policy.Config.DegradedLatencyMs = loaded.Config.DegradedLatencyMs
	}
	if loaded.Config.LocalFallbackProvider != "" {
		policy.Config.LocalFallbackProvider = loaded.Config.LocalFallbackProvider
	}
	if loaded.Config.TemplateReply != "" {
		policy.Config.TemplateReply = loaded.Config.TemplateReply
	}
	if loaded.Config.HealthCheckPath != "" {
		policy.Config.HealthCheckPath = loaded.Config.HealthCheckPath
	}
	if len(loaded.Scenarios) > 0 {
		policy.Scenarios = loaded.Scenarios
	}
	return policy
}

// ApplyConfig 应用降级策略配置
func (f *ProviderFailover) ApplyConfig(config FailoverConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config = config
}

// Config 返回当前配置（只读副本）
func (f *ProviderFailover) Config() FailoverConfig {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.config
}

// Start 启动健康检查循环（后台 goroutine）
func (f *ProviderFailover) Start(ctx context.Context) {
	if f.stopped.Load() {
		return
	}
	go f.healthCheckLoop(ctx)
}

// Stop 停止健康检查
func (f *ProviderFailover) Stop() {
	if f.stopped.CompareAndSwap(false, true) {
		close(f.stopCh)
	}
}

func (f *ProviderFailover) healthCheckLoop(ctx context.Context) {
	policy := f.LoadPolicy(ctx)
	f.ApplyPolicy(policy)

	ticker := time.NewTicker(f.interval())
	defer ticker.Stop()
	f.checkAll(ctx)
	for {
		select {
		case <-f.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.checkAll(ctx)
			policy := f.LoadPolicy(ctx)
			f.ApplyPolicy(policy)
			ticker.Reset(f.interval())
		}
	}
}

// ApplyPolicy 应用策略配置
func (f *ProviderFailover) ApplyPolicy(policy FailoverPolicy) {
	f.ApplyConfig(policy.Config)
}

func (f *ProviderFailover) interval() time.Duration {
	cfg := f.Config()
	sec := cfg.HealthCheckInterval
	if sec <= 0 {
		sec = int(HealthCheckInterval() / time.Second)
	}
	return time.Duration(sec) * time.Second
}

func (f *ProviderFailover) checkAll(ctx context.Context) {
	if f.dispatcher == nil {
		return
	}
	providers := f.dispatcher.GetProviderList()
	cfg := f.Config()
	for i := range providers {
		p := &providers[i]
		if !p.Enabled {
			continue
		}
		f.checkOne(ctx, p, cfg)
	}
}

func (f *ProviderFailover) checkOne(ctx context.Context, provider *ProviderConfig, cfg FailoverConfig) {
	checkCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout())
	defer cancel()
	latency, err := f.checker.Ping(checkCtx, provider, cfg)
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.health[provider.Name]
	if !ok {
		h = &ProviderHealth{ProviderName: provider.Name, Status: ProviderStatusUp}
		f.health[provider.Name] = h
	}
	h.LastCheck = time.Now()
	h.LatencyP95Ms = latency
	if err != nil {
		h.ConsecutiveFailures++
		h.LastError = err.Error()
		if h.ConsecutiveFailures >= cfg.FailureThreshold {
			h.Status = ProviderStatusDown
			h.CircuitOpenUntil = time.Now().Add(time.Duration(cfg.CircuitOpenDuration) * time.Second)
		} else {
			h.Status = ProviderStatusDegraded
		}
		return
	}

	if time.Now().Before(h.CircuitOpenUntil) {
		return
	}
	h.ConsecutiveFailures = 0
	h.LastError = ""
	h.CircuitOpenUntil = time.Time{}
	if cfg.DegradedLatencyMs > 0 && latency > cfg.DegradedLatencyMs {
		h.Status = ProviderStatusDegraded
	} else {
		h.Status = ProviderStatusUp
	}
}

// IsCircuitOpen 判断 provider 是否处于熔断状态
func (f *ProviderFailover) IsCircuitOpen(providerName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	h, ok := f.health[providerName]
	if !ok {
		return false
	}
	if h.CircuitOpenUntil.IsZero() {
		return false
	}
	if time.Now().Before(h.CircuitOpenUntil) {
		return true
	}
	if cache.GlobalIsRedis() {
		if open, e := cache.GetGlobalCache().Exists(context.Background(), "mtk:circuit:open:"+providerName); e == nil && open {
			return true
		}
	}
	return false
}

// GetHealth 返回 provider 健康状态
func (f *ProviderFailover) GetHealth(providerName string) *ProviderHealth {
	f.mu.RLock()
	defer f.mu.RUnlock()
	h, ok := f.health[providerName]
	if !ok {
		return nil
	}
	cp := *h
	return &cp
}

// GetAllHealth 返回所有 provider 健康状态
func (f *ProviderFailover) GetAllHealth() []ProviderHealth {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]ProviderHealth, 0, len(f.health))
	for _, h := range f.health {
		out = append(out, *h)
	}
	return out
}

// ResetCircuit 重置 provider 熔断器
func (f *ProviderFailover) ResetCircuit(providerName string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.health[providerName]
	if !ok {
		return false
	}
	h.CircuitOpenUntil = time.Time{}
	if cache.GlobalIsRedis() {
		_ = cache.GetGlobalCache().Delete(context.Background(), "mtk:circuit:open:"+providerName)
	}
	h.ConsecutiveFailures = 0
	h.Status = ProviderStatusUp
	h.LastError = ""
	return true
}

// RecordSuccess 记录 provider 调用成功（由 Dispatch 调用）
func (f *ProviderFailover) RecordSuccess(providerName string, latencyMs int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.health[providerName]
	if !ok {
		h = &ProviderHealth{ProviderName: providerName, Status: ProviderStatusUp}
		f.health[providerName] = h
	}
	h.ConsecutiveFailures = 0
	h.LastError = ""
	h.CircuitOpenUntil = time.Time{}
	if cache.GlobalIsRedis() {
		_ = cache.GetGlobalCache().Delete(context.Background(), "mtk:circuit:open:"+providerName)
	}
	h.LatencyP95Ms = latencyMs
	cfg := f.config
	if cfg.DegradedLatencyMs > 0 && latencyMs > cfg.DegradedLatencyMs {
		h.Status = ProviderStatusDegraded
	} else {
		h.Status = ProviderStatusUp
	}
}

const rateLimitCooldownFallback = 15 * time.Second

func (f *ProviderFailover) recordRateLimitCooldown(h *ProviderHealth, rle *RateLimitError) {
	dur := rle.RetryAfter
	if dur <= 0 {
		dur = rateLimitCooldownFallback
	}
	h.Status = ProviderStatusDegraded
	h.LastError = rle.Error()
	h.CircuitOpenUntil = time.Now().Add(dur)
	if cache.GlobalIsRedis() {
		if _, err := cache.GetGlobalCache().SetNX(context.Background(), "mtk:circuit:open:"+h.ProviderName, "1", dur); err != nil {
			logger.Warnf("[Failover] 写入熔断标记失败 provider=%s: %v", h.ProviderName, err)
		}
	}
}

// RecordFailure 记录 provider 调用失败（由 Dispatch 调用）
func (f *ProviderFailover) RecordFailure(providerName string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.health[providerName]
	if !ok {
		h = &ProviderHealth{ProviderName: providerName, Status: ProviderStatusUp}
		f.health[providerName] = h
	}

	var rle *RateLimitError
	if errors.As(err, &rle) {
		f.recordRateLimitCooldown(h, rle)
		return
	}
	h.ConsecutiveFailures++
	if err != nil {
		h.LastError = err.Error()
	}
	cfg := f.config
	if h.ConsecutiveFailures >= cfg.FailureThreshold {
		h.Status = ProviderStatusDown
		dur := time.Duration(cfg.CircuitOpenDuration) * time.Second
		h.CircuitOpenUntil = time.Now().Add(dur)
		if cache.GlobalIsRedis() {
			if _, err := cache.GetGlobalCache().SetNX(context.Background(), "mtk:circuit:open:"+providerName, "1", dur); err != nil {
				logger.Warnf("[Failover] 写入熔断标记失败 provider=%s: %v", providerName, err)
			}
		}
	} else {
		h.Status = ProviderStatusDegraded
	}
}

// DispatchWithFailover 带降级的调度
// 顺序：主 Provider → 备用 Provider → 本地兜底 → 模板回复
func (f *ProviderFailover) DispatchWithFailover(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	if f.dispatcher == nil {
		return nil, fmt.Errorf("dispatcher is nil")
	}

	policy := f.LoadPolicy(ctx)
	f.ApplyPolicy(policy)

	candidates := f.buildCandidates(req.Scenario, policy)
	cfg := f.Config()

	var lastErr error
	for _, name := range candidates {
		if f.IsCircuitOpen(name) {
			continue
		}
		provider := f.dispatcher.GetProvider(name)
		if provider == nil || !provider.Enabled {
			continue
		}
		start := time.Now()
		result, err := f.callSingleProvider(ctx, provider, req, cfg)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			f.RecordFailure(name, err)
			lastErr = err
			logger.Warnf("[ProviderFailover] provider=%s failed: %v", name, err)
			continue
		}
		f.RecordSuccess(name, latency)
		return result, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no available provider for scenario: %s", req.Scenario)
	}
	logger.Errorf("[ProviderFailover] all providers failed scenario=%s: %v", req.Scenario, lastErr)
	return f.degradedResponse(req, cfg, lastErr), nil
}

func (f *ProviderFailover) buildCandidates(scenario DispatchScenario, policy FailoverPolicy) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	if list, ok := policy.Scenarios[string(scenario)]; ok {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	if route := f.dispatcher.GetRoute(scenario); route != nil {
		if !seen[route.Provider] {
			seen[route.Provider] = true
			out = append(out, route.Provider)
		}
		for _, name := range route.Fallbacks {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	if policy.Config.LocalFallbackProvider != "" && !seen[policy.Config.LocalFallbackProvider] {
		out = append(out, policy.Config.LocalFallbackProvider)
	}
	return out
}

func (f *ProviderFailover) callSingleProvider(ctx context.Context, provider *ProviderConfig, req DispatchRequest, cfg FailoverConfig) (*DispatchResult, error) {
	route := &ScenarioRoute{
		Scenario:   req.Scenario,
		Provider:   provider.Name,
		MaxLatency: 0,
		MinQuality: 0,
	}
	return f.dispatcher.callProvider(ctx, provider, req, route)
}

func (f *ProviderFailover) degradedResponse(req DispatchRequest, cfg FailoverConfig, cause error) *DispatchResult {

	reply := ResolveDegradedTemplate(req.Scenario, cfg.TemplateReply)
	return &DispatchResult{
		Provider:     "degraded",
		Model:        "template",
		Content:      reply,
		FinishReason: "degraded",
		Usage: TokenUsage{
			PromptTokens:     estimateTokens(req.Prompt),
			CompletionTokens: estimateTokens(reply),
			TotalTokens:      estimateTokens(req.Prompt) + estimateTokens(reply),
		},
	}
}

// IsDegraded 判断 DispatchResult 是否为降级响应
func IsDegraded(result *DispatchResult) bool {
	return result != nil && result.Provider == "degraded" && result.Model == "template"
}

var (
	globalFailover     *ProviderFailover
	globalFailoverOnce sync.Once
)

// InitGlobalFailover 初始化全局降级管理器
func InitGlobalFailover(dispatcher *Dispatcher) *ProviderFailover {
	globalFailoverOnce.Do(func() {
		globalFailover = NewProviderFailover(dispatcher)
	})
	return globalFailover
}

// GetGlobalFailover 获取全局降级管理器
func GetGlobalFailover() *ProviderFailover {
	return globalFailover
}
