package model

import "time"

// ChannelAccountAuthorization 渠道账号数据授权（原 RBAC 表不改动，本表为叠加维度）。
//
// 对应顶层迁移 migrations/092_channel_account_authorizations.sql。
// 所有客户/消息列表接口须按本表的可见账号范围 + 可见行业范围强制过滤（FR-002/010/016）。
type ChannelAccountAuthorization struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	UserID               uint   `gorm:"not null;index" json:"user_id"`
	ChannelAccountID     uint   `gorm:"not null;index" json:"channel_account_id"`
	VisibleIndustryScope string `gorm:"type:jsonb" json:"visible_industry_scope"`
	Role                 string `gorm:"type:varchar(32);default:'viewer'" json:"role"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 渠道账号数据授权表。
func (ChannelAccountAuthorization) TableName() string { return "channel_account_authorizations" }
