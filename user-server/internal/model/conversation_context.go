package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ConversationMessage 共享对话历史中的一条消息（多轮 + 历史）。
type ConversationMessage struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"` // user / assistant / self / other / system
	SenderID  string `json:"sender_id,omitempty"`
	Content   string `json:"content"`
	At        int64  `json:"at,omitempty"`
}

// ConversationMessages jsonb 数组载荷。
type ConversationMessages []ConversationMessage

func (m ConversationMessages) Value() (driver.Value, error) {
	if m == nil {
		return "[]", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (m *ConversationMessages) Scan(v any) error {
	if v == nil {
		*m = ConversationMessages{}
		return nil
	}
	var data []byte
	switch t := v.(type) {
	case string:
		data = []byte(t)
	case []byte:
		data = t
	default:
		*m = ConversationMessages{}
		return nil
	}
	if len(data) == 0 {
		*m = ConversationMessages{}
		return nil
	}
	return json.Unmarshal(data, m)
}

var _ driver.Valuer = (ConversationMessages)(nil)

// MemberRoles 群聊成员角色映射（sender_id → demand/supply/resource/broker/unknown）。
type MemberRoles map[string]string

func (r MemberRoles) Value() (driver.Value, error) {
	if r == nil {
		return "{}", nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (r *MemberRoles) Scan(v any) error {
	if v == nil {
		*r = MemberRoles{}
		return nil
	}
	var data []byte
	switch t := v.(type) {
	case string:
		data = []byte(t)
	case []byte:
		data = t
	default:
		*r = MemberRoles{}
		return nil
	}
	if len(data) == 0 {
		*r = MemberRoles{}
		return nil
	}
	return json.Unmarshal(data, r)
}

var _ driver.Valuer = (MemberRoles)(nil)

// ConversationContext 两智能体共享的对话上下文（FR-020/021）。
// 线索发现与自动回复各自写入标签/备注/摘要做交叉引用，不直调对方模块。
type ConversationContext struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                     int64                `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelAccountID       uint                 `gorm:"column:channel_account_id;type:bigint;not null;default:0;index" json:"channel_account_id"`
	ExternalConversationID string               `gorm:"column:external_conversation_id;type:varchar(128);not null;index" json:"external_conversation_id"`
	MemberRoles            MemberRoles          `gorm:"column:member_roles;type:jsonb;default:'{}'" json:"member_roles"`
	Messages               ConversationMessages `gorm:"column:messages;type:jsonb;default:'[]'" json:"messages"`
	Tags                   JSONStrings          `gorm:"column:tags;type:jsonb;default:'[]'" json:"tags"`
	Summary                string               `gorm:"column:summary;type:text;default:''" json:"summary"`
	CreatedAt              time.Time            `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt              time.Time            `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 共享对话上下文表。
func (ConversationContext) TableName() string { return "conversation_contexts" }
