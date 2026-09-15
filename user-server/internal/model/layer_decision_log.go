package model

import (
	"gorm.io/gorm"
	"time"
)

// LayerDecisionLog 决策日志
//
// 用于记录 AI 智能体的决策链路，便于问题排查和性能分析。
//
// 表: layer_decision_logs
// 索引:
//   - idx_layer_trace_id   (trace 维度, 端到端串联)
//   - idx_layer_created_at (时间维度)
//   - idx_layer_layer      (layer 维度, 快速聚合)
//
// 字段说明:
//   - TraceID:  端到端 trace id (与 llm_routing_logs.trace_id 对齐)
//   - SessionID / CustomerID: 业务维度
//   - Layer:    命中的层 (layer1 / layer2 / fallback_template / fallback_cache)
//   - Reason:   决策原因 (faq_match / sop_template / llm_response / 7b_fail_3b / cache_hit / template_default)
//   - Intent:   关联意图
//   - ConfIn:   输入置信度 (LayerRouter.Route 决策前)
//   - ConfOut:  输出置信度 (决策后, 用于下轮 cache)
//   - WallMs:   本次决策 wall time (ms)
//   - LLMSkipped: 是否跳过 LLM (Layer1 命中 -> true)
type LayerDecisionLog struct {
	ID         uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID    string         `gorm:"type:varchar(64);index" json:"trace_id"`
	SessionID  string         `gorm:"type:varchar(120);index" json:"session_id"`
	CustomerID string         `gorm:"type:varchar(64);index" json:"customer_id"`
	Layer      string         `gorm:"type:varchar(32);not null;index" json:"layer"`
	Reason     string         `gorm:"type:varchar(64);not null" json:"reason"`
	Intent     string         `gorm:"type:varchar(64);index" json:"intent"`
	ConfIn     float64        `gorm:"type:decimal(5,4);default:0" json:"conf_in"`
	ConfOut    float64        `gorm:"type:decimal(5,4);default:0" json:"conf_out"`
	WallMs     int            `gorm:"type:int;default:0" json:"wall_ms"`
	LLMSkipped *bool          `gorm:"type:boolean;default:false;not null" json:"llm_skipped"`
	Extra      string         `gorm:"type:text" json:"extra,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName GORM 表名
func (LayerDecisionLog) TableName() string { return "layer_decision_logs" }
