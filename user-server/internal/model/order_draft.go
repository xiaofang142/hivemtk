// order_draft.go 订单草稿持久化模型（T-P2-01 / R-1）
//
// 开工前这张表**不存在**：service.OrderDraft 是纯内存 map（键=草稿 ID），全仓没有
// 对应的 model 也没有 repo。因此这里不是"给已有表补字段"，而是把一份运行态结构
// 第一次落进 schema —— 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑
// GORM AutoMigrate，启动期版本化迁移固定 v1.0.0→v1.0.0 是空跑，见 migrate.go）。
package model

import "time"

// OrderDraft 订单草稿行（表 order_drafts）
//
// 列宽取向刻意与 service 侧字段一一对应，但**标识类列一律用 text 而非 varchar(n)**：
// 本表的 id/customer_id/product_name 由上游（AI 提取的意向、外部客户号）决定长度，
// 定宽列配上不受约束的输入 = 写入硬失败（T-P1-08 在 tool_call_audits 上刚踩过：
// trace_id 定宽 64 被上游透传的长值撑爆，而 CreateInBatches 整批回滚）。
// 只有取值集合可枚举的列（status/source）才用定宽。
type OrderDraft struct {
	// ID 草稿业务主键（service.generateDraftID 产出，形如 draft_<unixnano>_<seq>）。
	// 用它做主键而不是自增 serial：草稿 ID 会作为 sales_events.draft_id 的外键值
	// 散在事件流里，换不成稳定的键就没法"重启后仍能关联"（本卡 AC④）。
	ID          string `gorm:"type:text;primaryKey" json:"id"`
	CustomerID  string `gorm:"type:text;index;uniqueIndex:uq_order_draft_pending,priority:1,where:status = 'pending'" json:"customer_id"`
	OneID       string `gorm:"type:text;index" json:"one_id"`
	OwnerID     string `gorm:"type:text;index" json:"owner_id"`
	ProductName string `gorm:"type:text;uniqueIndex:uq_order_draft_pending,priority:2,where:status = 'pending'" json:"product_name"`
	ProductID   string `gorm:"type:text" json:"product_id"`
	Category    string `gorm:"type:text" json:"category"`

	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `gorm:"type:numeric(14,2)" json:"unit_price"`
	TotalAmount float64 `gorm:"type:numeric(14,2)" json:"total_amount"`
	Confidence  float64 `gorm:"type:double precision" json:"confidence"`

	Source     string `gorm:"type:varchar(32);index" json:"source"`
	SourceText string `gorm:"type:text" json:"source_text"`
	IntentID   string `gorm:"type:text" json:"intent_id"`

	Status       string  `gorm:"type:varchar(16);index" json:"status"`
	OrderID      string  `gorm:"type:text;index" json:"order_id"`
	Note         string  `gorm:"type:text" json:"note"`
	CancelReason string  `gorm:"type:text" json:"cancel_reason"`
	Metadata     JSONMap `gorm:"type:jsonb;default:'{}'" json:"metadata"`

	CreatedAt   time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ExpiresAt   time.Time  `gorm:"index" json:"expires_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
}

// TableName 指定表名
func (OrderDraft) TableName() string { return "order_drafts" }

// 草稿状态值域（与 service.DraftStatus 逐字一致；本包只存字符串，不引 PG ENUM，
// 理由同 sales_events：值域改动不该需要 DROP TYPE）。
const (
	OrderDraftStatusPending   = "pending"
	OrderDraftStatusConfirmed = "confirmed"
	OrderDraftStatusCancelled = "cancelled"
	OrderDraftStatusExpired   = "expired"
)

// OrderDraftTerminalStatuses 终态集合：只有落进这里的草稿才可被保留期清理。
//
// 为什么刻意不含 pending：**清理逻辑能删掉的每一行都必须是"不会再被任何业务流程
// 改回去"的行**。pending 会被 Confirm/Cancel/ExpireOverdue 改写；confirmed 是成单
// 证据链的一环（sales_events.draft_id 指向它），删了就等于把成交归因改成孤证。
var OrderDraftTerminalStatuses = []string{
	OrderDraftStatusCancelled,
	OrderDraftStatusExpired,
}
