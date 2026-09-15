package llm

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	textutil "hivemtk-user/internal/pkg/utils/text"
	"sync"
	"sync/atomic"
	"time"
)

type DispatchScenario string

const (
	ScenarioIntentRecognize DispatchScenario = "intent_recognize"

	ScenarioSOPReply DispatchScenario = "sop_reply"

	ScenarioObjection DispatchScenario = "objection"

	ScenarioFriendlyChat DispatchScenario = "friendly_chat"

	ScenarioLongSummary DispatchScenario = "long_summary"

	ScenarioHighQuality DispatchScenario = "high_quality"

	ScenarioLowCost DispatchScenario = "low_cost"
)

type ProviderConfig struct {
	Name          string   `json:"name"`
	APIKey        string   `json:"api_key"`
	BaseURL       string   `json:"base_url"`
	APIType       string   `json:"api_type"`
	Model         string   `json:"model"`
	CostPer1k     float64  `json:"cost_per_1k"`
	AvgLatencyMs  int      `json:"avg_latency_ms"`
	QualityScore  float64  `json:"quality_score"`
	MaxRPM        int      `json:"max_rpm"`
	MaxTPM        int      `json:"max_tpm"`
	Enabled       bool     `json:"enabled"`
	NoFC          bool     `json:"no_fc,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
	Vendor        string   `json:"vendor,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
}

type ScenarioRoute struct {
	Scenario    DispatchScenario `json:"scenario"`
	Provider    string           `json:"provider"`
	Fallbacks   []string         `json:"fallbacks"`
	CostWeight  int              `json:"cost_weight"`
	MaxLatency  int              `json:"max_latency"`
	MinQuality  float64          `json:"min_quality"`
	Version     int              `json:"version"`
	Weight      int              `json:"weight"`
	CanaryKey   string           `json:"canary_key,omitempty"`
	CanaryRoute *ScenarioRoute   `json:"canary_route,omitempty"`
}

type Dispatcher struct {
	mu             sync.RWMutex
	providers      map[string]*ProviderConfig
	routes         map[DispatchScenario]*ScenarioRoute
	llmService     *LLMService
	rpmCounter     map[string]*rpmBucket
	reactAdapter   *ReActAdapter
	reactAdapterMu sync.Once
	testMode       atomic.Bool
}

func NewDispatcher(llmService *LLMService) *Dispatcher {
	d := newDispatcherBase(llmService)
	d.registerDefaultProviders()
	d.registerDefaultRoutes()
	return d
}

func NewDispatcherFromConfig(cfg config.AppConfig) *Dispatcher {

	timeoutSec := cfg.Inference.LLM.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 180
	}

	setDefaultHTTPTimeout(time.Duration(timeoutSec) * time.Second)
	d := newDispatcherBase(NewLLMService())
	d.registerLocalProvider(cfg.Inference.LLM)
	d.registerCloudProvidersFromConfig(cfg.Inference.LLM)
	d.registerLocalFirstRoutes(cfg.Inference.LLM.PrimaryProvider, timeoutSec*1000)
	return d
}

func (d *Dispatcher) AddProvider(p ProviderConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.providers[p.Name] = &p
}

func (d *Dispatcher) SetRoute(r ScenarioRoute) ScenarioRoute {
	d.mu.Lock()
	defer d.mu.Unlock()
	prev, hasPrev := d.routes[r.Scenario]
	if r.Version == 0 {
		if hasPrev {
			r.Version = prev.Version + 1
		} else {
			r.Version = 1
		}
	}
	cp := r
	d.routes[r.Scenario] = &cp
	return cp
}

