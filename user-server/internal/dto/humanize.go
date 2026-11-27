package dto

import (
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

// HumanizeDimension 拟人度评估 5 维度
type HumanizeDimension string

const (
	HumanizeDimNaturalness     HumanizeDimension = "naturalness"
	HumanizeDimConciseness     HumanizeDimension = "conciseness"
	HumanizeDimEmpathy         HumanizeDimension = "empathy"
	HumanizeDimProfessionalism HumanizeDimension = "professionalism"
	HumanizeDimPersuasiveness  HumanizeDimension = "persuasiveness"
)

// AllHumanizeDimensions 全部 5 维度（遍历用）
var AllHumanizeDimensions = []HumanizeDimension{
	HumanizeDimNaturalness,
	HumanizeDimConciseness,
	HumanizeDimEmpathy,
	HumanizeDimProfessionalism,
	HumanizeDimPersuasiveness,
}

// HumanizeDimensionWeight 5 维度权重（合计 1.0）：
// Naturalness 0.25（强判别力）、Conciseness 0.15（次要维度）、Empathy 0.20（投诉场景必须共情）、
// Professionalism 0.20（人设一致性）、Persuasiveness 0.20（推进交易）。
var HumanizeDimensionWeight = map[HumanizeDimension]float64{
	HumanizeDimNaturalness:     0.25,
	HumanizeDimConciseness:     0.15,
	HumanizeDimEmpathy:         0.20,
	HumanizeDimProfessionalism: 0.20,
	HumanizeDimPersuasiveness:  0.20,
}

// HumanizeEvalInput 评估输入
type HumanizeEvalInput struct {
	CustomerID      string `json:"customer_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	CustomerMessage string `json:"customer_message"`
	AIReply         string `json:"ai_reply"`
	Persona         string `json:"persona,omitempty"`
	Industry        string `json:"industry,omitempty"`
	Platform        string `json:"platform,omitempty"`
	Intent          string `json:"intent,omitempty"`
	// ContextView 是统一会话上下文快照，按可见性与事实分级消费。
	// JSON 不序列化：它是服务内部契约，不对外暴露用户/线索详情。
	ContextView *agentcontext.AgentConversationContext `json:"-"`
}

// HumanizeDimensionScore 单维度得分
type HumanizeDimensionScore struct {
	Dimension HumanizeDimension `json:"dimension"`
	Score     float64           `json:"score"`
	Reason    string            `json:"reason,omitempty"`
}

// HumanizeEvalResult 评估结果
type HumanizeEvalResult struct {
	Scores             []HumanizeDimensionScore `json:"scores"`
	TotalScore         float64                  `json:"total_score"`
	DistanceToChampion float64                  `json:"distance_to_champion,omitempty"`
	Passed             bool                     `json:"passed"`
	AttemptCount       int                      `json:"attempt_count,omitempty"`
	FinalReply         string                   `json:"final_reply,omitempty"`
	AllReplies         []string                 `json:"all_replies,omitempty"`
	SampleStrategy     string                   `json:"sample_strategy,omitempty"`
	EvaluatorType      string                   `json:"evaluator_type,omitempty"`
	LLMModel           string                   `json:"llm_model,omitempty"`
	LLMLatencyMs       int                      `json:"llm_latency_ms,omitempty"`
	Input              *HumanizeEvalInput       `json:"input,omitempty"`
	CalculatedAt       time.Time                `json:"calculated_at"`
}

// ScoreHumanizeEvalByDimension 按维度查询得分（包级函数）
func ScoreHumanizeEvalByDimension(r *HumanizeEvalResult, dim HumanizeDimension) (float64, bool) {
	if r == nil {
		return 0, false
	}
	for _, s := range r.Scores {
		if s.Dimension == dim {
			return s.Score, true
		}
	}
	return 0, false
}

// ChampionBaselineDTO 销冠基线 DTO（供 service 层使用，与 model.ChampionBaseline 解耦）
type ChampionBaselineDTO struct {
	Persona         string  `json:"persona"`
	Industry        string  `json:"industry"`
	Intent          string  `json:"intent"`
	Naturalness     float64 `json:"naturalness"`
	Conciseness     float64 `json:"conciseness"`
	Empathy         float64 `json:"empathy"`
	Professionalism float64 `json:"professionalism"`
	Persuasiveness  float64 `json:"persuasiveness"`
}
