package agent_runtime

import (
	"time"
)

// Sentiment 情绪标签
type Sentiment string

const (
	SentimentCalm     Sentiment = "calm"
	SentimentAnxious  Sentiment = "anxious"
	SentimentAngry    Sentiment = "angry"
	SentimentAppreci  Sentiment = "appreci"
	SentimentConfused Sentiment = "confused"
	SentimentUnknown  Sentiment = "unknown"
)

// SentimentScore 情绪打分（0-1）
type SentimentScore struct {
	Label  Sentiment             `json:"label"`
	Score  float64               `json:"score"`
	Detail map[Sentiment]float64 `json:"detail,omitempty"`
}

// Intent 意图分类
type Intent string

const (
	IntentChitchat       Intent = "chitchat"
	IntentInquiry        Intent = "inquiry"
	IntentOrderStatus    Intent = "order_status"
	IntentComplaint      Intent = "complaint"
	IntentRefund         Intent = "refund"
	IntentAfterSales     Intent = "after_sales"
	IntentSalesLead      Intent = "sales_lead"
	IntentGreeting       Intent = "greeting"
	IntentFarewell       Intent = "farewell"
	IntentHandoffToHuman Intent = "handoff_human"
	IntentFAQ            Intent = "faq"
	IntentUnknown        Intent = "unknown"
)

// IntentResult 意图识别结果
type IntentResult struct {
	Primary   Intent            `json:"primary"`
	Secondary []Intent          `json:"secondary,omitempty"`
	Score     float64           `json:"score"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// AlignmentDimension 拟人度维度
type AlignmentDimension string

const (
	DimEmpathy    AlignmentDimension = "empathy"
	DimEnthusiasm AlignmentDimension = "enthusiasm"
	DimExpertise  AlignmentDimension = "expertise"
	DimPatience   AlignmentDimension = "patience"
	DimClarity    AlignmentDimension = "clarity"
	DimPoliteness AlignmentDimension = "politeness"
)

// AlignmentScore 6维拟人度评分（每个维度 1-5 分）
type AlignmentScore struct {
	Empathy    int `json:"empathy"`
	Enthusiasm int `json:"enthusiasm"`
	Expertise  int `json:"expertise"`
	Patience   int `json:"patience"`
	Clarity    int `json:"clarity"`
	Politeness int `json:"politeness"`
}

// Total 综合分（加权平均）
func (a AlignmentScore) Total() float64 {
	sum := a.Empathy + a.Enthusiasm + a.Expertise + a.Patience + a.Clarity + a.Politeness
	return float64(sum) / 6.0
}

// MaxDimension 返回最需要提升的维度（分最低）
func (a AlignmentScore) MaxDimension() AlignmentDimension {
	minDim := DimEmpathy
	minScore := a.Empathy
	if a.Enthusiasm < minScore {
		minDim = DimEnthusiasm
		minScore = a.Enthusiasm
	}
	if a.Expertise < minScore {
		minDim = DimExpertise
		minScore = a.Expertise
	}
	if a.Patience < minScore {
		minDim = DimPatience
		minScore = a.Patience
	}
	if a.Clarity < minScore {
		minDim = DimClarity
		minScore = a.Clarity
	}
	if a.Politeness < minScore {
		minDim = DimPoliteness
	}
	return minDim
}

// CrisisLevel 危机感等级
type CrisisLevel int

const (
	CrisisNone   CrisisLevel = 0
	CrisisLow    CrisisLevel = 1
	CrisisMedium CrisisLevel = 2
	CrisisHigh   CrisisLevel = 3
)

// CrisisSignal 危机信号
type CrisisSignal struct {
	Level      CrisisLevel `json:"level"`
	Triggers   []string    `json:"triggers"`
	Reason     string      `json:"reason"`
	DetectedAt time.Time   `json:"detected_at"`
}

// NeedsEscalation 是否需要转人工
func (c CrisisSignal) NeedsEscalation() bool {
	return c.Level >= CrisisHigh
}

// ActionPlan 任务规划器输出
type ActionPlan struct {
	PlanType   string            `json:"plan_type"`
	ToolCalls  []PlannedToolCall `json:"tool_calls,omitempty"`
	ReplyHint  string            `json:"reply_hint,omitempty"`
	SkipLLM    bool              `json:"skip_llm"`
	SkipReason string            `json:"skip_reason,omitempty"`
	Confidence float64           `json:"confidence"`
	// PlanSource 规划来源观测（TB-01）："chain"=绑定层数据驱动；"builtin:miss/rejected/error"=回落硬编码及原因；空=未装配绑定层的既有行为。
	PlanSource string `json:"plan_source,omitempty"`
}

// PlannedToolCall 计划工具调用
type PlannedToolCall struct {
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	Priority int            `json:"priority"`
}

// StageDecision 阶段决策记录（用于可观测与可调试）
type StageDecision struct {
	Stage    string        `json:"stage"`
	Action   string        `json:"action"`
	Reason   string        `json:"reason,omitempty"`
	Duration time.Duration `json:"duration_ns"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
}

// InferenceContext 推理上下文
//
// 编排器在每阶段调用时传递的"数据袋"，所有阶段都从这里读输入 + 写输出。
// 严格不可变（写时复制），便于并发快照与可观测回放。
type InferenceContext struct {
	Payload  CustomerMessagePayload `json:"payload"`
	AgentCtx *AgentContext          `json:"agent_ctx"`

	Sentiment SentimentScore `json:"sentiment,omitempty"`
	Intent    IntentResult   `json:"intent,omitempty"`

	Alignment AlignmentScore `json:"alignment,omitempty"`

	Crisis CrisisSignal `json:"crisis,omitempty"`

	Plan *ActionPlan `json:"plan,omitempty"`

	EpisodicMemory string `json:"episodic_memory,omitempty"`

	StartTime time.Time       `json:"start_time"`
	Stages    []StageDecision `json:"stages"`

	Decision InferenceDecision `json:"decision"`
}

// InferenceDecision 最终决策
type InferenceDecision struct {
	HandoffToHuman bool           `json:"handoff_to_human"`
	HandoffReason  string         `json:"handoff_reason,omitempty"`
	Plan           *ActionPlan    `json:"plan,omitempty"`
	Reply          string         `json:"reply,omitempty"`
	ReplyType      string         `json:"reply_type,omitempty"`
	Confidence     float64        `json:"confidence"`
	StopReason     string         `json:"stop_reason,omitempty"`
	TotalDuration  time.Duration  `json:"total_duration_ns"`
	Crisis         CrisisSignal   `json:"crisis,omitempty"`
	Sentiment      SentimentScore `json:"sentiment,omitempty"`
	Intent         IntentResult   `json:"intent,omitempty"`
	Alignment      AlignmentScore `json:"alignment,omitempty"`
	Review         *ReviewResult  `json:"review,omitempty"`
}

// StageResult 阶段执行结果
type StageResult struct {
	Continue    bool
	EarlyReturn bool
	Decision    *InferenceDecision
	Error       error
}

// ContinueResult 简单的"继续"结果
func ContinueResult() StageResult {
	return StageResult{Continue: true}
}

// StopResult 立即终止（成功）
func StopResult(decision *InferenceDecision) StageResult {
	return StageResult{
		Continue:    false,
		EarlyReturn: true,
		Decision:    decision,
	}
}

// FailResult 立即终止（失败）
func FailResult(err error) StageResult {
	return StageResult{
		Continue:    false,
		EarlyReturn: true,
		Error:       err,
	}
}
