package model

import "time"

// FeishuAccount 飞书账号（机器人应用）
type FeishuAccount struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID       uint       `gorm:"default:0;index" json:"owner_user_id"`
	AccountName       string     `gorm:"type:varchar(100);not null" json:"account_name"`
	AppID             string     `gorm:"type:varchar(100);not null" json:"app_id"`
	AppSecret         string     `gorm:"type:varchar(200);not null" json:"app_secret"`
	VerificationToken string     `gorm:"type:varchar(200)" json:"verification_token"`
	EncryptKey        string     `gorm:"type:varchar(200)" json:"encrypt_key"`
	WebhookEnabled    bool       `gorm:"default:false" json:"webhook_enabled"`
	AIAgentEnabled    bool       `gorm:"default:false" json:"ai_agent_enabled"`
	AccessToken       string     `gorm:"type:text" json:"access_token"`
	TokenExpires      *time.Time `json:"token_expires"`
	LastSyncAt        *time.Time `json:"last_sync_at"`
	LastErrorAt       *time.Time `json:"last_error_at"`
	LastErrorMsg      string     `gorm:"type:text" json:"last_error_msg"`
	Status            int        `gorm:"default:1" json:"status"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (FeishuAccount) TableName() string { return "feishu_accounts" }

// FeishuCustomer 飞书客户
type FeishuCustomer struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID  uint      `gorm:"index;not null" json:"account_id"`
	OpenID     string    `gorm:"type:varchar(200);index;not null" json:"open_id"`
	UnionID    string    `gorm:"type:varchar(200);index" json:"union_id"`
	UserID     string    `gorm:"type:varchar(200);index" json:"user_id"`
	Name       string    `gorm:"type:varchar(100)" json:"name"`
	Avatar     string    `gorm:"type:varchar(500)" json:"avatar"`
	Email      string    `gorm:"type:varchar(200)" json:"email"`
	Mobile     string    `gorm:"type:varchar(50)" json:"mobile"`
	LastActive time.Time `json:"last_active"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (FeishuCustomer) TableName() string { return "feishu_customers" }

// FeishuMessage 飞书消息
type FeishuMessage struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID uint      `gorm:"index;not null" json:"account_id"`
	MsgID     string    `gorm:"type:varchar(100);unique" json:"msg_id"`
	ChatID    string    `gorm:"type:varchar(100);index" json:"chat_id"`
	ChatType  string    `gorm:"type:varchar(20)" json:"chat_type"`
	SenderID  string    `gorm:"type:varchar(200);index" json:"sender_id"`
	MsgType   string    `gorm:"type:varchar(20)" json:"msg_type"`
	Content   string    `gorm:"type:text" json:"content"`
	Direction string    `gorm:"type:varchar(10);index" json:"direction"`
	IsAIReply bool      `gorm:"default:false" json:"is_ai_reply"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (FeishuMessage) TableName() string { return "feishu_messages" }

// TelegramAccount Telegram 机器人账号
type TelegramAccount struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID        uint       `gorm:"default:0;index" json:"owner_user_id"`
	AccountName        string     `gorm:"type:varchar(100);not null" json:"account_name"`
	BotToken           string     `gorm:"type:varchar(200);not null" json:"bot_token"`
	BotUsername        string     `gorm:"type:varchar(64)" json:"bot_username"`
	WebhookURL         string     `gorm:"type:varchar(500)" json:"webhook_url"`
	WebhookSecret      string     `gorm:"type:varchar(200)" json:"webhook_secret"`
	WebhookEnabled     bool       `gorm:"default:false" json:"webhook_enabled"`
	AIAgentEnabled     bool       `gorm:"default:false" json:"ai_agent_enabled"`
	LastSyncAt         *time.Time `json:"last_sync_at"`
	LastErrorAt        *time.Time `json:"last_error_at"`
	LastErrorMsg       string     `gorm:"type:text" json:"last_error_msg"`
	Status             int        `gorm:"default:1" json:"status"`
	PollingOwner       string     `gorm:"type:varchar(100);default:''" json:"polling_owner"`
	PollingHeartbeatAt *time.Time `json:"polling_heartbeat_at"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (TelegramAccount) TableName() string { return "telegram_accounts" }

// WhatsAppCloudAccount WhatsApp Cloud API 商业账号
type WhatsAppCloudAccount struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID        uint       `gorm:"default:0;index" json:"owner_user_id"`
	AccountName        string     `gorm:"type:varchar(100);not null" json:"account_name"`
	PhoneNumberID      string     `gorm:"type:varchar(100);not null" json:"phone_number_id"`
	WhatsAppBusinessID string     `gorm:"type:varchar(100);not null" json:"whatsapp_business_id"`
	AccessToken        string     `gorm:"type:varchar(500);not null" json:"access_token"`
	VerifyToken        string     `gorm:"type:varchar(200)" json:"verify_token"`
	AppSecret          string     `gorm:"type:varchar(200)" json:"app_secret"`
	WebhookEnabled     bool       `gorm:"default:false" json:"webhook_enabled"`
	AIAgentEnabled     bool       `gorm:"default:false" json:"ai_agent_enabled"`
	LastSyncAt         *time.Time `json:"last_sync_at"`
	LastErrorAt        *time.Time `json:"last_error_at"`
	LastErrorMsg       string     `gorm:"type:text" json:"last_error_msg"`
	Status             int        `gorm:"default:1" json:"status"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WhatsAppCloudAccount) TableName() string { return "whatsapp_cloud_accounts" }

// QQAccount QQ 机器人开放平台账号（q.qq.com）
//
// 凭证模型：AppID + AppSecret 换 access_token（服务端缓存 7200s），
// WebhookSecret 为官方 BotSecret（用于 Ed25519 webhook 验签派生）。
type QQAccount struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID    uint       `gorm:"default:0;index" json:"owner_user_id"`
	AccountName    string     `gorm:"type:varchar(100);not null" json:"account_name"`
	AppID          string     `gorm:"type:varchar(100);not null" json:"app_id"`
	AppSecret      string     `gorm:"type:varchar(200);not null" json:"app_secret"`
	WebhookSecret  string     `gorm:"type:varchar(200)" json:"webhook_secret"`
	WebhookURL     string     `gorm:"type:varchar(500)" json:"webhook_url"`
	WebhookEnabled bool       `gorm:"default:false" json:"webhook_enabled"`
	AIAgentEnabled bool       `gorm:"default:false" json:"ai_agent_enabled"`
	AccessToken    string     `gorm:"type:text" json:"access_token"`
	TokenExpires   *time.Time `json:"token_expires"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastErrorAt    *time.Time `json:"last_error_at"`
	LastErrorMsg   string     `gorm:"type:text" json:"last_error_msg"`
	Status         int        `gorm:"default:1" json:"status"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (QQAccount) TableName() string { return "qq_accounts" }
