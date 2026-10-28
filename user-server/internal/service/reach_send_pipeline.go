package service

// reach_send_pipeline.go 同步发送流水线核心定义：步骤常量、错误、请求/响应类型、
// 各环节接口（权限/退订/频控/审计/成本/旅程/渠道适配器）、流水线配置与构造。
// 按职责拆分的其余文件：
//   - reach_send_pipeline_steps.go      Send 编排与各步骤实现（含重试/回退/静默时段守卫）
//   - reach_send_pipeline_components.go 接口的进程内实现与适配器（限流器/审计/成本/旅程/测试适配器）
//   - reach_send_pipeline_quiethours.go 全渠道静默时段（R-4）窗口计算与延迟队列
//   - reach_send_pipeline_compliance.go 合规提醒审计日志（R-8）
//   - reach_send_pipeline_stats.go      带统计的 Pipeline 装饰器

import (
	"context"
	"errors"
	"time"
)

const (
	SendStepPermission = "permission"

	// SendStepCompliancePolicy E1：地区×行业合规策略判定步骤（见 reach_compliance_policy.go）。
	//
	// 位置在 permission 之后、rate_limit 之前：permission 已经判过 DNC 与**全局**静默时段，
	// 这里补的是「地区/行业维度」的更严判据；放在频控之前，是为了让被策略拒掉的消息
	// 不消耗 GCRA 令牌与日配额（否则一次违规尝试会把合法的后续发送挤掉）。
	SendStepCompliancePolicy = "compliance_policy"

	SendStepRateLimit = "rate_limit"

	SendStepRetry = "retry"

	SendStepFallback = "fallback"

	SendStepAudit = "audit"

	SendStepCost = "cost"

	SendStepJourney = "journey"

	SendStepSend = "send"
)

var DefaultSendPipelineSteps = []string{
	SendStepPermission,
	SendStepCompliancePolicy,
	SendStepRateLimit,
	SendStepRetry,
	SendStepFallback,
	SendStepAudit,
	SendStepCost,
	SendStepJourney,
	SendStepSend,
}

var (
	ErrSendPermissionDenied = errors.New("send permission denied")

	// ErrSendDoNotContact R-3a：收件人命中全局跨渠道退订标志位，必须跳过发送
	ErrSendDoNotContact = errors.New("do not contact: recipient opted out globally")

	// ErrSendDoNotContactUnavailable D04：退订状态查询失败，主动外发按拒发处理
	//（fail-closed）。宁可迟发人工核实，不可在状态未知时对客户群发。
	ErrSendDoNotContactUnavailable = errors.New("do not contact: state query unavailable (fail-closed)")

	// ErrSendConsentRevoked D04：客户已撤回该目的的同意，撤回即生效（含队列重发前）
	ErrSendConsentRevoked = errors.New("consent: recipient revoked consent for this purpose")

	// ErrSendConsentRequired D04：enforce 模式下无可信同意凭据，拒发
	//（metadata 自述的 opt_in 只是 legacy_assertion，不能替代凭据）
	ErrSendConsentRequired = errors.New("consent: no trusted consent record for this purpose")

	// ErrSendQuotaDenied D05：配额预占失败（超限/意图冲突），本次外发拒发
	ErrSendQuotaDenied = errors.New("send quota: denied by reservation")

	// ErrSendQuietHoursDeferred R-4：命中全渠道 quiet hours，消息已入延迟队列（非失败）
	ErrSendQuietHoursDeferred = errors.New("send deferred to quiet hours release time")

	ErrSendRateLimited = errors.New("send rate limited")

	ErrSendInsufficientCost = errors.New("insufficient balance for send")

	ErrSendChannelNotConfig = errors.New("channel adapter not configured")

	// ErrSendComplianceDenied E1：命中地区×行业合规策略，本次外发被拒（终态，不重试）。
	// 与 ErrSendQuietHoursDeferred 区分：后者是「换个时间还能发」，本条是「这个渠道/这个地区
	// 在当前策略下根本不该发」。调用方据此决定重排还是放弃。
	ErrSendComplianceDenied = errors.New("send denied by compliance policy")
)

type ReachSendRequest struct {
	Channel     string
	AccountID   string
	RecipientID string
	CustomerID  string
	OperatorID  string
	MsgType     string
	Content     string
	Subject     string
	TemplateID  string
	Params      map[string]string
	Attachments []string
	CardID      string
	Fallback    *FallbackConfig
	Metadata    map[string]string
}

