package model

import "time"

// ChannelAccount 渠道账号主数据（单渠道多账号）。
//
// 对应顶层迁移 migrations/089_channel_accounts.sql。两类智能体绑定收口在本表
// （auto_reply_agent_id / lead_mining_agent_id），不再走「智能体管理里绑账号」。
// identify_code 是**每个渠道账号一枚**的唯一识别码（全局唯一，按码反查租户/账号），
// 是本语义下唯一的权威列，故用 global_uniq_ 前缀防被租户索引规范化。
// channel_identify_codes 只保留存量渠道级码作历史兼容（老上报端仍可凭老码解析），
// 管理面不再为渠道生成新码。
// prev_code / prev_valid_until 承载重置后的短暂在途窗口：窗口内的旧码仍被
// 凭证闸门与解析器接受，窗口过后立即失效（非长期兼容期）。
type ChannelAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelID        uint   `gorm:"not null;index" json:"channel_id"`
	Name             string `gorm:"type:varchar(100);not null" json:"name"`
	Remark           string `gorm:"type:varchar(500)" json:"remark"`
	SelfIdentifyName string `gorm:"type:varchar(100)" json:"self_identify_name"`
	Type             string `gorm:"type:varchar(32);default:'dm';index" json:"type"`

	AutoReplyAgentID  *uint `gorm:"index" json:"auto_reply_agent_id,omitempty"`
	LeadMiningAgentID *uint `gorm:"index" json:"lead_mining_agent_id,omitempty"`

	// OwnerUserIDs 负责人/多通知人（JSON 数组字符串，元素为用户 ID 字符串）。
	OwnerUserIDs JSONStrings `gorm:"type:jsonb" json:"owner_user_ids"`
	// DataScope 可见账号/行业范围 JSON 对象（数据授权维度，原 RBAC 表不改）。
	DataScope string `gorm:"type:jsonb" json:"data_scope"`

	IdentifyCode string `gorm:"type:varchar(128);uniqueIndex:global_uniq_channel_account_identify_code" json:"identify_code"`
	// 重置在途窗口：旧码在 PrevValidUntil 之前仍被接受，之后立即失效。
	PrevCode       string     `gorm:"type:varchar(128)" json:"prev_code,omitempty"`
	PrevValidUntil *time.Time `json:"prev_valid_until,omitempty"`

	Status int `gorm:"default:1;index" json:"status"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 渠道账号主数据表。
func (ChannelAccount) TableName() string { return "channel_accounts" }
