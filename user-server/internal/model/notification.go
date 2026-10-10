package model

import "time"

// Notification 系统通知模型（商户端内置通知中心）
//
// 设计目标：
//   - 私域部署下，平台端下发的系统通知、版本公告、安全告警、租户自定义消息
//     全部落地到本表，供 user-web 通知中心 / 顶部铃铛 badge 使用
//   - 不依赖外部 IM，纯站内通知
//   - 与 platform message 双写：平台端推一条到 platform.messages，本表镜像一条
//     到 user_db（保证 user-web 离线仍可读）
//
// 字段说明：
//   - UserID  = 0 表示全体用户（admin/manager 可见）
//   - Type    枚举: info/warning/error/success/announcement
//   - Link    可选外链（公告/详情跳转）
//   - Metadata 预留 JSON 字段，存扩展信息
type Notification struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	UserID    uint       `json:"user_id" gorm:"index;not null;default:0"`
	Type      string     `json:"type" gorm:"size:32;index;not null;default:'info'"`
	Title     string     `json:"title" gorm:"size:255;not null"`
	Content   string     `json:"content" gorm:"type:text"`
	Link      string     `json:"link" gorm:"size:512"`
	IsRead    bool       `json:"is_read" gorm:"index;not null;default:false"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	Metadata  string     `json:"metadata" gorm:"type:text"`
	CreatedAt time.Time  `json:"created_at" gorm:"index"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TableName 表名
func (n *Notification) TableName() string {
	return "notifications"
}

// Notification 通知类型枚举
const (
	NotificationTypeInfo         = "info"
	NotificationTypeWarning      = "warning"
	NotificationTypeSuccess      = "success"
	NotificationTypeAnnouncement = "announcement"
)
