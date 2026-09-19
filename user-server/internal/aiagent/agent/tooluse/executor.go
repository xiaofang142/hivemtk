package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/trace"
	"hivemtk-user/internal/pkg/tracing"
)

// ===== 可观测性 observer（与追踪系统解耦：默认 nil，由 router 在启动时接线） =====
//
// 设计意图：工具执行引擎暴露一个可选 observer 钩子，事件类型来自 tracing 包。
// router 启动时接线：tooluse.ToolTraceSink = tracing.ReportToolCall。
// 从而以「观察者模式」自动记录 agent 多轮（agent_turn）/ 多工具（tool_call）调用，
// 业务代码与工具调用点均无需手写追踪埋点。
var ToolTraceSink func(ctx context.Context, ev tracing.ToolTraceEvent)

type turnIndexKey struct{}

var turnCounters sync.Map

// WithTurnIndex 把当前 agent 轮次序号注入 context，供其下所有 tool_call 继承。
func WithTurnIndex(ctx context.Context, idx int) context.Context {
	return context.WithValue(ctx, turnIndexKey{}, idx)
}

// GetTurnIndex 取出当前 agent 轮次序号。
func GetTurnIndex(ctx context.Context) int {
	if v, ok := ctx.Value(turnIndexKey{}).(int); ok {
		return v
	}
	return 0
}

func nextTurnIndex(ctx context.Context) int {
	key := traceIDFromContext(ctx)
	if key == "" {
		key = "default"
	}
	v, _ := turnCounters.LoadOrStore(key, new(int64))
	return int(atomic.AddInt64(v.(*int64), 1))
}

func traceIDFromContext(ctx context.Context) string {
	if c := tracing.CarrierFromContext(ctx); c != nil && c.TraceID != "" {
		return c.TraceID
	}
	return trace.TraceIDFromContext(ctx)
}

// ToolExecutorConfig 执行器全局配置
type ToolExecutorConfig struct {
	DefaultTimeout    time.Duration
	PermissionChecker PermissionChecker
	RateLimiter       RateLimiter
	RetryPolicy       RetryPolicy
	AuditLogger       AuditLogger
	CostTracker       CostTracker
	CircuitBreaker    *CircuitBreakerRegistry

	// CircuitBreakerShadow true 时熔断装饰器走观察态：照常判定与记账，但不拦下请求。
	// 由接线方按 FF_TOOL_CIRCUIT_BREAKER 设定；CircuitBreaker 为 nil 时无意义。
	CircuitBreakerShadow bool
	// OnCircuitDecision 每次熔断判定回调一次（nil = 不上报）。用于观察期累计与告警。
	OnCircuitDecision CircuitDecisionFunc

	// ApprovalChecker 冷触达审批门检查器；nil = 不接线，行为与现状一致。
	// 仅对 IsColdOutreachTool 为真的工具生效。
	ApprovalChecker ApprovalChecker
	// ApprovalShadow true 时只经 checker 留痕、不拦下请求（FF_LTC_APPROVAL_GATE=shadow）。
	// 由接线方设定；本字段为 true 时 ApprovalGateDecorator 不存在任何拒绝路径。
	ApprovalShadow bool

	FeedbackSink FeedbackSink
}

// ToolOverride 工具级别配置覆盖
type ToolOverride struct {
	ToolName   string
	Timeout    time.Duration
	MaxRetries int
	BaseDelay  time.Duration
	Disabled   bool
}

// ToolExecutor 工具执行引擎
// 线程安全；缓存每个工具的装饰后 handler
type ToolExecutor struct {
	registry      *ToolRegistry
	config        ToolExecutorConfig
	retryPolicies *ToolRetryPolicies

	mu        sync.RWMutex
	overrides map[string]ToolOverride
	cache     map[string]ToolHandler
}

// NewToolExecutor 创建工具执行器
func NewToolExecutor(registry *ToolRegistry, config ToolExecutorConfig) *ToolExecutor {
	if config.DefaultTimeout <= 0 {
		config.DefaultTimeout = 30 * time.Second
	}
	return &ToolExecutor{
		registry:  registry,
		config:    config,
		overrides: make(map[string]ToolOverride),
		cache:     make(map[string]ToolHandler),
	}
}

