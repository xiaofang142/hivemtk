package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 邮件出站状态机。两条入站路径共用同一张表：ImmediateSend 走请求协程里的即时投递，
// 其余（排期 / 崩溃遗留）由排水 worker 按节拍认领。
//
//	pending(0) ──到期认领──▶ sending(3) ──投递成功──▶ sent(1)
//	                                   └─投递失败──▶ failed(2)
//	pending(0) ──排期时刻早于 TTL──▶ expired(4)（不再投递，也不留在队列里等下一轮）
//	sending(3) ──认领后进程崩溃超过 stale 窗口──▶ pending(0)
//
// sending 回捞重投是 at-least-once：代价是收件人可能收到重复邮件，
// 收益是崩溃遗留不会变成"永久卡住的第二种 pending"。反向的选择（认领即写 sent）
// 更糟——那会在台账上留下一条根本没发出去的"已发送"。
const (
	EmailStatusPending = 0
	EmailStatusSent    = 1
	EmailStatusFailed  = 2
	EmailStatusSending = 3
	EmailStatusExpired = 4
)

// EmailSend 已发送邮件模型
type EmailSend struct {
	ID          string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	To          string         `gorm:"size:255;not null" json:"to"`
	Subject     string         `gorm:"size:255;not null" json:"subject"`
	Content     string         `gorm:"type:text" json:"content"`
	Attachments string         `gorm:"type:text" json:"attachments"`
	Status      int            `gorm:"default:0" json:"status"`
	SendTime    *time.Time     `json:"send_time,omitempty"`
	SmtpID      string         `json:"smtp_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (e *EmailSend) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
