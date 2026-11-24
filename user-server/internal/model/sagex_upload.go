package model

import "time"

// SageXInstallation SageX 通知上报设备/安装实例绑定。
//
// SageX Android 端（sagex / qq-listener / wechat-listener）按 notification-upload-v1
// 协议上报通知事件。服务端必须仅凭 Bearer Token 鉴权结果确定租户/设备绑定，
// 不能信任 installation_id 做授权；installation_id 仅用于去重/展示。
type SageXInstallation struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	// Name 人类可读名称（如“张三的 QQ 监听机”）。
	Name string `gorm:"type:varchar(100)" json:"name"`

	// InstallationID SageX 客户端 installation_id（设备上同一 app 安装实例）。
	// 非授权依据；可为空（首次上报前未绑定）。
	InstallationID string `gorm:"type:varchar(100);index" json:"installation_id"`

	// Token SageX 上报 Bearer Token。按协议该 token 由服务端签发并配置到客户端。
	// 存储为明文以兼容现有 IntegrationAccount 密钥存储口径；日志/响应不得泄露。
	Token string `gorm:"type:varchar(200);not null;uniqueIndex" json:"-"`

	// AccountID 写入 HiveMTK 收件箱的账号标识（message_hub.account_id / inbox_conversations.account_id）。
	// 为空时回退为 installation_id。
	AccountID string `gorm:"type:varchar(100)" json:"account_id"`

	// Status 1=启用 0=禁用。
	Status int `gorm:"default:1" json:"status"`

	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SageXInstallation) TableName() string { return "sagex_installations" }

// SageXEventReceipt SageX 事件接收回执（幂等去重）。
//
// 唯一键：(tenant_id, installation_id, event_id)。同一事件重试返回 duplicate。
type SageXEventReceipt struct {
	TenantID uint `gorm:"not null;default:1;index;uniqueIndex:uk_sagex_receipt,priority:1" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	InstallationID string `gorm:"type:varchar(100);not null;uniqueIndex:uk_sagex_receipt,priority:2" json:"installation_id"`
	EventID        string `gorm:"type:varchar(100);not null;uniqueIndex:uk_sagex_receipt,priority:3" json:"event_id"`

	// BatchID 最近一次接收该事件的 batch_id（审计用）。
	BatchID string `gorm:"type:varchar(100)" json:"batch_id"`

	// Status accepted|duplicate|rejected（协议 ACK 状态）；存储最近一次确认状态。
	Status string `gorm:"type:varchar(20);not null" json:"status"`

	// MessageHubID 关联落库的 message_hub.id（若已入箱）。
	MessageHubID uint `gorm:"default:0;index" json:"message_hub_id"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SageXEventReceipt) TableName() string { return "sagex_event_receipts" }
