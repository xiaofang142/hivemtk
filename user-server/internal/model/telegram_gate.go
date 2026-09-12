package model

import "time"

// TelegramGroupGate TG 群组入群管控网关（每群一条配置）
//
// 两种模式对应两套 Telegram 官方链路：
//   - mode=join_request（方案 A）：群开启 "申请加入"，Bot 收到 chat_join_request 后
//     先私聊用户引导激活（Bot API 无法主动向未交互用户发起私聊，用户必须先 /start），
//     用户激活后才 approveChatJoinRequest 放行。
//   - mode=mute_unlock（方案 B）：用户直接进群，Bot 立即 restrictChatMember 全功能禁言，
//     用户点击群内提示链接到 Bot 私聊 /start 激活后解除禁言。
type TelegramGroupGate struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	AccountID    uint   `gorm:"not null;uniqueIndex:uk_tg_gate_account_chat" json:"account_id"`               // 所属 Bot 账号
	ChatID       string `gorm:"type:varchar(64);not null;uniqueIndex:uk_tg_gate_account_chat" json:"chat_id"` // 群组 chat_id（负数）
	ChatTitle    string `gorm:"type:varchar(255)" json:"chat_title"`
	Mode         string `gorm:"type:varchar(32);default:'mute_unlock'" json:"mode"` // join_request | mute_unlock
	Enabled      bool   `gorm:"default:false" json:"enabled"`                       // 管控总开关
	VerifyTTLMin int    `gorm:"default:10" json:"verify_ttl_min"`                   // 验证有效期（分钟），超时清理

	WelcomeMsg     string `gorm:"type:text" json:"welcome_msg"`          // 进群/申请提示语模板
	VerifyMsg      string `gorm:"type:text" json:"verify_msg"`           // 私聊验证引导语模板
	AIAgentEnabled bool   `gorm:"default:false" json:"ai_agent_enabled"` // 通过验证后是否走智能体欢迎

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (TelegramGroupGate) TableName() string { return "telegram_group_gates" }

// 成员 join_status 取值
const (
	TGMemberPending    = "pending"    // 已申请/已进群，待私聊激活
	TGMemberApproved   = "approved"   // 已激活放行
	TGMemberRestricted = "restricted" // 群内禁言中（方案 B 待解锁）
	TGMemberKicked     = "kicked"     // 超时被清理
)

// TelegramGroupMember TG 群成员验证台账
type TelegramGroupMember struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	AccountID uint   `gorm:"not null;uniqueIndex:uk_tg_gate_member,priority:1" json:"account_id"`
	ChatID    string `gorm:"type:varchar(64);not null;uniqueIndex:uk_tg_gate_member,priority:2" json:"chat_id"`
	UserID    string `gorm:"type:varchar(64);not null;uniqueIndex:uk_tg_gate_member,priority:3" json:"user_id"` // Telegram user_id
	Username  string `gorm:"type:varchar(128)" json:"username"`
	FullName  string `gorm:"type:varchar(128)" json:"full_name"`

	JoinStatus   string     `gorm:"type:varchar(32);default:'pending';index" json:"join_status"` // pending/approved/restricted/kicked
	JoinMode     string     `gorm:"type:varchar(32)" json:"join_mode"`                           // join_request | mute_unlock
	Authorized   bool       `gorm:"default:false" json:"authorized"`                             // 是否完成私聊激活（Bot 已获私聊权限）
	AuthorizedAt *time.Time `json:"authorized_at"`

	VerifyToken string     `gorm:"type:varchar(64);index" json:"verify_token"` // /start <token> 匹配用
	ExpiresAt   *time.Time `json:"expires_at"`                                 // 验证截止时间
	CreatedAt   time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (TelegramGroupMember) TableName() string { return "telegram_group_members" }
