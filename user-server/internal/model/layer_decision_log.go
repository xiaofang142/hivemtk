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
// 索引 (开发库 pg_indexes 实测，8 个含主键):
//   - layer_decision_logs_pkey
//   - idx_layer_decision_logs_trace_id / _session_id / _customer_id
//   - idx_layer_decision_logs_intent / _layer / _created_at / _deleted_at
//
// 字段说明:
//   - TraceID:  本表自造的 lr-<unixnano>（traceID() 兜底分支），与 llm_routing_logs 的
//     UUID v4 trace_id 不同值，两表无法按它 join
//   - SessionID / CustomerID: 业务维度
//   - Layer:    命中的层 (只有 layer1 / layer2，见 dto.Layer1 / dto.Layer2)
//   - Reason:   决策原因，实际写入集为 layer1_disabled / faq_hit / sop_hit /
//     low_confidence_skip / fallback；其余 dto.Reason* 常量当前无赋值点
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
