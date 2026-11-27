// reach_delayed_outbound.go AI 回复延迟出站记录模型（表 reach_delayed_outbound）
package model

import (
	"time"
)

// DelayedOutboundReply AI 回复延迟出站记录（quiet hours 延迟队列）
type DelayedOutboundReply struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID             uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform       string `gorm:"type:varchar(30);index" json:"platform"`
	AccountID      string `gorm:"type:varchar(64)" json:"account_id"`
	ConversationID string `gorm:"type:varchar(128);index" json:"conversation_id"`
	SenderID       string `gorm:"type:varchar(128)" json:"sender_id"`
	// CorrelationID 是逻辑回复的不可变身份；quiet-hours/retry/replay 始终复用。
	CorrelationID string `gorm:"type:varchar(100);index" json:"correlation_id"`
	// Origin 明确 AI/human，避免延迟重放把人工回复硬编码成 AI。
	Origin     string `gorm:"type:varchar(20)" json:"origin"`
	SessionID  string `gorm:"type:varchar(120);index" json:"session_id"`
	IsGroup    bool   `gorm:"default:false" json:"is_group"`
	GroupID    string `gorm:"type:varchar(100)" json:"group_id"`
	Visibility string `gorm:"type:varchar(32);default:'unknown'" json:"visibility"`
	// AudienceJSON/VisibilityEvidence/HubExtra 保存原 scope 与 Extra，重放不得降级。
	AudienceJSON       JSONStrings `gorm:"type:jsonb" json:"audience_json"`
	VisibilityEvidence JSONMap     `gorm:"type:jsonb" json:"visibility_evidence"`
	HubExtra           JSONMap     `gorm:"type:jsonb" json:"hub_extra"`
	Content            string      `gorm:"type:text" json:"content"`
	Cards              JSONMap     `gorm:"type:jsonb" json:"cards"`
	SendAt             time.Time   `gorm:"index" json:"send_at"`
	Status             string      `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	SentAt             *time.Time  `json:"sent_at"`

	// Kind 区分两条互补的延迟语义：
	//   - quiet_hours（含改动前的历史空值行）：命中免打扰时段，窗口开放时首发
	//   - send_retry：实时投递失败后的持久化重试，到期重投同一份内容
	Kind string `gorm:"type:varchar(20);default:'quiet_hours'" json:"kind"`
	// Attempts 已重投次数（首次入队为 0），达到上限后判 failed 不再重投。
	Attempts int `gorm:"not null;default:0" json:"attempts"`
	// LastError 最近一次投递失败的原因，供排障与"为什么客户没收到"复盘。
	LastError string `gorm:"type:text" json:"last_error"`

	// 租约三件套（D03）：行被 worker 抢占为 sending 时签发。
	// ClaimToken 用于回写栅栏——旧 worker 拿过期 token 不得改写已被重认领的行；
	// LeaseUntil 过期后行可被回收进程判定（重认领或转 unknown），不再永久悬挂。
	ClaimToken string     `gorm:"type:varchar(64);index" json:"claim_token"`
	ClaimedAt  *time.Time `json:"claimed_at"`
	LeaseUntil *time.Time `gorm:"index" json:"lease_until"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (DelayedOutboundReply) TableName() string { return "reach_delayed_outbound" }

// 延迟出站条目类别与终态
const (
	DelayedKindQuietHours = "quiet_hours"
	DelayedKindSendRetry  = "send_retry"

	DelayedStatusPending = "pending"
	DelayedStatusSending = "sending"
	DelayedStatusSent    = "sent"
	// DelayedStatusSuperseded 重投前发现会话已被回复（人工或后续 AI），这条旧内容不再补投
	DelayedStatusSuperseded = "superseded"
	// DelayedStatusFailed 重投次数用尽仍失败，保留行与 last_error 作为终态证据
	DelayedStatusFailed = "failed"
	// DelayedStatusUnknown 发送结果未知待人工核对（D03）：worker 在投递中途崩溃、
	// 租约过期时无法区分"没发出去"与"发了没回写"，禁止自动重发（可能双发），
	// 由人工/查询核对后经 ResolveUnknown 收敛到 pending/sent/superseded。
	DelayedStatusUnknown = "unknown"
)
