package model

import (
	"gorm.io/gorm"
	"time"
)

// SOPTemplate SOP 模板 (Layer1 拼接回复)
//
// 五层架构归属: L5 数据层 (横向)
// 设计依据: AI 智能体性能优化
//   - Layer1 路由: 当 FAQ 未命中 + SOP 模板高置信 -> 模板拼接回复
//   - 避免 LLM 调用,响应 <50ms
//
// 表: sop_templates
// 索引:
//   - idx_sop_intent_stage (intent+stage 复合索引, MatchByIntentStage 快速取)
//   - idx_sop_enabled       (enabled 过滤)
//
// 字段说明:
//   - Intent:    关联意图 (与 IntentLog.IntentMajor 对齐)
//   - Stage:     SOP 阶段 (initial/middle/late/objection/closing)
//   - Template:  Go text/template 格式 (支持 {{.VarName}} 变量)
//   - Vars:      模板变量元数据 (JSON: 变量名->描述+示例)
//   - Priority:  优先级 (数字越大越优先, 同 intent+stage 多模板时用)
//   - Confidence: 基准置信度 (0-1)
type SOPTemplate struct {
	ID         uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Name       string         `gorm:"type:varchar(100);not null" json:"name"`
	Intent     string         `gorm:"type:varchar(64);not null;index" json:"intent"`
	Stage      string         `gorm:"type:varchar(32);not null;index" json:"stage"`
	Template   string         `gorm:"type:text;not null" json:"template"`
	Vars       string         `gorm:"type:text" json:"vars"`
	Priority   int            `gorm:"type:int;default:0" json:"priority"`
	Confidence float64        `gorm:"type:decimal(5,4);default:0.8" json:"confidence"`
	AgentID    *uint          `gorm:"index" json:"agent_id,omitempty"`
	Enabled    *bool          `gorm:"type:boolean;default:true;not null" json:"enabled"`
	HitCount   int64          `gorm:"type:bigint;default:0" json:"hit_count"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName GORM 表名
func (SOPTemplate) TableName() string { return "sop_templates" }