// SetOverride 设置工具级别覆盖
// 设置后会清除该工具的 handler 缓存，下次执行时重建
func (e *ToolExecutor) SetOverride(override ToolOverride) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.overrides[override.ToolName] = override
	delete(e.cache, override.ToolName)
}

// GetOverride 获取工具级别覆盖
func (e *ToolExecutor) GetOverride(toolName string) (ToolOverride, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	o, ok := e.overrides[toolName]
	return o, ok
}

// ClearOverride 清除工具级别覆盖
func (e *ToolExecutor) ClearOverride(toolName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.overrides, toolName)
	delete(e.cache, toolName)
}

// ClearCache 清空所有 handler 缓存（用于配置变更后）
func (e *ToolExecutor) ClearCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = make(map[string]ToolHandler)
}

func (e *ToolExecutor) clearCacheLocked() {
	e.cache = make(map[string]ToolHandler)
}

// SetRetryPolicies 注入按工具名配置的重试策略
func (e *ToolExecutor) SetRetryPolicies(policies *ToolRetryPolicies) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retryPolicies = policies
	e.clearCacheLocked()
}

// Registry 返回关联的注册中心
func (e *ToolExecutor) Registry() *ToolRegistry {
	return e.registry
}

// ExecuteRequest 执行请求
type ExecuteRequest struct {
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	ToolCtx  *ToolContext   `json:"-"`
}

// ExecuteResult 执行结果
type ExecuteResult struct {
	ToolResult
	Err error `json:"-"`
}

// Execute 执行单个工具
// 流程：
//  1. 从 registry 取工具
//  2. 检查 override.Disabled
//  3. 取出（或构造并缓存）装饰后 handler
//  4. 注入 toolName + toolCtx 到 context
//  5. 调用 handler
func (e *ToolExecutor) Execute(ctx context.Context, req ExecuteRequest) ExecuteResult {
	if req.ToolName == "" {
		return ExecuteResult{
			ToolResult: ErrorResult("", fmt.Errorf("tool_name 不能为空")),
			Err:        fmt.Errorf("tool_name 不能为空"),
		}
	}
	tool, err := e.registry.Get(req.ToolName)
	if err != nil {
		return ExecuteResult{
			ToolResult: ErrorResult(req.ToolName, err),
			Err:        err,
		}
	}
	if o, ok := e.GetOverride(req.ToolName); ok && o.Disabled {
		err := fmt.Errorf("tool %s is disabled", req.ToolName)
		return ExecuteResult{
			ToolResult: ErrorResult(req.ToolName, err),
			Err:        err,
		}
	}
	handler := e.getOrBuildHandler(tool)
	execCtx := WithToolName(ctx, req.ToolName)
	if req.ToolCtx != nil {
		execCtx = WithToolContext(execCtx, req.ToolCtx)
	}
	start := time.Now()
	result, err := handler(execCtx, req.Args)
	if result.Timing.DurationMs == 0 {
		result.Timing.DurationMs = time.Since(start).Milliseconds()
	}
	if result.AuditTrace == "" && req.ToolCtx != nil {
		result.AuditTrace = req.ToolCtx.AuditTrace
	}
	if err != nil || !result.Success {
		if result.ToolName == "" {
			result.ToolName = req.ToolName
		}
		if result.Error == "" {
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "tool execution failed without error detail"
			}
		}
		result.Success = false
	}
	if ToolTraceSink != nil {
		status := "ok"
		if !result.Success || err != nil {
			status = "abnormal"
		}
		ev := tracing.ToolTraceEvent{
			Kind:       model.SpanKindToolCall,
			TraceID:    traceIDFromContext(execCtx),
			ToolName:   req.ToolName,
			TurnIndex:  GetTurnIndex(execCtx),
			Input:      req.Args,
			Output:     result.Data,
			Error:      result.Error,
			DurationMs: result.Timing.DurationMs,
			Status:     status,
		}
		if tc := GetToolContext(execCtx); tc != nil {
			ev.AgentID = tc.AgentID
			ev.SessionID = tc.SessionID
			ev.CustomerID = tc.CustomerID
			ev.CallerID = tc.CallerID
		}
		ToolTraceSink(execCtx, ev)
	}
	return ExecuteResult{ToolResult: result, Err: err}
}

