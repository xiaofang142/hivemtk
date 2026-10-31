package llm

import (
	"context"

	"fmt"
	"strings"
	"unicode"

	"sort"

	"time"

	"encoding/json"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	textutil "hivemtk-user/internal/pkg/utils/text"
	"hivemtk-user/internal/platform"
)

type DispatchRequest struct {
	Scenario        DispatchScenario `json:"scenario"`
	Prompt          string           `json:"prompt"`
	SystemPrompt    string           `json:"system_prompt"`
	MaxTokens       int              `json:"max_tokens"`
	Temperature     float64          `json:"temperature"`
	JSONMode        bool             `json:"json_mode"`
	CacheKey        string           `json:"cache_key"`
	CacheTTL        int              `json:"cache_ttl"`
	ReturnLogprobs  bool             `json:"return_logprobs"`
	TopLogprobs     int              `json:"top_logprobs"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	Messages        []ChatMessage    `json:"messages,omitempty"`
	CanaryKey       string           `json:"canary_key,omitempty"`
	InternalLang    string           `json:"internal_lang,omitempty"`
	TargetLang      string           `json:"target_lang,omitempty"`
	CrossLingual    bool             `json:"cross_lingual,omitempty"`
	GlossaryVersion string           `json:"glossary_version,omitempty"`
	FanOut          *FanOutConfig    `json:"fanout,omitempty"`

	// AgentID / UserID 计量归因：平台垫资模型按它们在用量明细里归因到具体
	// 智能体 / 操作人（否则商户只能看到一笔总账，无法回答"谁用掉的"）。
	// 自带 Key 路径不读这两个字段，所以对存量调用点零影响。
	AgentID string `json:"agent_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

type FanOutConfig struct {
	Enable   bool          `json:"enable"`
	Strategy string        `json:"strategy"`
	Timeout  time.Duration `json:"timeout"`
}

type DispatchResult struct {
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	Content         string     `json:"content"`
	TotalTokens     int        `json:"total_tokens"`
	Cost            float64    `json:"cost"`
	LatencyMs       int        `json:"latency_ms"`
	FromCache       bool       `json:"from_cache"`
	Logprobs        []float64  `json:"logprobs,omitempty"`
	TopTokenEntropy float64    `json:"top_token_entropy,omitempty"`
	FinishReason    string     `json:"finish_reason,omitempty"`
	ToolCalls       []ToolCall `json:"tool_calls,omitempty"`
	Usage           TokenUsage `json:"usage,omitempty"`
	BaseURL         string     `json:"base_url,omitempty"`
	IsFallback      bool       `json:"is_fallback,omitempty"`
	TokenSource     string     `json:"token_source,omitempty"`
	Estimator       string     `json:"estimator,omitempty"`
	PromptCost      float64    `json:"prompt_cost,omitempty"`
	CompletionCost  float64    `json:"completion_cost,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (d *Dispatcher) pickEnabledFallback(route *ScenarioRoute) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	type pe struct {
		name  string
		score float64
	}
	list := make([]pe, 0, len(d.providers))
	for name, p := range d.providers {
		if !p.Enabled {
			continue
		}
		if route != nil && route.MinQuality > 0 && p.QualityScore < route.MinQuality {
			continue
		}
		list = append(list, pe{name: name, score: p.QualityScore})
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	return list[0].name
}

func degradedReply(req DispatchRequest) *DispatchResult {

	tmpl := ""
	if fo := GetGlobalFailover(); fo != nil {
		tmpl = ResolveDegradedTemplate(req.Scenario, fo.Config().TemplateReply)
	} else {
		tmpl = TemplateReplyFor(req.Scenario)
	}
	promptTokens := estimateTokens(req.Prompt)
	completionTokens := estimateTokens(tmpl)
	return &DispatchResult{
		Provider:     "degraded",
		Model:        "template",
		Content:      tmpl,
		FinishReason: "degraded",
		Usage: TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func (d *Dispatcher) callProvider(ctx context.Context, provider *ProviderConfig, req DispatchRequest, route *ScenarioRoute) (*DispatchResult, error) {

	if provider.KeyInvalid {
		// 密文解不开：直接给可执行文案，别让上游的 401 顶到用户脸上
		return nil, fmt.Errorf("提供商 %s 的 API Key 无法解密（主密钥可能已轮换），请在「系统设置 → 模型提供商」重新保存密钥", provider.Name)
	}

	if provider.APIKey == "" {
		logger.Warnf("[LLM] WARN: provider %s APIKey empty (local model is fine; cloud will 401)", provider.Name)
	}

	if route.MaxLatency > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(route.MaxLatency)*time.Millisecond)
		defer cancel()
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {

		maxTokens = 2048
	}
	temperature := req.Temperature
	if temperature <= 0 {
		temperature = 0.7
	}

	config := &LLMConfig{
		APIKey:      provider.APIKey,
		BaseURL:     provider.BaseURL,
		APIType:     provider.APIType,
		Model:       provider.Model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		// 与同包 GetDefaultConfig 对齐取 3：取 1 时网关一次瞬时 5xx 就直接降级成
		// "抱歉，AI 服务暂时不可用"落到客户脸上（route 无 fallbacks 时无人接手）。
		MaxRetries:     3,
		RequestTimeout: route.MaxLatency / 1000,
		SystemPrompt:   req.SystemPrompt,
		// 平台垫资 provider 的分流信息：Source 决定 LLMService 走代理还是直连，
		// ProviderName 决定代理侧寻址哪个真实 provider，其余是计量归因上下文。
		Source:       provider.ProviderSource(),
		ProviderName: provider.Name,
		Scenario:     string(req.Scenario),
		TraceID:      traceContextTraceID(ctx),
		AgentID:      req.AgentID,
		UserID:       req.UserID,
	}
	if req.JSONMode {
		config.ResponseFormat = "json_object"
	}

	if req.ReturnLogprobs {
		config.Logprobs = true
		if req.TopLogprobs > 0 {
			config.TopLogprobs = req.TopLogprobs
		} else {
			config.TopLogprobs = 20
		}
	}

	config.Tools = req.Tools
	config.ToolChoice = req.ToolChoice
	config.Messages = req.Messages

	reactMode := IsReActMode(&req, provider.NoFC)
	logger.Infof("[Dispatcher] provider=%s NoFC=%v reactMode=%v tools=%d",
		provider.Name, provider.NoFC, reactMode, len(req.Tools))
	if reactMode {

		originalSystem := config.SystemPrompt
		config.SystemPrompt = d.getReActAdapter().WrapSystemPrompt(originalSystem, req.Tools)

		config.Tools = nil
		config.ToolChoice = ""

	}

	traceID := ""
	if c := tracing.CarrierFromContext(ctx); c != nil {
		traceID = c.TraceID
	}
	if traceID == "" {
		traceID = tracing.TraceIDFromContext(ctx)
	}
	spanID := generateSpanID()
	parentSpanID := ""

	start := time.Now()

	result, err := d.llmService.GenerateWithTools(ctx, config, req.Prompt)
	latency := int(time.Since(start).Milliseconds())

	llmStatus := "ok"
	llmErrMsg := ""
	if err != nil {
		llmStatus = "error"
		llmErrMsg = err.Error()
	}
	finishReason := ""
	if result != nil {
		finishReason = result.FinishReason
	}
	tenantID, _ := tenant.FromContext(ctx)
	PublishLLMCall(
		tenantID, traceID, spanID, parentSpanID,
		provider.Name, provider.Model,
		int64(latency), llmStatus,
		map[string]any{
			"scenario":      string(req.Scenario),
			"latency_ms":    latency,
			"finish_reason": finishReason,
			"error":         llmErrMsg,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", provider.Name, err)
	}

	promptTokens := result.Usage.PromptTokens
	completionTokens := result.Usage.CompletionTokens
	totalTokens := result.Usage.TotalTokens

	// total_tokens 缺失时的分层处理。部分自建网关/中转只回 prompt+completion、不回 total，
	// 这时必须信任分列值（total = 分列之和）；全部缺失才按字符数估算兜底——
	// 否则会把厂商给的真实用量丢掉换成估算值，预付费场景下商户账单与厂商账单对不上。
	//
	// 1. total 有 → 直接用；2. total 无但分列有 → 信任分列；3. 全无 → 估算兜底。
	if totalTokens <= 0 && (promptTokens > 0 || completionTokens > 0) {
		totalTokens = promptTokens + completionTokens
	}
	if promptTokens <= 0 && completionTokens <= 0 {
		promptTokens = estimateTokens(req.Prompt)
		completionTokens = estimateTokens(result.Content)
		totalTokens = promptTokens + completionTokens
	} else if totalTokens <= 0 {
		totalTokens = promptTokens + completionTokens
	}
	if totalTokens <= 0 {
		totalTokens = 1 // 兜底：绝不能是 0，否则下面除以它的地方会得到 NaN
	}
	// 计价：按输入/输出分别乘各自单价（computeCost），非「总价按比例劈两半」。
	// 这决定了 llm_routing_logs 里 prompt_cost / completion_cost 两列的口径。
	cost, promptCost, completionCost := computeCost(*provider, promptTokens, completionTokens)
	tokenSource := InferTokenSource(result.Usage.TotalTokens, result.Content)
	estimator := ClassifyEstimator(tokenSource)

	var usage TokenUsage
	if result.Usage.TotalTokens > 0 {
		usage = TokenUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
	}
	dispatchResult := &DispatchResult{
		Provider:       provider.Name,
		Model:          provider.Model,
		Content:        result.Content,
		TotalTokens:    totalTokens,
		Cost:           cost,
		LatencyMs:      latency,
		FinishReason:   result.FinishReason,
		ToolCalls:      result.ToolCalls,
		Usage:          usage,
		BaseURL:        provider.BaseURL,
		TokenSource:    tokenSource,
		Estimator:      estimator,
		PromptCost:     promptCost,
		CompletionCost: completionCost,
	}

	if reactMode {
		dispatchResult = d.getReActAdapter().AdaptResult(dispatchResult)
	}

	// 平台垫资 provider：把本次用量回填给上层（落本地明细 + 更新余额镜像）。
	// 位置在成功之后（失败调用不计量）、react 适配之后（其不改变 token 数）。
	// 这里**不扣费** —— 扣费的权威在平台代理侧（转发时实时扣），本地再扣一次
	// 就是两本账，重试时会二次扣。
	if provider.IsPlatformProvider() && dispatchResult != nil {
		if sink := PlatformUsageSink(); sink != nil {
			tenantID, _ := tenant.FromContext(ctx)
			sink(PlatformUsageEvent{
				Provider:         provider.Name,
				Model:            provider.Model,
				PlanCode:         provider.PlanCode,
				Scenario:         string(req.Scenario),
				TraceID:          traceID,
				AgentID:          req.AgentID,
				UserID:           req.UserID,
				TenantCode:       platform.TenantCodeFromContext(ctx),
				TenantID:         tenantID,
				PromptTokens:     int64(promptTokens),
				CompletionTokens: int64(completionTokens),
				InputCostCents:   int64(promptCost * 100),
				OutputCostCents:  int64(completionCost * 100),
			})
		}
	}
	return dispatchResult, nil
}

// DispatchStructured 结构化输出
func (d *Dispatcher) DispatchStructured(ctx context.Context, req DispatchRequest, schema any) (*DispatchResult, error) {
	req.JSONMode = true
	result, err := d.Dispatch(ctx, req)
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(result.Content)
	if jsonStr == "" {
		// response_format=json_object 只是入参约定，兼容网关可以整段忽略它回散文。
		// 兜底：把"只输出 JSON"补成最后一条消息再打一次，仍取不到才认失败。
		retryReq := req
		retryReq.CacheKey = "" // 重试不得复用首轮缓存，否则原样取回同一段散文
		retryReq.Messages = appendJSONRetryMessage(req.Messages, req.Prompt)
		if r2, err2 := d.Dispatch(ctx, retryReq); err2 == nil {
			if s2 := extractJSON(r2.Content); s2 != "" {
				result, jsonStr = r2, s2
			}
		}
	}
	if jsonStr == "" {
		return result, fmt.Errorf("no JSON content in response: %s", textutil.Truncate(result.Content, 200))
	}
	if err := json.Unmarshal([]byte(jsonStr), schema); err != nil {
		return result, fmt.Errorf("parse JSON: %w", err)
	}
	result.Content = jsonStr
	return result, nil
}

// jsonRetryInstruction 追加在重试请求末尾的硬约束。
const jsonRetryInstruction = "上一条回复不是合法 JSON。现在只输出目标 JSON 对象本身：不要解释文字、不要 markdown 代码块、不要前后缀。"

// appendJSONRetryMessage 保留原对话、追加一条硬约束消息。
// 原请求走 Messages 时接在其后；只走 Prompt 时把约束拼进 Prompt（Messages 为空时才会用 Prompt）。
func appendJSONRetryMessage(msgs []ChatMessage, prompt string) []ChatMessage {
	if len(msgs) > 0 {
		out := make([]ChatMessage, 0, len(msgs)+1)
		out = append(out, msgs...)
		return append(out, ChatMessage{Role: "user", Content: jsonRetryInstruction})
	}
	if prompt == "" {
		return []ChatMessage{{Role: "user", Content: jsonRetryInstruction}}
	}
	return []ChatMessage{{Role: "user", Content: prompt + "\n\n" + jsonRetryInstruction}}
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	cn := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			cn++
		}
	}
	en := len(text) - cn*3
	if en < 0 {
		en = len(text)
	}
	return cn + en/4
}

// GetProviderList 获取所有 provider
func (d *Dispatcher) GetProviderList() []ProviderConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	list := make([]ProviderConfig, 0, len(d.providers))
	for _, p := range d.providers {
		list = append(list, *p)
	}
	return list
}

// GetAllProviders 获取所有 provider（GetProviderList 的语义别名）。
// 注意：GetProviderList / GetProvider / GetRoute / RemoveProvider 的实际定义
// 在 dispatcher_registry.go（保持向后兼容的返回签名）。
func (d *Dispatcher) GetAllProviders() []ProviderConfig {
	return d.GetProviderList()
}

// GetAllRoutes 获取所有路由（语义别名）
func (d *Dispatcher) GetAllRoutes() []ScenarioRoute {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ScenarioRoute, 0, len(d.routes))
	for _, r := range d.routes {
		out = append(out, *r)
	}
	return out
}

// CallProviderForTest 调 provider 一次（用于管理后台测试 provider 连通性），不触发告警/熔断/统计：
// 不调 AlertProviderFailure/Success、不调 LogRoutingDecision、不更新 provider.AvgLatencyMs；
// 走 callProvider 同一逻辑（testMode 开关屏蔽副作用）。
func (d *Dispatcher) CallProviderForTest(ctx context.Context, provider *ProviderConfig, req DispatchRequest, route *ScenarioRoute) (*DispatchResult, error) {

	d.testMode.Store(true)
	defer d.testMode.Store(false)

	return d.callProvider(ctx, provider, req, route)
}

// DispatchMultiModel 多模型投票（用于异议处理等高质量场景）
func (d *Dispatcher) DispatchMultiModel(ctx context.Context, req DispatchRequest, providers []string) ([]*DispatchResult, error) {
	results := make([]*DispatchResult, 0, len(providers))
	for _, name := range providers {
		d.mu.RLock()
		provider, ok := d.providers[name]
		d.mu.RUnlock()
		if !ok || !provider.Enabled || provider.APIKey == "" {
			continue
		}

		if fo := GetGlobalFailover(); fo != nil && fo.IsCircuitOpen(name) {
			logger.Debugf("[LLM] DispatchMultiModel provider=%s 集群熔断中，跳过", name)
			continue
		}
		result, err := d.callProvider(ctx, provider, req, &ScenarioRoute{MaxLatency: 10000})
		if err != nil {

			if fo := GetGlobalFailover(); fo != nil {
				fo.RecordFailure(name, err)
			}
			continue
		}

		if fo := GetGlobalFailover(); fo != nil {
			fo.RecordSuccess(name, int64(result.LatencyMs))
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("all providers failed")
	}
	return results, nil
}

// DefaultVoteAgreementThreshold 投票一致阈值的代码兜底；生效值走 VoteAgreementThreshold()。
const DefaultVoteAgreementThreshold = 0.80

var voteAgreementThresholdProvider func() float64

// SetVoteAgreementThresholdProvider 注入投票一致阈值；传 nil 视为不注入。
func SetVoteAgreementThresholdProvider(fn func() float64) {
	voteAgreementThresholdProvider = fn
}

// VoteAgreementThreshold 生效的投票一致阈值。区间 (0,1]：非正数会让任何一对答案都算一致
// （投票直接退化成随便选一个），大于 1 则永远选不出共识。越界一律回落兜底。
func VoteAgreementThreshold() float64 {
	if p := voteAgreementThresholdProvider; p != nil {
		if v := p(); v > 0 && v <= 1 {
			return v
		}
	}
	return DefaultVoteAgreementThreshold
}

func normalizeVoteText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func bigramJaccard(a, b string) float64 {
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		if a == "" && b == "" {
			return 1
		}
		return 0
	}
	setA := make(map[string]struct{}, len(ra))
	for i := 0; i+1 < len(ra); i++ {
		setA[string(ra[i:i+2])] = struct{}{}
	}
	setB := make(map[string]struct{}, len(rb))
	inter := 0
	for i := 0; i+1 < len(rb); i++ {
		g := string(rb[i : i+2])
		setB[g] = struct{}{}
		if _, ok := setA[g]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// MultiModelVote 多模型投票返回最一致答案：
// 1. 归一化后计算两两 bigram-Jaccard 相似度；
// 2. 支持数 = 与自身相似度 ≥阈值 的答案数（含自身）；
// 3. 存在严格过半多数派 → 返回该派系中 QualityScore 最高者的原文；
// 4. 无过半多数派 → 回退 QualityScore 最高者。
func (d *Dispatcher) MultiModelVote(results []*DispatchResult) string {
	if len(results) == 0 {
		return ""
	}
	if len(results) == 1 {
		return results[0].Content
	}

	norms := make([]string, len(results))
	for i, r := range results {
		norms[i] = normalizeVoteText(r.Content)
	}
	quality := make([]float64, len(results))
	for i, r := range results {
		d.mu.RLock()
		p, ok := d.providers[r.Provider]
		d.mu.RUnlock()
		if ok {
			quality[i] = p.QualityScore
		}
	}

	support := make([]int, len(results))
	for i := range norms {
		for j := range norms {
			if i != j && bigramJaccard(norms[i], norms[j]) >= VoteAgreementThreshold() {
				support[i]++
			}
		}
	}

	majoritySize := len(results)/2 + 1
	bestIdx, bestQuality := -1, -1.0
	hasMajority := false
	for i := range results {
		if support[i]+1 >= majoritySize {
			hasMajority = true
			break
		}
	}
	if hasMajority {
		for i := range results {
			if support[i]+1 >= majoritySize && quality[i] > bestQuality {
				bestQuality = quality[i]
				bestIdx = i
			}
		}
		logger.Debugf("[LLM] MultiModelVote 多数派命中: %d/%d", support[bestIdx]+1, len(results))
	} else {

		for i := range results {
			if quality[i] > bestQuality {
				bestQuality = quality[i]
				bestIdx = i
			}
		}
		logger.Debugf("[LLM] MultiModelVote 无过半多数派，回退质量分最高: provider=%s", results[bestIdx].Provider)
	}
	return results[bestIdx].Content
}
