package model

import "time"

// SOP 派发认领状态 / 种类（FA-45：定时器的 durable 认领-投递-确认协议）。
const (
	SOPDispatchClaimPending   = "pending"
	SOPDispatchClaimConfirmed = "confirmed"
	SOPDispatchClaimFailed    = "failed"

	SOPDispatchKindWake = "wake"
	SOPDispatchKindSkip = "skip"
)

// SOPDispatchClaim 定时器派发的耐久认领凭据。
//
// 一条 claim 对应一个 timer（TimerID 唯一）。协议：
//   - 先建 pending claim（带租约），再投递，绝不先改 timer 状态；
//   - 投递成功才 Confirm + 改 timer 状态；投递失败立即释放租约，下轮恢复重派；
//   - 进程在 Confirm 后、改 timer 状态前崩溃：下一轮见 confirmed claim 只补改状态，不重投。
//
// 这样队列满/停机/标记后崩溃都有可复算的恢复路径，"dispatched" 日志只在确认后打。
type SOPDispatchClaim struct {
	TenantID    uint      `gorm:"not null;default:1;index" json:"tenant_id"`
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TimerID     uint      `gorm:"uniqueIndex;not null" json:"timer_id"`
	ExecutionID uint      `gorm:"index;not null" json:"execution_id"`
	NodeID      string    `gorm:"type:varchar(120)" json:"node_id"`
	Kind        string    `gorm:"type:varchar(16);not null" json:"kind"`
	Status      string    `gorm:"type:varchar(16);not null;index" json:"status"`
	LeaseUntil  time.Time `gorm:"index" json:"lease_until"`
	Attempts    int       `gorm:"default:0" json:"attempts"`
	WaitEvent   string    `gorm:"type:varchar(120)" json:"wait_event"`
	WaitPayload JSONMap   `gorm:"type:jsonb" json:"wait_payload"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

func (SOPDispatchClaim) TableName() string { return "sop_dispatch_claims" }
