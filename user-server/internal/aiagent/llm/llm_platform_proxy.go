package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/platform"
)

// ============================================================
// 平台垫资模型（source='platform'）的调用路径
//
// 与自带 Key 路径的**唯一**区别是最后一步发到哪里：
//   自带 Key：user-server ──────────────→ 厂商（Key 由商户自己保管）
//   平台垫资：user-server → 平台代理 ──→ 厂商（Key 只在平台侧）
//
// 平台不下发真实 Key：下发等于把垫资凭据交出去，
// 商户可以直连厂商无限量用，平台既无法计量也无法止损。
//
// 扣费的**权威在平台侧**：代理转发时按「输入 token × 输入单价 +
// 输出 token × 输出单价」实时扣减，余额不足直接返回 402 且请求根本
// 到不了厂商。本地不再二次扣减 —— 两次扣账是预付系统最贵的 bug。
// ============================================================

// LLMProviderSourcePlatform provider 来源常量：平台垫资
const LLMProviderSourcePlatform = "platform"

// platformProxyCaller 代理客户端的取用点（包级变量，便于测试替换）
var platformProxyCaller = func() *platform.LLMProxyClient {
	return platform.NewLLMProxyClient()
}

// SetPlatformProxyCallerForTest 注入替身（测试用）
func SetPlatformProxyCallerForTest(fn func() *platform.LLMProxyClient) {
	if fn == nil {
		platformProxyCaller = func() *platform.LLMProxyClient { return platform.NewLLMProxyClient() }
		return
	}
	platformProxyCaller = fn
}

// IsPlatformProxyConfigured 平台代理是否可用（未配置地址/密钥时返回 false）。
//
// 调用方据此把 source='platform' 的 provider 当作**不可用**处理，
// 让调度器走 fallback —— 而不是发一次注定 401 的请求再失败。
func IsPlatformProxyConfigured() bool {
	return platformProxyCaller().Available()
}

// callPlatformProxy 走平台代理发起一次调用
func (s *LLMService) callPlatformProxy(ctx context.Context, config *LLMConfig, body []byte) (*chatResponse, error) {
	client := platformProxyCaller()
	if !client.Available() {
		return nil, fmt.Errorf("平台模型未配置（PLATFORM_API_URL / PLATFORM_MERCHANT_KEY 缺失）：" +
			"请在系统设置中配置平台地址后重试")
	}
	meta := platform.LLMCallMeta{
		Scenario: config.Scenario,
		TraceID:  config.TraceID,
		AgentID:  config.AgentID,
		UserID:   config.UserID,
		// 平台按 (merchant_key, tenant_code) 每租户一行记账：从 ctx 解析本租户
		// code，取不到（无绑定/查不到行/库未就绪）= 空串照发，平台走旧行为兜底。
		TenantCode: platform.TenantCodeFromContext(ctx),
	}
	resp, err := client.ChatCompletions(ctx, config.ProviderName, body, meta)
	if err != nil {
		return nil, fmt.Errorf("调用平台模型失败: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// 走原解析路径：报文是厂商原样转发，OpenAI 兼容结构不变
		return parseChatResponse(resp.Body)

	case http.StatusPaymentRequired:
		// 余额不足。**必须**单独一类错误：调度器与上层要据此提示"去充值"，
		// 而不是笼统的"模型不可用"——后者会让用户反复重试直到余额耗尽。
		return nil, ErrInsufficientLLMBalance

	case http.StatusServiceUnavailable:
		return nil, fmt.Errorf("平台模型未启用: %s", truncateForError(resp.Body))

	case http.StatusUnauthorized:
		return nil, fmt.Errorf("平台鉴权失败（商户密钥无效）: %s", truncateForError(resp.Body))

	default:
		return nil, fmt.Errorf("平台代理返回 status=%d: %s", resp.StatusCode, truncateForError(resp.Body))
	}
}

// parseChatResponse 解析 OpenAI 兼容响应
func parseChatResponse(body []byte) (*chatResponse, error) {
	var out chatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析平台模型响应失败: %w", err)
	}
	return &out, nil
}

// truncateForError 截断错误报文，避免把整段 HTML / 大 JSON 打进日志
func truncateForError(body []byte) string {
	const max = 300
	s := string(body)
	if len(s) > max {
		return s[:max] + "…(truncated)"
	}
	return s
}

// ErrInsufficientLLMBalance 平台模型余额不足。
//
// 单独一类错误（而不是笼统的 "调用失败"）的理由：处置动作完全不同 ——
// 余额不足要引导"去充值"，调用失败要引导"稍后重试或换模型"。
// 混在一起会让用户在余额已经为负时反复重试，直到把失败日志刷满。
var ErrInsufficientLLMBalance = errors.New("平台模型余额不足，请购买 LLM 套餐后重试")