func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	// 空接收者保护：*Dispatcher 为 nil 时，下面 d.mu.RLock() 会空指针 panic。
	//
	// 为什么必须在这里兜底：本类型的指针常被塞进接口（例如 trace_learning.InsightLLM）。
	// Go 里「值为 nil 的具名指针」装进接口后，接口本身**非 nil**，
	// 于是调用方惯用的 `if iface == nil` 判断会失效（typed-nil 陷阱），
	// 最终把空指针一路带到方法体内炸掉。在方法入口兜底可一次性覆盖全部调用点。
	if d == nil {
		return nil, errors.New("llm dispatcher is nil")
	}
	d.mu.RLock()
	route, ok := d.routes[req.Scenario]
	if !ok {
		d.mu.RUnlock()
		return nil, fmt.Errorf("no route for scenario: %s", req.Scenario)
	}
	d.mu.RUnlock()

	activeRoute := route
	if canary := DecideCanaryRoute(route, req.CanaryKey); canary != nil {
		activeRoute = canary
	}

	if req.CacheKey != "" && req.CacheTTL > 0 {
		if c, hit := d.getCache(ctx, req.CacheKey); hit {

			if !d.testMode.Load() {

				ctid := ""
				if c := tracing.CarrierFromContext(ctx); c != nil {
					ctid = c.TraceID
				}
				if ctid == "" {
					ctid = tracing.TraceIDFromContext(ctx)
				}
				cacheEntry := &LogEntry{
					TraceID:          ctid,
					Scenario:         req.Scenario,
					Provider:         "cache",
					Model:            "cache",
					Success:          true,
					FromCache:        true,
					Source:           SourceCache,
					TokenSource:      TokenSourceActual,
					ScenarioProvider: string(req.Scenario) + "|cache",
					InternalLang:     req.InternalLang,
					TargetLang:       req.TargetLang,
					CrossLingual:     req.CrossLingual,
					GlossaryVersion:  req.GlossaryVersion,
					CacheHit:         req.CacheKey != "",
				}
				LogRoutingDecision(ctx, cacheEntry)
			}
			return &DispatchResult{Provider: "cache", Model: "cache", Content: c, FromCache: true}, nil
		}
	}

	candidates := []string{activeRoute.Provider}
	candidates = append(candidates, activeRoute.Fallbacks...)

	if (req.FanOut == nil || !req.FanOut.Enable) && req.Scenario == ScenarioHighQuality {
		if fanoutVoteEnabled() {
			if len(candidates) >= 2 {
				req.FanOut = &FanOutConfig{Enable: true, Strategy: "vote", Timeout: 8 * time.Second}
			}
		}
	}
	if req.FanOut != nil && req.FanOut.Enable && len(candidates) >= 2 {
		return d.dispatchFanOut(ctx, req, activeRoute, candidates)
	}

	traceID := ""
	if c := tracing.CarrierFromContext(ctx); c != nil {
		traceID = c.TraceID
	}
	if traceID == "" {
		traceID = tracing.TraceIDFromContext(ctx)
	}

	var lastErr error
	attempted := 0
	for _, providerName := range candidates {
		d.mu.RLock()
		provider, exists := d.providers[providerName]
		d.mu.RUnlock()
		if !exists || !provider.Enabled {
			continue
		}

		if activeRoute.MinQuality > 0 && provider.QualityScore < activeRoute.MinQuality {
			continue
		}
		if activeRoute.MaxLatency > 0 && provider.AvgLatencyMs > activeRoute.MaxLatency {
			continue
		}

		if fo := GetGlobalFailover(); fo != nil && fo.IsCircuitOpen(providerName) {
			logger.Debugf("[LLM] provider=%s 集群熔断中，跳过 scenario=%s", providerName, req.Scenario)
			continue
		}

		if !d.allowRequest(providerName, provider.MaxRPM) {
			continue
		}

		if provider.ContextWindow > 0 {
			estimatedTokens := estimateRequestTokens(req)
			if estimatedTokens > provider.ContextWindow {
				logger.Warnf("[LLM] context_window_exceeded provider=%s model=%s tokens=%d/%d, auto skip to next fallback",
					provider.Name, provider.Model, estimatedTokens, provider.ContextWindow)
				continue
			}
		}

		attempted++
		result, err := d.callProvider(ctx, provider, req, activeRoute)
		if err != nil {
			lastErr = err

			if d.testMode.Load() {
				continue
			}

			if fo := GetGlobalFailover(); fo != nil {
				fo.RecordFailure(providerName, err)
			}

			logger.Warnf("[LLM Fallback] scenario=%s provider=%s trace_id=%s failed (attempt %d/%d): %v",
				req.Scenario, providerName, traceID, attempted, len(candidates), err)

			AlertProviderFailure(string(req.Scenario), providerName, err, traceID)

			isFallback := attempted > 1
			failEntry := NewLogEntry(req.Scenario, provider, provider.Model,
				0, 0, 0, 0, 0, false, err.Error(), false, isFallback, traceID, "", SourceFallback)
			failEntry.InternalLang = req.InternalLang
			failEntry.TargetLang = req.TargetLang
			failEntry.CrossLingual = req.CrossLingual
			failEntry.GlossaryVersion = req.GlossaryVersion
			failEntry.CacheHit = req.CacheKey != ""
			LogRoutingDecision(ctx, failEntry)
			continue
		}

		if attempted > 1 {
			logger.Infof("[LLM Fallback] scenario=%s succeeded on provider=%s (after %d attempts) trace_id=%s",
				req.Scenario, providerName, attempted, traceID)
		}

		if d.testMode.Load() {
			return result, nil
		}

		AlertProviderSuccess(string(req.Scenario), providerName, traceID)

		if fo := GetGlobalFailover(); fo != nil {
			fo.RecordSuccess(providerName, int64(result.LatencyMs))
		}
		if req.CacheKey != "" && req.CacheTTL > 0 {
			d.setCache(ctx, req.CacheKey, req.CacheTTL, result.Content)
		}

		isFallback := attempted > 1
		successEntry := NewLogEntry(req.Scenario, provider, result.Model,
			result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens,
			result.Cost, result.LatencyMs, true, "", false, isFallback, traceID, result.Content, SourceDispatch)
		successEntry.InternalLang = req.InternalLang
		successEntry.TargetLang = req.TargetLang
		successEntry.CrossLingual = req.CrossLingual
		successEntry.GlossaryVersion = req.GlossaryVersion
		successEntry.CacheHit = req.CacheKey != ""
		LogRoutingDecision(ctx, successEntry)
		return result, nil
	}

	if attempted == 0 {
		if fb := d.pickEnabledFallback(activeRoute); fb != "" {
			d.mu.RLock()
			provider, exists := d.providers[fb]
			d.mu.RUnlock()
			if exists {
				logger.Infof("[LLM] scenario=%s 路由候选全部不可用，兜底启用 provider=%s trace_id=%s", req.Scenario, fb, traceID)
				result, err := d.callProvider(ctx, provider, req, activeRoute)
				lastErr = err
				if err == nil {

					if fo := GetGlobalFailover(); fo != nil {
						fo.RecordSuccess(fb, int64(result.LatencyMs))
					}
					AlertProviderSuccess(string(req.Scenario), fb, traceID)
					if req.CacheKey != "" && req.CacheTTL > 0 {
						d.setCache(ctx, req.CacheKey, req.CacheTTL, result.Content)
					}
					fallbackEntry := NewLogEntry(req.Scenario, provider, result.Model,
						result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens,
						result.Cost, result.LatencyMs, true, "", false, true, traceID, result.Content, SourceDispatch)
					fallbackEntry.InternalLang = req.InternalLang
					fallbackEntry.TargetLang = req.TargetLang
					fallbackEntry.CrossLingual = req.CrossLingual
					fallbackEntry.GlossaryVersion = req.GlossaryVersion
					fallbackEntry.CacheHit = req.CacheKey != ""
					LogRoutingDecision(ctx, fallbackEntry)
					return result, nil
				}
				logger.Warnf("[LLM] scenario=%s 兜底 provider=%s 调用失败: %v", req.Scenario, fb, lastErr)
				AlertProviderFailure(string(req.Scenario), fb, lastErr, traceID)
			}
		}
	}

	if lastErr == nil {

		logger.Warnf("[LLM] scenario=%s 无可用 provider（全部被熔断/限流跳过），返回降级回复 trace_id=%s", req.Scenario, traceID)
		AlertAllProvidersFailed(string(req.Scenario), fmt.Errorf("no available provider"), traceID)
		return degradedReply(req), nil
	}

	logger.Errorf("[LLM Fallback] all providers failed scenario=%s trace_id=%s attempted=%d: %v",
		req.Scenario, traceID, attempted, lastErr)
	AlertAllProvidersFailed(string(req.Scenario), lastErr, traceID)
	return nil, lastErr
}