type FallbackConfig struct {
	Enabled       bool
	BackupChannel string
	BackupAccount string
	MaxAttempts   int
}

type SendResponse struct {
	Success        bool          `json:"success"`
	MessageID      string        `json:"message_id"`
	Channel        string        `json:"channel"`
	AccountID      string        `json:"account_id"`
	FallbackUsed   bool          `json:"fallback_used"`
	PrimaryChannel string        `json:"primary_channel"`
	RetryCount     int           `json:"retry_count"`
	StepResults    []SendStepLog `json:"step_results"`
	Error          string        `json:"error,omitempty"`
	DurationMs     int64         `json:"duration_ms"`
	SentAt         time.Time     `json:"sent_at"`
	// Deferred R-4：命中 quiet hours 进入延迟队列时为 true（非失败语义）
	Deferred bool `json:"deferred,omitempty"`
	// DeferredAt R-4：计划首发时间
	DeferredAt time.Time `json:"deferred_at,omitempty"`
}

type SendStepLog struct {
	Step       string    `json:"step"`
	Success    bool      `json:"success"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMs int64     `json:"duration_ms"`
	Output     []any     `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	Skipped    bool      `json:"skipped,omitempty"`
}

type SendPermissionChecker interface {
	CheckSendPermission(ctx context.Context, req *ReachSendRequest) error
}

// DoNotContactChecker R-3a：全局跨渠道退订标志位检查接口
// （由 DoNotContactService 实现，permission 步骤发送前置调用）
type DoNotContactChecker interface {
	IsBlocked(ctx context.Context, oneID, channel string) bool
}

// DoNotContactStrictChecker D04：故障显式的退订检查（DB 失败上抛错误）。
// 主动营销外发在查询失败时必须拒发（fail-closed），不沿用 IsBlocked 的 fail-open。
type DoNotContactStrictChecker interface {
	IsBlockedStrict(ctx context.Context, oneID, channel string) (bool, error)
}

type SendRateLimiter interface {
	Allow(ctx context.Context, key string, limit RateLimitSpec) bool
}

type RateLimitSpec struct {
	QPS          int
	Burst        int
	DailyQuota   int
	PerUserLimit int
}

type SendRetryPolicy struct {
	MaxRetries      int
	IntervalMs      int
	Backoff         string
	MaxIntervalMs   int
	RetryableErrors []string
}

func DefaultSendRetryPolicy() SendRetryPolicy {
	return SendRetryPolicy{
		MaxRetries:    3,
		IntervalMs:    500,
		Backoff:       "exponential",
		MaxIntervalMs: 10000,
	}
}

type SendAuditLogger interface {
	LogSend(ctx context.Context, req *ReachSendRequest, resp *SendResponse)
}

type SendCostTracker interface {
	Charge(ctx context.Context, channel string, req *ReachSendRequest) (cost float64, err error)
}

type SendJourneyTracker interface {
	RecordTouch(ctx context.Context, customerID, channel, source string) error
}

type ChannelAdapter interface {
	Send(ctx context.Context, req *ReachSendRequest) (msgID string, err error)
}

type SendPipeline interface {
	Send(ctx context.Context, req *ReachSendRequest) *SendResponse
}

