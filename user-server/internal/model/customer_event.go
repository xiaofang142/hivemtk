package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EventType 事件类型常量
const (
	EventTypePageView  EventType = "page_view"
	EventTypeClick     EventType = "click"
	EventTypePurchase  EventType = "purchase"
	EventTypeAddToCart EventType = "add_to_cart"
	EventTypeSignup    EventType = "signup"
	EventTypeLogin     EventType = "login"
)

// EventType 事件类型
type EventType string

// EventSource 事件来源
type EventSource string

// EventSource 事件来源常量
const (
	EventSourceWechat      EventSource = "wechat"
	EventSourceDouyin      EventSource = "douyin"
	EventSourceXiaohongshu EventSource = "xiaohongshu"
	EventSourceWebsite     EventSource = "website"
	EventSourceApp         EventSource = "app"
)

// CustomerEvent 客户行为事件模型
type CustomerEvent struct {
	ID          string      `gorm:"type:varchar(36);primaryKey" json:"id"`
	CustomerID  string      `gorm:"type:varchar(64);index;not null" json:"customer_id"`
	EventType   EventType   `gorm:"type:varchar(32);index;not null" json:"event_type"`
	EventSource EventSource `gorm:"type:varchar(32);index" json:"event_source"`
	EventData   string      `gorm:"type:text" json:"event_data"`
	OccurredAt  time.Time   `gorm:"index;not null" json:"occurred_at"`
	CreatedAt   time.Time   `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 返回表名
func (CustomerEvent) TableName() string {
	return "customer_events"
}

// BeforeCreate 创建前钩子 - 自动生成 ID
func (e *CustomerEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}

	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}

	return nil
}
