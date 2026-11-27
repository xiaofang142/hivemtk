package model

import (
	"time"
)

// WeComAccount 企业微信账号
type WeComAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CorpID         string     `gorm:"type:varchar(100);not null" json:"corp_id"`
	CorpSecret     string     `gorm:"type:varchar(200);not null" json:"corp_secret"`
	AgentID        int        `json:"agent_id"`
	AgentSecret    string     `gorm:"type:varchar(200)" json:"agent_secret"`
	AccessToken    string     `gorm:"type:text" json:"access_token"`
	TokenExpires   time.Time  `json:"token_expires"`
	Status         int        `gorm:"default:1" json:"status"`
	LoginState     string     `gorm:"type:varchar(20);default:'offline'" json:"login_state"`
	FriendCount    int        `gorm:"default:0" json:"friend_count"`
	GroupCount     int        `gorm:"default:0" json:"group_count"`
	DailyMsgQuota  int        `gorm:"default:500" json:"daily_msg_quota"`
	DailyMsgUsed   int        `gorm:"default:0" json:"daily_msg_used"`
	QuotaResetAt   *time.Time `json:"quota_reset_at"`
	RiskLevel      string     `gorm:"type:varchar(20);default:'normal'" json:"risk_level"`
	RiskMessage    string     `gorm:"type:text" json:"risk_message"`
	Weight         int        `gorm:"default:100" json:"weight"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastActiveAt   *time.Time `json:"last_active_at"`
	LastErrorAt    *time.Time `json:"last_error_at"`
	LastErrorMsg   string     `gorm:"type:text" json:"last_error_msg"`
	TotalSent      int64      `gorm:"default:0" json:"total_sent"`
	TotalReceived  int64      `gorm:"default:0" json:"total_received"`
	ErrorCount     int        `gorm:"default:0" json:"error_count"`
	CallbackToken  string     `gorm:"type:varchar(100)" json:"callback_token"`
	EncodingAESKey string     `gorm:"type:varchar(200)" json:"encoding_aes_key"`
	WebhookEnabled bool       `gorm:"default:false" json:"webhook_enabled"`
	WebhookPath    string     `gorm:"type:varchar(512)" json:"webhook_path"`
	AIAgentEnabled bool       `gorm:"default:false" json:"ai_agent_enabled"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (WeComAccount) TableName() string {
	return "wecom_accounts"
}

// WeComCustomer 企业微信客户
type WeComCustomer struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID      uint      `gorm:"index" json:"account_id"`
	ExternalUserID string    `gorm:"type:varchar(100);index;not null" json:"external_user_id"`
	Name           string    `gorm:"type:varchar(100)" json:"name"`
	Nickname       string    `gorm:"type:varchar(100)" json:"nickname"`
	Avatar         string    `gorm:"type:varchar(500)" json:"avatar"`
	Gender         int       `json:"gender"`
	Type           int       `json:"type"`
	UnionID        string    `gorm:"type:varchar(100)" json:"union_id"`
	EmployeeID     string    `gorm:"type:varchar(100);index" json:"employee_id"`
	EmployeeName   string    `gorm:"type:varchar(100)" json:"employee_name"`
	AddTime        time.Time `json:"add_time"`
	Source         string    `gorm:"type:varchar(50)" json:"source"`
	Tags           string    `gorm:"type:text" json:"tags"`
	Remark         string    `gorm:"type:varchar(500)" json:"remark"`
	Description    string    `gorm:"type:text" json:"description"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (WeComCustomer) TableName() string {
	return "wecom_customers"
}

// WeComGroup 企业微信客户群
type WeComGroup struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID   uint      `gorm:"index" json:"account_id"`
	ChatID      string    `gorm:"type:varchar(100);unique;not null" json:"chat_id"`
	Name        string    `gorm:"type:varchar(100)" json:"name"`
	OwnerID     string    `gorm:"type:varchar(100);index" json:"owner_id"`
	OwnerName   string    `gorm:"type:varchar(100)" json:"owner_name"`
	MemberCount int       `gorm:"default:0" json:"member_count"`
	MemberLimit int       `gorm:"default:500" json:"member_limit"`
	Status      int       `gorm:"default:1" json:"status"`
	CreateTime  time.Time `json:"create_time"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (WeComGroup) TableName() string {
	return "wecom_groups"
}

// WeComGroupMember 企业微信客户群成员
type WeComGroupMember struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID   uint      `gorm:"index;not null" json:"group_id"`
	ChatID    string    `gorm:"type:varchar(100);index" json:"chat_id"`
	UserID    string    `gorm:"type:varchar(100);index" json:"user_id"`
	UserName  string    `gorm:"type:varchar(100)" json:"user_name"`
	JoinTime  time.Time `json:"join_time"`
	Type      int       `json:"type"`
	IsOwner   bool      `gorm:"default:false" json:"is_owner"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (WeComGroupMember) TableName() string {
	return "wecom_group_members"
}

// WeComMessage 企业微信消息记录
type WeComMessage struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID uint       `gorm:"index" json:"account_id"`
	// 企微 message/send 返回的 msgid 实测 128 字符，原 varchar(100) 会截断报错
	// （SQLSTATE 22001），导致发送记录静默落库失败。见 migrations/104。
	MsgID     string     `gorm:"type:varchar(255);unique" json:"msg_id"`
	MsgType   string     `gorm:"type:varchar(20)" json:"msg_type"`
	ToUser    string     `gorm:"type:text" json:"to_user"`
	ToParty   string     `gorm:"type:text" json:"to_party"`
	ToTag     string     `gorm:"type:text" json:"to_tag"`
	Content   string     `gorm:"type:text" json:"content"`
	MediaID   string     `gorm:"type:varchar(200)" json:"media_id"`
	Status    int        `gorm:"default:0" json:"status"`
	SendTime  *time.Time `json:"send_time"`
	ErrorMsg  string     `gorm:"type:text" json:"error_msg"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (WeComMessage) TableName() string {
	return "wecom_messages"
}

// WeComTag 企业微信标签
type WeComTag struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID     uint      `gorm:"index" json:"account_id"`
	TagID         string    `gorm:"type:varchar(100);not null" json:"tag_id"`
	TagName       string    `gorm:"type:varchar(50);not null" json:"tag_name"`
	GroupID       string    `gorm:"type:varchar(100)" json:"group_id"`
	GroupName     string    `gorm:"type:varchar(50)" json:"group_name"`
	CustomerCount int       `gorm:"default:0" json:"customer_count"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (WeComTag) TableName() string {
	return "wecom_tags"
}
