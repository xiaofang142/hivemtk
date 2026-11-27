package model

import "time"

// SessionMessage 会话消息
type SessionMessage struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	CorrelationID string       `gorm:"type:varchar(100);index" json:"correlation_id,omitempty"`
	SessionID     string       `gorm:"type:varchar(120);index;not null" json:"session_id"`
	Content       string       `gorm:"type:text" json:"content"`
	ContentType   MessageType  `gorm:"type:varchar(20)" json:"content_type"`
	CardData      string       `gorm:"type:text" json:"card_data"`
	CardType      RichCardType `gorm:"type:varchar(20)" json:"card_type"`
	MediaURL      string       `gorm:"type:varchar(500)" json:"media_url"`
	SenderType    string       `gorm:"type:varchar(20);not null" json:"sender_type"`
	SenderID      string       `gorm:"type:varchar(50)" json:"sender_id"`
	SenderName    string       `gorm:"type:varchar(100)" json:"sender_name"`
	SenderAvatar  string       `gorm:"type:varchar(500)" json:"sender_avatar"`
	AIConfidence  float64      `gorm:"type:decimal(5,2)" json:"ai_confidence"`
	AISource      string       `gorm:"type:varchar(20)" json:"ai_source"`
	// Citations 答案引用快照（T2 引用溯源，JSON 数组，结构见 dto.Citation）。
	// 与 AIConfidence 同期写入，构成一次回答的可追溯证据链；空串 = 无引用。
	Citations   string     `gorm:"type:jsonb;not null;default:'[]'" json:"citations,omitempty"`
	IsInternal  bool       `gorm:"default:false;index" json:"is_internal"`
	IsRead      bool       `gorm:"default:false" json:"is_read"`
	ReadAt      *time.Time `json:"read_at"`
	DeliveredAt *time.Time `gorm:"index" json:"delivered_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 指定表名
func (SessionMessage) TableName() string {
	return "session_messages"
}