// OnProviderFailure 实现 AlertHook
func (f AlertHookFunc) OnProviderFailure(scenario, provider string, err error, traceID string) {
	if f.OnFailure != nil {
		f.OnFailure(scenario, provider, err, traceID)
	}
}

// OnProviderSuccess 实现 AlertHook
func (f AlertHookFunc) OnProviderSuccess(scenario, provider, traceID string) {
	if f.OnSuccess != nil {
		f.OnSuccess(scenario, provider, traceID)
	}
}

// OnAllProvidersFailed 实现 AlertHook
func (f AlertHookFunc) OnAllProvidersFailed(scenario string, err error, traceID string) {
	if f.OnAllFailed != nil {
		f.OnAllFailed(scenario, err, traceID)
	}
}

var (
	alertHookMu sync.RWMutex
	alertHook   AlertHook = NoopAlertHook{}
)

// OnProviderFailure 默认空实现
func (NoopAlertHook) OnProviderFailure(string, string, error, string) {}

// OnProviderSuccess 默认空实现
func (NoopAlertHook) OnProviderSuccess(string, string, string) {}

// OnAllProvidersFailed 默认空实现
func (NoopAlertHook) OnAllProvidersFailed(string, error, string) {}

// OnProviderFailure 写 WARN 日志
func (h LoggingAlertHook) OnProviderFailure(scenario, provider string, err error, traceID string) {
	logger.Warnf("[LLM Alert] provider failure scenario=%s provider=%s trace_id=%s err=%v",
		scenario, provider, traceID, err)
	if h.OnFailure != nil {
		h.OnFailure(scenario, provider, traceID, err)
	}
}

