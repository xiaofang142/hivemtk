package model

import "time"

// IntentLog 精细意图识别日志
//
// OPT-DB-10：intent_logs 物理表已并入统一意图表 intent_records（source='fine_grained'），
// 本结构为 intent_records 的列映射视图，不再是独立物理表。
// 列结构由迁移 v3.22.3（IntentMergeMigration）保证，禁止加入 automigrate 清单，
// 否则 AutoMigrate 会重建 intent_logs 造成双表回潮。
type IntentLog struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID  string    `gorm:"column:customer_id;type:varchar(64)" json:"customer_id"`
	SessionID   string    `gorm:"column:session_id;type:varchar(120)" json:"session_id"`
	Message     string    `gorm:"column:raw_text;type:text;not null" json:"message"`
	IntentMajor string    `gorm:"column:intent_type;type:varchar(50);not null" json:"intent_major"`
	IntentMinor string    `gorm:"column:intent_subtype;type:varchar(50)" json:"intent_minor"`
	Confidence  float64   `gorm:"column:confidence;type:decimal(5,4);not null" json:"confidence"`
	Method      string    `gorm:"column:method;type:varchar(16)" json:"method"`
	LatencyMs   int       `gorm:"column:latency_ms;default:0" json:"latency_ms"`
	Reasoning   string    `gorm:"column:reasoning;type:text" json:"reasoning,omitempty"`
	TraceID     string    `gorm:"column:trace_id;type:varchar(64)" json:"trace_id,omitempty"`
	Timestamp   time.Time `gorm:"column:timestamp" json:"timestamp"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	Source      string    `gorm:"column:source;type:varchar(32);default:fine_grained" json:"-"`
}

// TableName GORM 表名（统一意图表 intent_records）
func (IntentLog) TableName() string { return "intent_records" }

// IntentLogSource 精细意图日志在 intent_records 中的 source 取值
const IntentLogSource = "fine_grained"