func (e *ToolExecutor) getOrBuildHandler(tool Tool) ToolHandler {
	name := tool.Name()
	e.mu.RLock()
	if h, ok := e.cache[name]; ok {
		e.mu.RUnlock()
		return h
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if h, ok := e.cache[name]; ok {
		return h
	}
	h := e.buildHandler(tool)
	e.cache[name] = h
	return h
}

func (e *ToolExecutor) buildHandler(tool Tool) ToolHandler {
	raw := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		start := time.Now()
		r, err := tool.Execute(ctx, args)
		dur := time.Since(start)
		if r.ToolName == "" {
			r.ToolName = tool.Name()
		}
		if r.ExecutedAt.IsZero() {
			r.ExecutedAt = start
		}
		if r.Timing.DurationMs == 0 {
			r.Timing.DurationMs = dur.Milliseconds()
		}
		return r, err
	}

	timeout := e.config.DefaultTimeout
	policy := e.config.RetryPolicy
	if e.retryPolicies != nil {
		policy = e.retryPolicies.Get(tool.Name())
	}
	if o, ok := e.overrides[tool.Name()]; ok {
		if o.Timeout > 0 {
			timeout = o.Timeout
		}
		if o.MaxRetries >= 0 {
			policy = NewExponentialBackoffPolicy(
				o.MaxRetries+1,
				func() time.Duration {
					if o.BaseDelay > 0 {
						return o.BaseDelay
					}
					return 100 * time.Millisecond
				}(),
				10*time.Second,
			)
		}
	}

	chain := BuildChainWithBreakerDecorator(raw,
		e.config.PermissionChecker,
		e.config.RateLimiter,
		e.circuitBreakerDecorator(),
		policy,
		timeout,
		e.config.AuditLogger,
		e.config.CostTracker,
	)
	if e.config.FeedbackSink != nil {
		chain = FeedbackCollectorDecorator(e.config.FeedbackSink)(chain)
	}
	if e.config.ApprovalChecker != nil && IsColdOutreachTool(tool) {
		// 审批门放在整条链之外（含 feedback）：被拒的冷触达不应消耗限流令牌、不应进重试、
		// 不应被 audit 记成"执行过一次外发"、也不应产生一条工具反馈（它的留痕走 checker 自己的 OnDecision）。
		// shadow 态下这一层是纯透传，位置无所谓；位置真正生效是 T-P1-06 转阻断的时候，
		// 现在定死是为了那时不必再挪——挪一次就要重新证明"少拦/多拦了哪些调用"。
		chain = ApprovalGateDecorator(tool, e.config.ApprovalChecker, e.config.ApprovalShadow)(chain)
	}
	return chain
}

// circuitBreakerDecorator 按配置选出熔断装饰器；未接熔断时返回 nil，
// 使 BuildChainWithBreakerDecorator 退化为 BuildDefaultChain（链序与接线前逐字节一致）。
func (e *ToolExecutor) circuitBreakerDecorator() ToolDecorator {
	if e.config.CircuitBreaker == nil {
		return nil
	}
	if e.config.CircuitBreakerShadow {
		return CircuitBreakerShadowDecorator(e.config.CircuitBreaker, e.config.OnCircuitDecision)
	}
	return CircuitBreakerDecoratorWithObserver(e.config.CircuitBreaker, e.config.OnCircuitDecision)
}

// BatchExecuteRequest 批量执行请求
type BatchExecuteRequest struct {
	Requests       []ExecuteRequest `json:"requests"`
	Parallel       bool             `json:"parallel"`
	MaxConcurrency int              `json:"max_concurrency,omitempty"`
	StopOnError    bool             `json:"stop_on_error,omitempty"`
}

// BatchExecuteResponse 批量执行响应
type BatchExecuteResponse struct {
	Results         []ExecuteResult `json:"results"`
	SuccessCount    int             `json:"success_count"`
	FailedCount     int             `json:"failed_count"`
	TotalDurationMs int64           `json:"total_duration_ms"`
}

