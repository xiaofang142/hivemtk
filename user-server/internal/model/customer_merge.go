package model

import "time"

// CustomerMerge 客户合并审计（FR-003/005 去重合并可追溯）。
//
// 勘察结论：本仓 OneID 以 customers.unified_id 落地（GenerateCustomerUnifiedID），
// 合并规则与执行在 service/oneid_merge_rule.go + service/customer_identity.go，
// customer_tags / followup_records / operation_logs 也已有模型。故 T020 真正缺失
// 的只有「合并审计留痕表」——本模型即该缺口；不另建重复的 one_ids / tags 表。
type CustomerMerge struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	MergeID     string `gorm:"type:varchar(32);not null;index" json:"merge_id"`
	OneID       string `gorm:"type:varchar(128);index" json:"one_id,omitempty"`
	PrimaryID   string `gorm:"type:varchar(36);not null;index" json:"primary_id"`
	SecondaryID string `gorm:"type:varchar(36);not null;index" json:"secondary_id"`

	// Source/RuleName/Action 记录「按哪条规则、以何种动作命中」。
	Source   string `gorm:"type:varchar(64);default:''" json:"source,omitempty"`
	RuleName string `gorm:"type:varchar(100);default:''" json:"rule_name,omitempty"`
	Action   string `gorm:"type:varchar(16);default:'auto'" json:"action,omitempty"`

	// OriginalValues 保留被合并方的原始关键字段快照（来源、操作人、时间、原始值）。
	OriginalValues string `gorm:"type:jsonb;default:'{}'" json:"original_values"`
	Reason         string `gorm:"type:text;default:''" json:"reason,omitempty"`

	OperatorID   uint   `gorm:"index" json:"operator_id,omitempty"`
	OperatorName string `gorm:"type:varchar(64);default:''" json:"operator_name,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (CustomerMerge) TableName() string { return "customer_merges" }