// OnProviderSuccess 写 INFO 日志
func (h LoggingAlertHook) OnProviderSuccess(scenario, provider, traceID string) {
	logger.Infof("[LLM Alert] provider recovered scenario=%s provider=%s trace_id=%s",
		scenario, provider, traceID)
	if h.OnSuccess != nil {
		h.OnSuccess(scenario, provider, traceID)
	}
}

// OnAllProvidersFailed 写 ERROR 日志
func (h LoggingAlertHook) OnAllProvidersFailed(scenario string, err error, traceID string) {
	logger.Errorf("[LLM Alert] all providers FAILED scenario=%s trace_id=%s err=%v",
		scenario, traceID, err)
	if h.OnAllFailed != nil {
		h.OnAllFailed(scenario, traceID, err)
	}
}

func (s *InMemoryAlertSink) record(ev AlertEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buffer) >= s.cap {

		s.buffer = s.buffer[1:]
	}
	s.buffer = append(s.buffer, ev)
}

// OnProviderFailure 累计失败
func (s *InMemoryAlertSink) OnProviderFailure(scenario, provider string, err error, traceID string) {
	s.record(AlertEvent{
		Time: time.Now(), Severity: "warn", Scenario: scenario,
		Provider: provider, TraceID: traceID, Message: err.Error(),
	})
}

// OnProviderSuccess 累计成功（用于计算成功率）
func (s *InMemoryAlertSink) OnProviderSuccess(scenario, provider, traceID string) {
	s.record(AlertEvent{
		Time: time.Now(), Severity: "info", Scenario: scenario,
		Provider: provider, TraceID: traceID, Message: "recovered",
	})
}

// OnAllProvidersFailed 累计全部失败
func (s *InMemoryAlertSink) OnAllProvidersFailed(scenario string, err error, traceID string) {
	s.record(AlertEvent{
		Time: time.Now(), Severity: "error", Scenario: scenario,
		TraceID: traceID, Message: err.Error(),
	})
}

