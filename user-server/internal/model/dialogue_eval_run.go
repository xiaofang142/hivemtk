package model

import "time"

// DialogueEvalRun 对话级 gold set 评测的一次运行记录（T-P8-04，LTC-15）。
//
// RagEvalRun（表 rag_eval_runs）是 RAG 检索指标形状（recall/MRR/NDCG），
// 装不下"一轮对话多回合 + 关键词覆盖 + 期望动作"这类对话级分数，
// 所以对话级基线另起一张表。这里的每一行 = 一次离线评测的汇总；
// 逐样本明细压在 Details（JSON），查"哪一轮掉分"读它，不另建明细表
// （样本数是个位数量级，明细拆表是过度设计）。
type DialogueEvalRun struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	// Name 本次运行名（惯例：gold-baseline-<日期> / gold-degraded-<日期>）。
	Name string `gorm:"type:varchar(128);index" json:"name"`

	// SampleCount 参评样本数（fixture 里的对话条数）。
	SampleCount int `json:"sample_count"`

	// CompositeAvg 综合分均值（turn 级 composite 再按样本平均）。
	CompositeAvg float64 `gorm:"type:decimal(6,4);default:0" json:"composite_avg"`
	// KeyCoverageAvg 关键词覆盖率均值。
	KeyCoverageAvg float64 `gorm:"type:decimal(6,4);default:0" json:"key_coverage_avg"`
	// ActionMatchRate 期望动作一致率（只统计动作非空的回合）。
	ActionMatchRate float64 `gorm:"type:decimal(6,4);default:0" json:"action_match_rate"`
	// ChrfAvg ChrF 均值。
	ChrfAvg float64 `gorm:"type:decimal(6,4);default:0" json:"chrf_avg"`

	// Details 逐样本明细 JSON（[]eval.DialogueSampleScore），反向掉分定位到样本靠它。
	Details string `gorm:"type:text" json:"details,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 对话级评测运行表。
func (DialogueEvalRun) TableName() string { return "dialogue_eval_runs" }