// BatchExecute 批量执行工具
func (e *ToolExecutor) BatchExecute(ctx context.Context, req BatchExecuteRequest) BatchExecuteResponse {
	start := time.Now()
	n := len(req.Requests)
	if n == 0 {
		return BatchExecuteResponse{}
	}

	results := make([]ExecuteResult, n)

	if req.Parallel {

		var sem chan struct{}
		if req.MaxConcurrency > 0 {
			sem = make(chan struct{}, req.MaxConcurrency)
		}
		var wg sync.WaitGroup
		for i, r := range req.Requests {
			wg.Add(1)
			if sem != nil {
				sem <- struct{}{}
			}
			go func(idx int, er ExecuteRequest) {
				defer wg.Done()
				if sem != nil {
					defer func() { <-sem }()
				}
				results[idx] = e.Execute(ctx, er)
			}(i, r)
		}
		wg.Wait()
	} else {
		for i, r := range req.Requests {
			results[i] = e.Execute(ctx, r)
			if req.StopOnError && results[i].Err != nil {
				for j := i + 1; j < n; j++ {
					results[j] = ExecuteResult{
						ToolResult: ErrorResult(r.ToolName, fmt.Errorf("skipped due to previous error")),
						Err:        fmt.Errorf("skipped due to previous error"),
					}
				}
				break
			}
		}
	}

	resp := BatchExecuteResponse{
		Results:         results,
		TotalDurationMs: time.Since(start).Milliseconds(),
	}
	for _, r := range results {
		if r.Err == nil && r.Success {
			resp.SuccessCount++
		} else {
			resp.FailedCount++
		}
	}
	return resp
}

// LLMToolCall LLM 返回的 tool_call（OpenAI 兼容格式）
type LLMToolCall struct {
	ID       string          `json:"id"`
	Function LLMToolFunction `json:"function"`
}

// LLMToolFunction LLM 工具调用 function 部分
type LLMToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LLMToolResult 工具执行结果（回传给 LLM 的格式）
type LLMToolResult struct {
	ToolCallID string          `json:"tool_call_id"`
	Content    string          `json:"content"`
	Success    bool            `json:"success"`
	Card       *model.RichCard `json:"card,omitempty"`
}

// DispatchByLLMToolCall 根据 LLM 返回的 tool_call 调度执行
// 一次接收多个 tool_call，并发执行，返回每个 tool_call 的结果
//
// 并发上限控制（默认 5）
// 设计依据：防止 LLM 一次返回 10+ tool_calls 时打爆下游服务
// 5 并发是经过权衡的平衡值：足够覆盖常见业务场景（如 customer.search + order.query 并行），
// 又能保护下游 DB/外部 API 不被突发流量打垮
func (e *ToolExecutor) DispatchByLLMToolCall(ctx context.Context, toolCalls []LLMToolCall, toolCtx *ToolContext) []LLMToolResult {
	if len(toolCalls) == 0 {
		return nil
	}
	turnIdx := nextTurnIndex(ctx)
	dispatchCtx := WithTurnIndex(ctx, turnIdx)
	agentID := ""
	if toolCtx != nil {
		agentID = toolCtx.AgentID
	}
	start := time.Now()
	results := make([]LLMToolResult, len(toolCalls))

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, call := range toolCalls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, c LLMToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = e.executeSingleLLMToolCall(dispatchCtx, c, toolCtx)
		}(i, call)
	}
	wg.Wait()
	turnStatus := tracing.StatusOk
	turnErr := ""
	if dispatchCtx.Err() != nil {
		turnStatus = tracing.StatusAbnormal
		turnErr = dispatchCtx.Err().Error()
	} else {
		for _, r := range results {
			if !r.Success {
				turnStatus = tracing.StatusAbnormal
				break
			}
		}
	}
	if ToolTraceSink != nil {
		ev := tracing.ToolTraceEvent{
			Kind:       model.SpanKindAgentTurn,
			TraceID:    traceIDFromContext(dispatchCtx),
			AgentID:    agentID,
			TurnIndex:  turnIdx,
			Input:      map[string]any{"tool_calls": len(toolCalls)},
			DurationMs: time.Since(start).Milliseconds(),
			Status:     turnStatus,
			Error:      turnErr,
		}
		if turnStatus == tracing.StatusAbnormal && turnErr == "" {
			for _, r := range results {
				if !r.Success {
					if er := extractToolError(r); er != "" {
						ev.Error = er
					}
					break
				}
			}
		}
		ToolTraceSink(dispatchCtx, ev)
	}
	return results
}

func extractToolError(r LLMToolResult) string {
	if r.Content == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Content), &m); err != nil {
		return ""
	}
	if e, ok := m["error"]; ok {
		if s, ok := e.(string); ok {
			return s
		}
	}
	return ""
}

