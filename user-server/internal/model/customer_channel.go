package model

import (
	"time"

	"gorm.io/gorm"
)

// CustomerChannel 客户-渠道身份绑定副表
//
// 业务场景：同一个 OneID（手机号/邮箱）客户可能在多个渠道都有账号。
// 主表 Customer 记录最常用的身份字段（首渠道），其余渠道身份存此表。
// 自动同步：所有渠道 webhook 入站时自动 Upsert（按 OneID + channel）。
type CustomerChannel struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// OneID 宽度必须与 customers.unified_id 对齐（varchar(128)）。
	// 手机号型 OneID = "phone:" + sha256 hex(64) = 70 字符，
	// 若此处收窄为 varchar(64)，合并客户时 ReassignOneID 写回 one_id 会报
	// `value too long for type character varying(64)`，整个合并事务回滚。
	OneID         string         `gorm:"type:varchar(128);index;uniqueIndex:idx_oneid_channel" json:"one_id"`
	Channel       string         `gorm:"type:varchar(32);uniqueIndex:idx_oneid_channel" json:"channel"`
	ChannelUserID string         `gorm:"type:varchar(128);index" json:"channel_user_id"`
	ChannelName   string         `gorm:"type:varchar(128)" json:"channel_name"`
	AccountID     string         `gorm:"type:varchar(64);index" json:"account_id"`
	GroupID       string         `gorm:"type:varchar(64)" json:"group_id,omitempty"`
	IsGroup       bool           `gorm:"default:false" json:"is_group"`
	IsPrimary     bool           `gorm:"default:false" json:"is_primary"`
	PreferredRank int            `gorm:"default:0" json:"preferred_rank"`
	LastSeenAt    time.Time      `gorm:"autoUpdateTime" json:"last_seen_at"`
	FirstSeenAt   time.Time      `gorm:"autoCreateTime" json:"first_seen_at"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (CustomerChannel) TableName() string {
	return "customer_channels"
}
