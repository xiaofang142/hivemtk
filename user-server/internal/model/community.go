package model

import (
	"gorm.io/gorm"
	"time"
)

// CommunityGroup 社群模型
type CommunityGroup struct {
	ID          string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	Name        string         `gorm:"type:varchar(255);not null" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	MemberCount int            `gorm:"default:0" json:"member_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (CommunityGroup) TableName() string {
	return "community_groups"
}

// CommunityMember 社群成员模型
type CommunityMember struct {
	ID        string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	GroupID   string    `gorm:"type:varchar(36);not null;index" json:"group_id"`
	Name      string    `gorm:"type:varchar(255);not null" json:"name"`
	Username  string    `gorm:"type:varchar(255);not null;index" json:"username"`
	Role      string    `gorm:"type:varchar(50);default:'member'" json:"role"`
	Status    string    `gorm:"type:varchar(50);default:'active'" json:"status"`
	JoinDate  time.Time `json:"join_date"`
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (CommunityMember) TableName() string {
	return "community_members"
}

// CommunityMessage 社群消息模型
type CommunityMessage struct {
	ID          string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	GroupID     string    `gorm:"type:varchar(36);not null;index" json:"group_id"`
	UserID      string    `gorm:"type:varchar(36);not null;index" json:"user_id"`
	UserName    string    `gorm:"type:varchar(255);not null" json:"user_name"`
	Content     string    `gorm:"type:text;not null" json:"content"`
	MessageType string    `gorm:"type:varchar(50);default:'text'" json:"message_type"`
	Timestamp   time.Time `json:"timestamp"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName 指定表名
func (CommunityMessage) TableName() string {
	return "community_messages"
}