type SendPipelineConfig struct {
	PermissionChecker SendPermissionChecker
	// DoNotContact R-3a：全局退订标志位检查器（nil 时跳过该检查）
	DoNotContact  DoNotContactChecker
	RateLimiter   SendRateLimiter
	RateLimitSpec RateLimitSpec
	// CompliancePolicy E1：地区×行业合规策略判定器。
	// nil = 该步骤被跳过（记 skipped），行为与升级前一致——这是默认值，不是降级。
	CompliancePolicy CompliancePolicyEvaluator
	RetryPolicy      SendRetryPolicy
	AuditLogger      SendAuditLogger
	CostTracker      SendCostTracker
	JourneyTracker   SendJourneyTracker
	Adapter          ChannelAdapter
	FallbackAdapters map[string]ChannelAdapter
	Steps            []string

	// QuietHoursEnabled R-4：启用全渠道 quiet hours 守卫（22:00-8:00 CST）。
	// 短信渠道不受此守卫影响（既有铁律保持：sms.go 夜间拒绝式拦截）。
	QuietHoursEnabled bool
	// QuietHoursDeferrer R-4：命中 quiet hours 后的延迟入队器；
	// nil 且 QuietHoursEnabled=true 时使用全局 GlobalQuietHoursQueue（惰性创建）。
	QuietHoursDeferrer SendQuietHoursDeferrer
	// QuietHoursClock 可替换时钟（测试注入），生产为 nil 即 time.Now
	QuietHoursClock func() time.Time

	// ConsentChecker D04：可信同意判定（nil = 跳过该闸，零行为变更）。
	ConsentChecker ConsentChecker
	// ConsentMode D04：revocation_only（默认，仅显式撤回拒发）/
	// enforce（无可信凭据一律拒发，存量盘点完成后收紧用）。
	ConsentMode string
	// DoNotContactStrict D04：故障显式退订检查（nil 时退回 DoNotContact 的 fail-open）。
	DoNotContactStrict DoNotContactStrictChecker

	// QuotaReserver D05：意图级硬配额预占（nil = 不设额）。
	QuotaReserver QuotaReserver
	// QuotaScopesFn D05：本次请求的预占维度（nil 或返回空 = 不设额）。
	// 仅当请求带 action_intent_id 时才会被调用——无幂等键不做预占。
	QuotaScopesFn func(ctx context.Context, req *ReachSendRequest) []QuotaScopeDef
}

func DefaultSendPipelineConfig(adapter ChannelAdapter) SendPipelineConfig {
	return SendPipelineConfig{
		PermissionChecker: AllowAllSendPermission{},
		RateLimiter:       NoOpSendRateLimiter{},
		RetryPolicy:       DefaultSendRetryPolicy(),
		AuditLogger:       NewMemorySendAuditLogger(1000),
		CostTracker:       NoOpSendCostTracker{},
		JourneyTracker:    NoOpSendJourneyTracker{},
		Adapter:           adapter,
		FallbackAdapters:  map[string]ChannelAdapter{},
		Steps:             DefaultSendPipelineSteps,
	}
}

var defaultThirdPartyRateLimitByChannel = map[string]RateLimitSpec{
	"sms":          {QPS: 50, Burst: 100},
	"email":        {QPS: 30, Burst: 60},
	"wecom":        {QPS: 30, Burst: 60},
	"weixin":       {QPS: 30, Burst: 60},
	"douyin":       {QPS: 30, Burst: 60},
	"douyin_web":   {QPS: 30, Burst: 60},
	"kuaishou":     {QPS: 30, Burst: 60},
	"kuaishou_web": {QPS: 30, Burst: 60},
	"xiaohongshu":  {QPS: 30, Burst: 60},
	"xhs":          {QPS: 30, Burst: 60},
	"xhs_web":      {QPS: 30, Burst: 60},
	"tiktok":       {QPS: 30, Burst: 60},
	"tiktok_web":   {QPS: 30, Burst: 60},
	"xianyu":       {QPS: 30, Burst: 60},
	"xianyu_web":   {QPS: 30, Burst: 60},
	"dingtalk":     {QPS: 30, Burst: 60},
	"telegram":     {QPS: 30, Burst: 60},
	"whatsapp":     {QPS: 30, Burst: 60},
	"feishu":       {QPS: 30, Burst: 60},
}

func DefaultThirdPartyRateLimitSpec(channel string) RateLimitSpec {
	if spec, ok := defaultThirdPartyRateLimitByChannel[channel]; ok {
		return spec
	}
	return RateLimitSpec{}
}

func NewDefaultRateLimitedPipelineConfigWithCache(adapter ChannelAdapter) SendPipelineConfig {
	cfg := DefaultSendPipelineConfig(adapter)
	if gl := NewGCRARateLimiterFromGlobalCache(); gl != nil {
		cfg.RateLimiter = gl
	} else {
		cfg.RateLimiter = NewMemorySendRateLimiter()
	}
	return cfg
}

type defaultSendPipeline struct {
	config SendPipelineConfig
}

func NewSendPipeline(config SendPipelineConfig) SendPipeline {
	if len(config.Steps) == 0 {
		config.Steps = DefaultSendPipelineSteps
	}
	return &defaultSendPipeline{config: config}
}
