package model

import "time"

// SupportMetrics 客服支持度量看板聚合结果。
// 对应运营后台需求「指标看板」：自助解决率 / 转人工率 / 会话轮次 / 知识库召回准确率。
type SupportMetrics struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`

	// 会话侧（customer_sessions）
	TotalSessions       int64 `json:"total_sessions"`
	HandoffSessions     int64 `json:"handoff_sessions"`
	SelfServiceSessions int64 `json:"self_service_sessions"`

	// 比率与均值，除零一律归 0，避免前端 NaN
	SelfServiceRate float64 `json:"self_service_rate"`
	HandoffRate     float64 `json:"handoff_rate"`
	AvgTurns        float64 `json:"avg_turns"`

	// 知识库召回（rag_query_logs / rag_metrics_daily）
	RagRecallAccuracy float64 `json:"rag_recall_accuracy"`
	RagPrecision      float64 `json:"rag_precision"`
	RagTotalQueries   int64   `json:"rag_total_queries"`
}
