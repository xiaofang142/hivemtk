package model

import "time"

// FollowUpReminder 跟进提醒持久化行（FA-26/27）。
//
// 为什么建表：FollowUpService 此前是进程内 map（followup_reminder.go 的 reminders），
// HTTP 读侧、回款钩子、AI 工具三条生产链各自 new 一份实例——AI/回款创建的提醒
// 在今日跟进里看不到，重启或多副本即丢。持久化后三条链共享同一权威存储。
//
// 租户与授权（FA-26）：行带 tenant_id（复合索引前缀）；服务层读写一律按 ctx 租户
// 过滤，完成/取消在服务层校验操作者=负责人或管理员——普通销售拿到他人提醒 ID
// 不得改写他人待办。
type FollowUpReminder struct {
	// TenantID 租户隔离键（读侧复合索引前缀；tenancyidx.Normalize 兜底复合化）。
	TenantID uint `gorm:"not null;default:1;index:idx_followup_tenant_owner_status,priority:1" json:"tenant_id"`
	// ID 业务主键（服务层生成的 rem_ 前缀串，与既有 HTTP 契约一致，保持字符串）。
	ID          string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	CustomerID  string     `gorm:"type:varchar(64);index" json:"customer_id"`
	OneID       string     `gorm:"type:varchar(64)" json:"one_id"`
	OwnerID     string     `gorm:"type:varchar(64);index:idx_followup_tenant_owner_status,priority:2" json:"owner_id"`
	Type        string     `gorm:"type:varchar(32)" json:"type"`
	Priority    int        `gorm:"default:1" json:"priority"`
	Title       string     `gorm:"type:varchar(200)" json:"title"`
	Description string     `gorm:"type:text" json:"description"`
	DueAt       time.Time  `gorm:"index" json:"due_at"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Status      string     `gorm:"type:varchar(16);default:'pending';index:idx_followup_tenant_owner_status,priority:3" json:"status"`
	SOPName     string     `gorm:"type:varchar(100)" json:"sop_name,omitempty"`
	AutoHandle  bool       `json:"auto_handle"`
	Channel     string     `gorm:"type:varchar(32)" json:"channel,omitempty"`
}

// TableName 显式指定表名。
func (*FollowUpReminder) TableName() string {
	return "follow_up_reminders"
}
