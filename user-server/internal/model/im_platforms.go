package model

import "time"

// 025-im-platforms 新平台账号表（specs/025-im-platforms）。
//
// 每个平台独立账号表，不塞进 integration_accounts：验签密钥、渠道侧 ID 与
// 拉取游标都是平台专属字段，混表会让「取密钥的那一行」和「归属租户的那一行」
// 出现两个真值来源（024 审计 D-03 的同类风险）。

// WeComCSAccount 企业微信客服账号（微信客服 open_kfid + 回调 Token/AESKey）。
//
// 与 WeComAccount（企微自建应用）是两套协议：客服回调按 cursor 增量拉取，
// 游标必须落库，否则拉取 worker 重启后会从空游标重拉或丢消息。
type WeComCSAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID    uint       `gorm:"default:0;index" json:"owner_user_id"`
	AccountName    string     `gorm:"type:varchar(100);not null" json:"account_name"`
	CorpID         string     `gorm:"type:varchar(100);not null" json:"corp_id"`
	Secret         string     `gorm:"type:varchar(200);not null" json:"-"`
	OpenKfID       string     `gorm:"type:varchar(100);not null;index" json:"open_kfid"`
	Token          string     `gorm:"type:varchar(200)" json:"-"`
	AESKey         string     `gorm:"type:varchar(200)" json:"-"`
	SyncCursor     string     `gorm:"type:varchar(512)" json:"sync_cursor"`
	CursorUpdated  *time.Time `json:"cursor_updated_at"`
	AIAgentEnabled bool       `gorm:"default:false" json:"ai_agent_enabled"`
	Status         int        `gorm:"default:1" json:"status"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WeComCSAccount) TableName() string { return "wecom_cs_accounts" }

// OneBotAccount OneBot v11 协议端（NapCat/Lagrange）账号。
//
// AccessToken 同时用于反向 WS 握手与 HTTP API 调用；SelfID 是 QQ 号，
// 用于多账号并存时把「谁连上来的」映射回账号行。
type OneBotAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID uint   `gorm:"default:0;index" json:"owner_user_id"`
	AccountName string `gorm:"type:varchar(100);not null" json:"account_name"`
	// SelfID 为该协议端登录的 QQ 号（字符串存，避免前端数字精度丢失）。
	SelfID string `gorm:"type:varchar(32);index" json:"self_id"`
	// HTTPBaseURL 为 OneBot HTTP API 基址（如 http://127.0.0.1:3000），反向 WS 无连接时回落。
	HTTPBaseURL    string     `gorm:"type:varchar(500)" json:"http_base_url"`
	AccessToken    string     `gorm:"type:varchar(200)" json:"-"`
	AIAgentEnabled bool       `gorm:"default:false" json:"ai_agent_enabled"`
	LastOnlineAt   *time.Time `json:"last_online_at"`
	Status         int        `gorm:"default:1" json:"status"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OneBotAccount) TableName() string { return "onebot_accounts" }

// SlackAccount Slack app 账号（Events API + chat.postMessage）。
type SlackAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID    uint      `gorm:"default:0;index" json:"owner_user_id"`
	AccountName    string    `gorm:"type:varchar(100);not null" json:"account_name"`
	TeamID         string    `gorm:"type:varchar(64);index" json:"team_id"`
	AppID          string    `gorm:"type:varchar(64)" json:"app_id"`
	BotToken       string    `gorm:"type:text" json:"-"`
	SigningSecret  string    `gorm:"type:varchar(200)" json:"-"`
	AIAgentEnabled bool      `gorm:"default:false" json:"ai_agent_enabled"`
	Status         int       `gorm:"default:1" json:"status"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SlackAccount) TableName() string { return "slack_accounts" }

// WeCombBotAccount 企业微信智能机器人账号（回调 Token/EncodingAESKey + 推送 webhook）。
type WeCombBotAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID    uint      `gorm:"default:0;index" json:"owner_user_id"`
	AccountName    string    `gorm:"type:varchar(100);not null" json:"account_name"`
	Token          string    `gorm:"type:varchar(200)" json:"-"`
	AESKey         string    `gorm:"type:varchar(200)" json:"-"`
	WebhookURL     string    `gorm:"type:varchar(500)" json:"webhook_url"`
	AIAgentEnabled bool      `gorm:"default:false" json:"ai_agent_enabled"`
	Status         int       `gorm:"default:1" json:"status"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WeCombBotAccount) TableName() string { return "wecombot_accounts" }

// CustomAccount 通用自定义渠道账号（固定 JSON 契约 + HMAC 签名 + 回调 URL）。
type CustomAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OwnerUserID    uint      `gorm:"default:0;index" json:"owner_user_id"`
	AccountName    string    `gorm:"type:varchar(100);not null" json:"account_name"`
	Secret         string    `gorm:"type:varchar(200)" json:"-"`
	CallbackURL    string    `gorm:"type:varchar(500)" json:"callback_url"`
	AIAgentEnabled bool      `gorm:"default:false" json:"ai_agent_enabled"`
	Status         int       `gorm:"default:1" json:"status"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (CustomAccount) TableName() string { return "custom_accounts" }
