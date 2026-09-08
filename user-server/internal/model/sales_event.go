package model

import "time"

// SalesEvent 销售事件流持久化（H2 技术债修复）
//
// 取代原 SalesDashboard 纯内存 slice 存储（重启丢数据、无界增长 OOM 风险）。
// 记录订单 / 跟进 / AI 谈单 / 订单草稿四类销售事件，供销售工作台与业绩聚合
// 做 DB 权威统计；与实时驾驶舱（dashboard_sse_stats）互补。
type SalesEvent struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType   string     `gorm:"type:varchar(30);index" json:"event_type"`
	OrderID     string     `gorm:"type:varchar(64);index" json:"order_id"`
	DraftID     string     `gorm:"type:varchar(64);index" json:"draft_id"`
	CustomerID  string     `gorm:"type:varchar(64);index" json:"customer_id"`
	OwnerID     string     `gorm:"type:varchar(64);index" json:"owner_id"`
	ProductName string     `gorm:"type:varchar(200)" json:"product_name"`
	// Amount 金额统一 NUMERIC(12,2) 存储，杜绝 float64 二进制误差累积；
	// Go 侧读写仍用 float64（GORM 自动转换），聚合逻辑见 sales_event_stats
	Amount      float64    `gorm:"type:numeric(12,2)" json:"amount"`
	Action      string     `gorm:"type:varchar(20)" json:"action"`
	Channel     string     `gorm:"type:varchar(30)" json:"channel"`
	Result      string     `gorm:"type:varchar(20)" json:"result"`
	Intent      string     `gorm:"type:varchar(50)" json:"intent"`
	IsAI        bool       `json:"is_ai"`
	IsAIHandled bool       `json:"is_ai_handled"`
	Replied     bool       `json:"replied"`
	Transferred bool       `json:"transferred"`
	CostTokens  int        `json:"cost_tokens"`
	LatencyMs   int        `json:"latency_ms"`
	SalesName   string     `gorm:"type:varchar(100)" json:"sales_name"`
	Team        string     `gorm:"type:varchar(100)" json:"team"`
	Tags        string     `gorm:"type:text" json:"tags"`
	JoinedAt    *time.Time `json:"joined_at,omitempty"`
	Confidence  float64    `json:"confidence"`
	Source      string     `gorm:"type:varchar(30)" json:"source"`
	OccurredAt  time.Time  `gorm:"index" json:"occurred_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (SalesEvent) TableName() string { return "sales_events" }

// 销售事件类型常量
const (
	SalesEventTypeOrder        = "order"
	SalesEventTypeFollowUp     = "followup"
	SalesEventTypeAIDeal       = "ai_deal"
	SalesEventTypeOrderDraft   = "order_draft"
	SalesEventTypeSalesProfile = "sales_profile"
)
