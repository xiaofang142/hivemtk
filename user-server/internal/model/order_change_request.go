package model

import (
	"time"

	"gorm.io/gorm"
)

// OrderChangeRequest 改单申请单（TB-05）。
//
// 契约（2026-11-10 冻结，与设计文档 D6/C6 对齐）：
//   - **客服侧只写申请单**：商业订单的变更发生在获准后的外部系统侧
//     （平台 API/ERP/买家自助/人工坐席），OrderPort 只读冻结契约不破；
//   - 状态机：pending → approved → executing → executed | rejected；pending → rejected | cancelled；
//     executed 是终态（不可逆）；非法迁移返回错误（service 层裁决）；
//   - capability 是创建时冻结的平台能力路由结论（v1 数据源=静态规则表，
//     后续数据化）：direct_api（京东改址类，需商家授权🔵）｜buyer_initiated
//     （抖音/闲鱼=买家发起型）｜via_erp（管家婆）｜manual（兜底派坐席）；
//   - v1 执行通路只开放 manual / buyer_initiated 两条安全路径；
//     direct_api / via_erp 显式不可用（真实凭证与连接器未接，不承诺未验证能力）；
//   - 订单必须存在于只读镜像（external_orders）才能创建申请单（不编造订单）。
type OrderChangeRequest struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ChangeNo string `gorm:"type:varchar(64);not null;index" json:"change_no"`
	// Platform/OrderID 指向只读镜像（external_orders 平台+订单号），不在客服侧落订单写路径
	Platform   string `gorm:"type:varchar(50);not null;index" json:"platform"`
	OrderID    string `gorm:"type:varchar(100);not null;index" json:"order_id"`
	CustomerID string `gorm:"type:varchar(64);index" json:"customer_id"`
	SessionID  string `gorm:"type:varchar(120)" json:"session_id"`
	// ChangeType 变更类别：address/sku/quantity/note/other
	ChangeType string `gorm:"type:varchar(30);not null" json:"change_type"`
	// PayloadJSON 变更载荷（{"address":"..."}），执行时原样交给获准的外部通道
	PayloadJSON string `gorm:"type:text;not null" json:"payload_json"`
	// Reason 客户诉求摘要（原始话术不入库，防 PII 扩散；引用 session 消息可回查）
	Reason string `gorm:"type:text" json:"reason"`
	// Capability 创建时冻结的能力路由结论
	Capability string `gorm:"type:varchar(30);not null" json:"capability"`
	// Status pending/approved/executing/executed/rejected/cancelled
	Status string `gorm:"type:varchar(16);not null;default:pending;index" json:"status"`

	ApprovedBy string     `gorm:"type:varchar(64)" json:"approved_by"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	// ExecutorNote 执行留痕：manual=坐席指派说明 / buyer_initiated=引导话术已发送 / 执行通道凭据引用
	ExecutorNote string     `gorm:"type:text" json:"executor_note"`
	ExecutedBy   string     `gorm:"type:varchar(64)" json:"executed_by"`
	ExecutedAt   *time.Time `json:"executed_at,omitempty"`
	// RejectedBy/RejectReason 拒绝留痕（可查）
	RejectedBy   string     `gorm:"type:varchar(64)" json:"rejected_by"`
	RejectReason string     `gorm:"type:text" json:"reject_reason"`
	RejectedAt   *time.Time `json:"rejected_at,omitempty"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (OrderChangeRequest) TableName() string { return "order_change_requests" }
