package model

import "time"

// SalesEvent 销售事件流持久化（H2 技术债修复）
//
// 取代原 SalesDashboard 纯内存 slice 存储（重启丢数据、无界增长 OOM 风险）。
// 记录订单 / 跟进 / AI 谈单 / 订单草稿四类销售事件，供销售工作台与业绩聚合
// 做 DB 权威统计；与实时驾驶舱（dashboard_sse_stats）互补。
//
// OpportunityID / QuoteID 两列（T-P2-04 / R-5，原文件头自记的 H2 技术债）是给 LTC
// 新域预留的外键位。预留时本卡只开列、**不写任何生产者**，并把"由谁写"记成了
// "商机事件由 P4 写入、报价事件由 P6 写入"——前者至今没发生（见 a），后者发生在
// T-P6-03。理由与三条口径如下：
//
//	a) 【T-P6-03 后更正，实测于本卡】"整张表零写入"这句已经不成立：从报价发送腿起，
//	   `event_type = "quote"` 这一类是生产路径上的**第一个**写入方，它把
//	   opportunity_id / quote_id / owner_id 三列一起填上（internal/service/quote_send.go
//	   的 SalesEvent 构造点）。除此之外的四类事件仍然没有生产者：它们的构造点都在
//	   `NewSalesEventStatsService(` 里，而该服务在非测试代码里 0 构造点、注入点 `SetStats(`
//	   亦 0 命中，已接线的 FollowUpService 走 `if s.stats != nil` 保护（stats 恒 nil）。
//	   该状态由 `check-unwired-assets.sh` **项 9** 盯梢，接线前不得宣称"商机事件已入库"。
//
//	   这一更正不改 c) 的结论：目前**没有任何**按 opportunity_id / quote_id 检索
//	   sales_events 的读路径（写方 ≠ 读方），索引照旧随第一个读方一起加。
//	b) NULL 与 '' 各有含义，别当成一回事：列可空且**不回填**，所以迁移前已有的行是 NULL
//	   （= 这条事件发生在有商机概念之前）；迁移后经 GORM 写入的行是 ''
//	   （= 有该概念、但这次事件没有商机）。Go 侧用 string 而非 *string，是让写入侧
//	   不必处处判 nil；代价是**经 GORM 读回时 NULL 与 '' 都落进同一个空串**，
//	   真要区分这两层历史只能在 SQL 里判 `IS NULL` / `= ''`
//	   （internal/repository/sales_event_ltc_columns_test.go 把这条差异钉住了）。
//	c) 两列**今天不带索引**，计划与理由：首个按 `opportunity_id` 检索的读路径（P4 的
//	   商机时间线）落地时，随那张卡加单列索引 `idx_sales_events_opportunity`；按
//	   `quote_id` 的检索同理。提前建索引只会在一整张只增不改的事件表上加写放大，
//	   而没有任何查询方的"预留索引"连有没有用都无法验证（R-4 那条僵尸表是同一课）。
type SalesEvent struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType   string `gorm:"type:varchar(30);index" json:"event_type"`
	OrderID     string `gorm:"type:varchar(64);index" json:"order_id"`
	DraftID     string `gorm:"type:varchar(64);index" json:"draft_id"`
	CustomerID  string `gorm:"type:varchar(64);index" json:"customer_id"`
	OwnerID     string `gorm:"type:varchar(64);index" json:"owner_id"`
	ProductName string `gorm:"type:varchar(200)" json:"product_name"`
	// OpportunityID / QuoteID 是 LTC 预留位：可空、不回填、暂无索引、暂无生产者，
	// 三条口径见本类型文档 a/b/c（漏掉任一条都会让这两列变成下一个 conversion_funnels）。
	OpportunityID string `gorm:"type:varchar(64)" json:"opportunity_id"`
	QuoteID       string `gorm:"type:varchar(64)" json:"quote_id"`
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
	// SalesEventTypeQuote 报价外发成功那一条（T-P6-03）。
	// 它是这张表在生产路径上的**第一个**带 opportunity_id / quote_id 的写入方：
	// 上面文档里的口径 a（"整张表零写入"）随那张卡一起更正，见文件头与
	// scripts/check-unwired-assets.sh 项 9。
	SalesEventTypeQuote = "quote"
)