func mustToolErrorJSON(code, msg string) string {
	b, err := json.Marshal(map[string]string{"error": msg, "error_code": code})
	if err != nil {
		return fmt.Sprintf(`{"error":%s,"error_code":%s}`, quoteJSON(msg), quoteJSON(code))
	}
	return string(b)
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (e *ToolExecutor) executeSingleLLMToolCall(ctx context.Context, call LLMToolCall, toolCtx *ToolContext) LLMToolResult {
	args := make(map[string]any)
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return LLMToolResult{
				ToolCallID: call.ID,

				Content: mustToolErrorJSON(ToolErrInvalidParams, fmt.Sprintf("arguments JSON 解析失败：%s", err.Error())),
				Success: false,
			}
		}
	}
	if err := e.preflightToolCall(call.Function.Name, args); err != nil {
		return LLMToolResult{
			ToolCallID: call.ID,
			Content:    mustToolErrorJSON(ClassifyToolError(err), fmt.Sprintf("preflight_check_failed: %s", err.Error())),
			Success:    false,
		}
	}
	execResult := e.Execute(ctx, ExecuteRequest{
		ToolName: call.Function.Name,
		Args:     args,
		ToolCtx:  toolCtx,
	})
	content, _ := json.Marshal(execResult.ToolResult)
	contentStr := string(content)

	const maxContentLen = 4000
	if len(contentStr) > maxContentLen {
		originalLen := len(contentStr)
		contentStr = contentStr[:maxContentLen] + fmt.Sprintf(`...[truncated, original_size=%d]`, originalLen)
	}
	return LLMToolResult{
		ToolCallID: call.ID,
		Content:    contentStr,
		Success:    execResult.Err == nil && execResult.Success,
		Card:       execResult.ToolResult.Card,
	}
}

// ExecuteByName 便捷执行：直接传 toolName + args
func (e *ToolExecutor) ExecuteByName(ctx context.Context, toolName string, args map[string]any) (ToolResult, error) {
	r := e.Execute(ctx, ExecuteRequest{
		ToolName: toolName,
		Args:     args,
	})
	return r.ToolResult, r.Err
}

// ExecuteByNameWithCtx 便捷执行：带 ToolContext
func (e *ToolExecutor) ExecuteByNameWithCtx(ctx context.Context, toolName string, args map[string]any, toolCtx *ToolContext) (ToolResult, error) {
	r := e.Execute(ctx, ExecuteRequest{
		ToolName: toolName,
		Args:     args,
		ToolCtx:  toolCtx,
	})
	return r.ToolResult, r.Err
}

// ListAvailableTools 列出所有可用工具（排除被 disabled 的）
func (e *ToolExecutor) ListAvailableTools() []Tool {
	all := e.registry.List()
	out := make([]Tool, 0, len(all))
	for _, t := range all {
		if o, ok := e.GetOverride(t.Name()); ok && o.Disabled {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ListAvailableLLMFunctions 列出所有可用工具的 LLM Function Calling 格式
func (e *ToolExecutor) ListAvailableLLMFunctions() []LLMFunction {
	tools := e.ListAvailableTools()
	out := make([]LLMFunction, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToLLMFunction(t))
	}
	return out
}

var (
	globalExecutor     *ToolExecutor
	globalExecutorOnce sync.Once
)

// InitGlobalExecutor 初始化全局执行器（应用启动时调用一次）
func InitGlobalExecutor(registry *ToolRegistry, config ToolExecutorConfig) {
	globalExecutorOnce.Do(func() {
		globalExecutor = NewToolExecutor(registry, config)
	})
}

// GetGlobalExecutor 获取全局执行器
// 必须先调用 InitGlobalExecutor 初始化，否则返回 nil
func GetGlobalExecutor() *ToolExecutor {
	return globalExecutor
}

func (e *ToolExecutor) preflightToolCall(toolName string, args map[string]any) error {
	if toolName == "" {
		return fmt.Errorf("tool_name is empty")
	}
	tool, err := e.registry.Get(toolName)
	if err != nil {
		return fmt.Errorf("tool %q not registered", toolName)
	}
	if o, ok := e.GetOverride(toolName); ok && o.Disabled {
		return fmt.Errorf("tool %q is disabled", toolName)
	}
	_ = tool
	return nil
}

// SetGlobalExecutor 替换全局执行器（用于测试 / 热重载）
func SetGlobalExecutor(exec *ToolExecutor) {
	globalExecutor = exec
}
