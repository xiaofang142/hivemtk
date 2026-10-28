// reach_deferred_outbound.go 主动触达延迟出站持久化模型（表 reach_deferred_outbound）
//
// D02：主动触达（reach 发送管线）命中全渠道 quiet hours / 合规静默窗口时，
// 原始发送意图（完整 ReachSendRequest）持久落库；进程重启后仍有一条待发，
// 到期由 worker 经原管线重发。与 AI 回复的 reach_delayed_outbound（D03）互补：
// 两张表各管一条管道的 quiet 语义，禁止合并。
package model

import (
	"time"
)

// ReachDeferredOutbound 主动触达延迟出站记录
type ReachDeferredOutbound struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel     string `gorm:"type:varchar(30);index" json:"channel"`
	AccountID   string `gorm:"type:varchar(64)" json:"account_id"`
	RecipientID string `gorm:"type:varchar(128);index" json:"recipient_id"`
	CustomerID  string `gorm:"type:varchar(64);index" json:"customer_id"`
	OperatorID  string `gorm:"type:varchar(64)" json:"operator_id"`
	// Purpose 延后原因：quiet_hours（全渠道静默时段）/ compliance（地区×行业静默窗口）
	Purpose string `gorm:"type:varchar(20);default:'quiet_hours';index" json:"purpose"`
	// RequestJSON 完整 ReachSendRequest 快照（重发时按原意图执行，不重新取数）
	RequestJSON JSONMap `gorm:"type:jsonb" json:"request_json"`

	SendAt time.Time  `gorm:"index" json:"send_at"`
	Status string     `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	SentAt *time.Time `json:"sent_at"`

	// Attempts 重发尝试次数；达到上限判 failed。
	Attempts  int    `gorm:"not null;default:0" json:"attempts"`
	LastError string `gorm:"type:text" json:"last_error"`

	// 租约（与 D03 同款）：worker 认领时签发 token；过期行转 unknown 待人工核对，
	// 不自动重发（对客户双发是业务事故）。
	ClaimToken string     `gorm:"type:varchar(64);index" json:"claim_token"`
	ClaimedAt  *time.Time `json:"claimed_at"`
	LeaseUntil *time.Time `gorm:"index" json:"lease_until"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (ReachDeferredOutbound) TableName() string { return "reach_deferred_outbound" }

// 主动触达延迟出站的状态与类别
const (
	DeferredReachPurposeQuietHours = "quiet_hours"
	DeferredReachPurposeCompliance = "compliance"

	DeferredReachStatusPending   = "pending"
	DeferredReachStatusSending   = "sending"
	DeferredReachStatusSent      = "sent"
	DeferredReachStatusFailed    = "failed"
	DeferredReachStatusCancelled = "cancelled"
	// DeferredReachStatusUnknown 外部发送结果未知待人工核对（worker 投递中崩溃、租约过期）
	DeferredReachStatusUnknown = "unknown"
)
