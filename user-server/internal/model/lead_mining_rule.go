package model

import "time"

// LeadMiningRule 三粒度线索发掘规则（global/channel/account）。
//
// 表由迁移 091（每租户回填一条 global 规则）建立，094 再次以幂等方式确保列齐全。
// Scope 优先级：account > channel > global；scope_id 在 global 时为 0，
// channel/account 时分别存渠道账号表的主键 id（不是渠道识别码字符串）。
// 租户隔离：tenant_id 必填，仓储层缺租户一律 fail-closed。
type LeadMiningRule struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID       int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	Scope    string      `gorm:"column:scope;type:varchar(16);not null;default:'global'" json:"scope"`
	ScopeID  uint        `gorm:"column:scope_id;not null;default:0" json:"scope_id"`
	Keywords JSONStrings `gorm:"column:keywords;type:jsonb;default:'[]'" json:"keywords"`

	// SemanticKeywords 语义关键词：由 T036 的语义检测器做同义/相关词扩展匹配，
	// 与 Keywords 的字面匹配互为补充，二者并行执行、各自留证。
	SemanticKeywords  JSONStrings `gorm:"column:semantic_keywords;type:jsonb;default:'[]'" json:"semantic_keywords"`
	SystemDescription string      `gorm:"column:system_description;type:text;default:''" json:"system_description"`
	Enabled           bool        `gorm:"column:enabled;not null;default:true" json:"enabled"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 线索发掘规则表。
func (LeadMiningRule) TableName() string { return "lead_mining_rules" }