// IsBalanceInsufficient 判断错误是否余额不足（控制层据此映射业务码）
func IsBalanceInsufficient(err error) bool { return errors.Is(err, ErrInsufficientLLMBalance) }

// traceContextTraceID 从 ctx 取 trace_id（计量幂等键的一半）。
// 放在这里而不是让 dispatcher 传进来：dispatcher 里取 trace 的代码在
// config 构造**之后**，而 config 构造就需要 trace_id。
func traceContextTraceID(ctx context.Context) string {
	if c := tracing.CarrierFromContext(ctx); c != nil && c.TraceID != "" {
		return c.TraceID
	}
	return tracing.TraceIDFromContext(ctx)
}

// ============================================================
// 用量回填（平台垫资路径专用）
//
// 抽成可注入的 sink 而不是让 llm 包直接 import service：
// aiagent/llm 是底层包，service 依赖它，反向依赖会成环。装配期由
// cmd/api/main.go 注入实现；未注入时只是**不记本地明细**，
// 而扣费仍然照常在平台侧执行 —— 记账失败不能反过来影响用户能否用。
// ============================================================

// PlatformUsageEvent 一次平台垫资调用的用量
type PlatformUsageEvent struct {
	Provider string
	Model    string
	PlanCode string
	Scenario string
	TraceID  string
	AgentID  string
	UserID   string
	// TenantCode 本次调用归属的租户短码（落本地镜像明细用，与代理调用 query
	// 同源）。空串 = 取不到租户归属，本地明细照落（tenant_id 列仍按隔离
	// callback 口径）。
	TenantCode string
	// TenantID 本次调用上下文的租户绑定。0 = ctx 未绑定（on 模式下明细
	// 落库会被隔离 callback 拒绝），本地 sink 用它重建租户上下文。
	TenantID         uint
	PromptTokens     int64
	CompletionTokens int64
	InputCostCents   int64
	OutputCostCents  int64
}

// PlatformUsageSink 用量回填函数（装配期注入）
var platformUsageSink func(PlatformUsageEvent)

// SetPlatformUsageSink 注入用量回填实现；传 nil 视为取消注入
func SetPlatformUsageSink(fn func(PlatformUsageEvent)) { platformUsageSink = fn }

// PlatformUsageSink 取当前注入的 sink
func PlatformUsageSink() func(PlatformUsageEvent) { return platformUsageSink }

// ============================================================
// 平台套餐单价快照（平台 provider 写入时点计价的唯一来源）
//
// 平台不下发 provider 价格（ProviderViewsForDeliver 无价列），但套餐接口
// （Plans()）带「分/1k token」单价。用户端 sync 把两者按 plan_code 关联落到
// 本地 provider 镜像的价列上；这里只留一个取价 seam（装配期由 service 注入，
// 理由与 sink 相同：llm 是底层包，不能反向依赖 service/repository）。
//
// 取不到价（未同步/套餐下架/库未就绪/未注入）→ (0,0) → 成本如实记 0，
// 不报错、不编价。平台 provider 自带的 CostPer1k* 浮点字段**不再**作为
// 平台调用的计价来源 —— 那等于第二定价源，且 float 乘除与平台的整数口径
// 在边界值上会差一分。
// ============================================================

// platformPlanPriceResolver 平台套餐单价解析（plan_code → 分/1k 单价对）
var platformPlanPriceResolver func(planCode string) (inputPer1kCents, outputPer1kCents int64)

// SetPlatformPlanPriceResolver 注入平台套餐单价解析实现；传 nil 视为取消注入
func SetPlatformPlanPriceResolver(fn func(planCode string) (inputPer1kCents, outputPer1kCents int64)) {
	platformPlanPriceResolver = fn
}

// PlatformPlanPriceResolver 取当前注入的单价解析实现
func PlatformPlanPriceResolver() func(planCode string) (inputPer1kCents, outputPer1kCents int64) {
	return platformPlanPriceResolver
}

// PriceTokensCents 按「分/1k token」整数计价，与平台侧 PriceTokens
// （platform-server llm_billing_service.go）同式同截断：整数乘除、向下取整、
// 不四舍五入 —— 少收一分比多收一分安全。禁 float64：浮点乘除会在边界值上
// 与平台口径差出一分（如 tokens*price 恰好差 1e-9 跨过整数线）。
func PriceTokensCents(tokens, pricePer1kCents int64) int64 {
	if tokens <= 0 || pricePer1kCents <= 0 {
		return 0
	}
	return tokens * pricePer1kCents / 1000
}