// Drain 消费所有告警（消费后清空）
func (s *InMemoryAlertSink) Drain() []AlertEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AlertEvent, len(s.buffer))
	copy(out, s.buffer)
	s.buffer = s.buffer[:0]
	return out
}

// Snapshot 快照
func (s *InMemoryAlertSink) Snapshot() []AlertEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AlertEvent, len(s.buffer))
	copy(out, s.buffer)
	return out
}
func estimateRequestTokens(req DispatchRequest) int {
	total := textutil.EstimateTokens(req.Prompt) + textutil.EstimateTokens(req.SystemPrompt)
	for _, m := range req.Messages {
		total += textutil.EstimateTokens(m.Content)
	}
	total += len(req.Tools) * 50
	return total
}

func (d *Dispatcher) dispatchFanOut(ctx context.Context, req DispatchRequest, route *ScenarioRoute, candidates []string) (*DispatchResult, error) {
	strategy := "fastest"
	timeout := 5 * time.Second
	if req.FanOut.Strategy != "" {
		strategy = req.FanOut.Strategy
	}
	if req.FanOut.Timeout > 0 {
		timeout = req.FanOut.Timeout
	}

	fanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type fanResult struct {
		r   *DispatchResult
		err error
		idx int
	}

	maxConcurrent := len(candidates)
	if maxConcurrent > 2 {
		maxConcurrent = 2
	}
	ch := make(chan fanResult, maxConcurrent)
	for i := 0; i < maxConcurrent; i++ {
		providerName := candidates[i]
		d.mu.RLock()
		provider, exists := d.providers[providerName]
		d.mu.RUnlock()
		if !exists || !provider.Enabled {
			ch <- fanResult{idx: i, err: fmt.Errorf("provider %s not available", providerName)}
			continue
		}
		go func(idx int, prov *ProviderConfig) {
			r, err := d.callProvider(fanCtx, prov, req, route)
			ch <- fanResult{r: r, err: err, idx: idx}
		}(i, provider)
	}

	if strategy == "fastest" {
		var lastErr error
		for i := 0; i < maxConcurrent; i++ {
			select {
			case res := <-ch:
				if res.err == nil && res.r != nil {
					logger.Infof("[LLM] fan-out fastest wins provider=%s", candidates[res.idx])
					return res.r, nil
				}
				lastErr = res.err
			case <-fanCtx.Done():
				return nil, fmt.Errorf("fan-out timeout: %w", fanCtx.Err())
			}
		}
		return nil, lastErr
	}

	if strategy == "vote" {
		results := make([]*DispatchResult, 0, maxConcurrent)
		var lastErr error
		for i := 0; i < maxConcurrent; i++ {
			select {
			case res := <-ch:
				if res.err == nil && res.r != nil {
					results = append(results, res.r)
				} else if res.err != nil {
					lastErr = res.err
				}
			case <-fanCtx.Done():
				return nil, fmt.Errorf("fan-out timeout: %w", fanCtx.Err())
			}
		}
		if len(results) == 0 {
			return nil, lastErr
		}
		winner := d.MultiModelVote(results)
		agreement := float64(1)
		if len(results) > 1 {
			agreement = float64(1) / float64(len(results))
		}
		logger.Infof("[LLM] fan-out vote decided: providers=%d agreement~%.2f winner_provider=%s",
			len(results), agreement, results[0].Provider)
		for _, r := range results {
			if r.Content == winner {
				return r, nil
			}
		}
		return results[0], nil
	}

	return nil, fmt.Errorf("fan-out strategy %q not implemented", strategy)
}

var fanoutVoteEnabledGetter = func() bool { return false }

func fanoutVoteEnabled() bool { return fanoutVoteEnabledGetter() }

// SetFanoutVoteEnabledGetter 装配层注入（config_param 就绪后调用）
func SetFanoutVoteEnabledGetter(fn func() bool) {
	if fn != nil {
		fanoutVoteEnabledGetter = fn
	}
}
