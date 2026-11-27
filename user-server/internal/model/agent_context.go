package model

import "time"

// ConversationMembership 保存可证明的群成员可见区间；display_name 仅作快照，不作身份。
type ConversationMembership struct {
	ID               uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         uint       `gorm:"not null;index:idx_conv_membership_active,priority:1" json:"tenant_id"`
	Platform         string     `gorm:"type:varchar(32);not null;index:idx_conv_membership_active,priority:2" json:"platform"`
	ChannelAccountID uint       `gorm:"not null;index:idx_conv_membership_active,priority:3" json:"channel_account_id"`
	GroupID          string     `gorm:"type:varchar(191);not null;index:idx_conv_membership_active,priority:4" json:"group_id"`
	MemberID         string     `gorm:"type:varchar(191);not null;index:idx_conv_membership_active,priority:5" json:"member_id"`
	DisplayName      string     `gorm:"type:varchar(255)" json:"display_name"`
	JoinedAt         *time.Time `json:"joined_at,omitempty"`
	LeftAt           *time.Time `gorm:"index:idx_conv_membership_active,priority:6" json:"left_at,omitempty"`
	EvidenceSource   string     `gorm:"type:varchar(64);not null" json:"evidence_source"`
	EvidenceRef      string     `gorm:"type:varchar(191);not null" json:"evidence_ref"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ConversationMembership) TableName() string { return "conversation_memberships" }

// ConversationMessageDisposition 记录每条入站消息的用户可见处理状态，不把执行场景水位当回复水位。
type ConversationMessageDisposition struct {
	ID                   uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID             uint       `gorm:"not null;uniqueIndex:uni_agent_conv_disposition,priority:1" json:"tenant_id"`
	Platform             string     `gorm:"type:varchar(32);not null;uniqueIndex:uni_agent_conv_disposition,priority:2" json:"platform"`
	ChannelAccountID     uint       `gorm:"not null;uniqueIndex:uni_agent_conv_disposition,priority:3" json:"channel_account_id"`
	ConversationID       string     `gorm:"type:varchar(191);not null;uniqueIndex:uni_agent_conv_disposition,priority:4;index" json:"conversation_id"`
	GroupID              string     `gorm:"type:varchar(191)" json:"group_id,omitempty"`
	MessageID            string     `gorm:"type:varchar(191);not null;uniqueIndex:uni_agent_conv_disposition,priority:5" json:"message_id"`
	MessageCursor        string     `gorm:"type:varchar(191);not null" json:"message_cursor"`
	BusLLMSessionIDBlind string     `gorm:"type:char(64);not null;index" json:"bus_llm_session_id_blind"`
	Status               string     `gorm:"type:varchar(32);not null;default:'pending';index" json:"status"`
	ReplyMessageID       string     `gorm:"type:varchar(191)" json:"reply_message_id,omitempty"`
	DeliveredAt          *time.Time `json:"delivered_at,omitempty"`
	Late                 bool       `gorm:"not null;default:false" json:"late"`
	Reason               string     `gorm:"type:varchar(255)" json:"reason,omitempty"`
	Version              int64      `gorm:"not null;default:1" json:"version"`
	CreatedAt            time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ConversationMessageDisposition) TableName() string {
	return "conversation_message_dispositions"
}

// MessageTombstone 标记编辑、删除、撤回和脱敏，供摘要/native generation 失效判断。
type MessageTombstone struct {
	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         uint      `gorm:"not null;uniqueIndex:uni_agent_msg_tombstone,priority:1" json:"tenant_id"`
	Platform         string    `gorm:"type:varchar(32);not null;uniqueIndex:uni_agent_msg_tombstone,priority:2" json:"platform"`
	ChannelAccountID uint      `gorm:"not null;uniqueIndex:uni_agent_msg_tombstone,priority:3" json:"channel_account_id"`
	ConversationID   string    `gorm:"type:varchar(191);not null;index" json:"conversation_id"`
	MessageID        string    `gorm:"type:varchar(191);not null;uniqueIndex:uni_agent_msg_tombstone,priority:4" json:"message_id"`
	TombstoneType    string    `gorm:"type:varchar(32);not null;uniqueIndex:uni_agent_msg_tombstone,priority:5" json:"tombstone_type"`
	EffectiveAt      time.Time `gorm:"not null" json:"effective_at"`
	SourceCursor     string    `gorm:"type:varchar(191);not null" json:"source_cursor"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (MessageTombstone) TableName() string { return "message_tombstones" }

const (
	ConversationDispositionPending    = "pending"
	ConversationDispositionProcessing = "processing"
	ConversationDispositionReplied    = "replied"
	ConversationDispositionSkipped    = "skipped"
	ConversationDispositionMerged     = "merged"

	MessageTombstoneEdited   = "edited"
	MessageTombstoneDeleted  = "deleted"
	MessageTombstoneRecalled = "recalled"
	MessageTombstoneRedacted = "redacted"
)
