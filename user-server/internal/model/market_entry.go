// market_entry.go 模板/技能市场写侧闭环（specs/013 S-22：发布-审核-支付）。
//
// 两张表，一份分工：
//   - market_catalog_entries：平台级目录条目（发布 → 审核 → 上架）。**无 tenant_id**
//     —— 目录是平台级共享橱窗，公开接口 /api/template-market/* 未登录可达，
//     若带 tenant_id 会被租户登记表（tenancyidx 按 tenant_id 列内省）判成租户表，
//     匿名读在 on 模式被 fail-closed 拦死（「需要租户上下文」）——公开橱窗当场全废。
//     锚点：internal/router/cross_tenant_scan_test.go 对
//     GET /api/template-market/detail/:id 的判词「平台级目录，无 tenant_id」。
//   - market_entry_orders：购买订单（带 tenant_id，对齐 module_orders）。
//     订单是租户生意数据：读路径全在鉴权档（auth 组挂 TenantMiddleware），
//     租户隔离 callback 照常生效。
//
// 建表登记：internal/pkg/db/migrate.go allModels()（与 bills / payments 同一机制）。
// 表归属：internal/model/module_tables.go coreOwnedTables（与 module_orders 同一条：
// 市场是 platform-link 基础模块的购买/橱窗入口，**购买入口不能被购买条件关闭**，
// 表随基座常在，不属任何可售模块）。
package model

import (
	"time"
)

// ── 目录条目状态机（对齐 asset_store 上架单的写法：单点跃迁表）──────────────

const (
	// MarketEntryPending 待审核（发布入口的落点：提审即进审核台）
	MarketEntryPending = "pending"
	// MarketEntryApproved 已上架（公开橱窗可见）
	MarketEntryApproved = "approved"
	// MarketEntryRejected 已驳回（带理由；改后可重新提审）
	MarketEntryRejected = "rejected"
	// MarketEntryDisabled 已下架（审核台主动撤柜；可重新上架）
	MarketEntryDisabled = "disabled"
)

// marketEntryTransitions 目录条目状态跃迁表。
//
// 审核是唯一写通道：pending 只能被 approved/rejected；上架后的下架与
// 复架都在审核台；rejected 复活只有一条路——提交人改完重新提审（回到 pending）。
var marketEntryTransitions = map[string]map[string]bool{
	MarketEntryPending:  {MarketEntryApproved: true, MarketEntryRejected: true},
	MarketEntryApproved: {MarketEntryDisabled: true},
	MarketEntryRejected: {MarketEntryPending: true},
	MarketEntryDisabled: {MarketEntryApproved: true, MarketEntryPending: true},
}

// MarketEntryTransitionAllowed 目录条目状态跃迁判定（single point of truth）。
func MarketEntryTransitionAllowed(from, to string) bool {
	return marketEntryTransitions[from][to]
}

// MarketEntryKindAvailable 契约 kind 值域（与 repository 的 EntryKind* 同值；
// 不直接引用 repository 包——model 是最底层，反向依赖仓储会成环）。
func MarketEntryKindAvailable(kind string) bool {
	return kind == "template" || kind == "skill"
}

// MarketEntryAvailabilityAvailable 契约 availability 值域（同上，与 repository 的
// Availability* 常量同值）。
func MarketEntryAvailabilityAvailable(a string) bool {
	switch a {
	case "builtin", "needs_module", "coming_soon":
		return true
	}
	return false
}

// MarketCatalogEntry 平台级目录条目（表 market_catalog_entries）。
//
// 元数据列对齐 specs/013 §2.1 契约（id/kind/name/category/summary/source/availability/tags），
// 写侧增加状态机与价格：price_cents=0 免费领取（0 元单直通，对齐模块市场），
// >0 需下单 + 线下收款确认后才算「已获得」。
type MarketCatalogEntry struct {
	// ID 目录唯一标识（目录契约主键，字符串形如 tpl-xxx / skill-xxx）。
	// 不用自增：ID 直接出现在公开 URL /api/template-market/detail/:id 上，
	// 且要与进程内种子的 ID（card-douyin 等）共处一个命名空间。
	ID string `gorm:"type:varchar(64);primaryKey" json:"id"`
	// Kind 模板 or 技能（template|skill，值域见 MarketEntryKindAvailable）
	Kind string `gorm:"type:varchar(16);index;not null" json:"kind"`
	Name string `gorm:"type:varchar(128);not null" json:"name"`
	// Category 分类筛选维度（目录契约列，与种子条目同族：channel_card/mcp/...）
	Category string `gorm:"type:varchar(32);index;not null" json:"category"`
	Summary  string `gorm:"type:text" json:"summary"`
	// Source 事实源：指向既有实现（文件路径/表/服务名），不复制内容本身（spec FR-7）
	Source string `gorm:"type:varchar(255);not null" json:"source"`
	// Availability builtin|needs_module|coming_soon（值域见 MarketEntryAvailabilityAvailable）
	Availability string `gorm:"type:varchar(16);not null" json:"availability"`
	// Tags 辅助标签（jsonb 数组，过滤语义在 service 层）
	Tags string `gorm:"type:jsonb;not null;default:'[]'" json:"tags"`
	// Status 状态机（见 marketEntryTransitions）
	Status string `gorm:"type:varchar(16);index;not null;default:'pending'" json:"status"`
	// PriceCents 售价（分）。0=免费；>0 需下单并经线下收款确认。
	PriceCents int64 `gorm:"type:bigint;not null;default:0" json:"price_cents"`

	// SubmitterID 提交人（user id；0=系统预置）。发布入口的归属列。
	SubmitterID int64 `gorm:"index;not null;default:0" json:"submitter_id"`
	// RejectReason 驳回理由（审核台写，提交人可见）
	RejectReason string `gorm:"type:text" json:"reject_reason"`
	// ReviewedBy/ReviewedAt 最近一次审核动作（操作人/时点；0/nil=未审过）
	ReviewedBy int64      `json:"reviewed_by"`
	ReviewedAt *time.Time `json:"reviewed_at"`
	// EnabledAt/DisabledAt 上架/下架时点（delivery.md §未实现 2 建议的列）
	EnabledAt  *time.Time `json:"enabled_at"`
	DisabledAt *time.Time `json:"disabled_at"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (MarketCatalogEntry) TableName() string { return "market_catalog_entries" }

// ── 购买订单 ────────────────────────────────────────────────────────────────

const (
	// MarketOrderPendingPayment 待支付（线下收款路径的等待态）
	MarketOrderPendingPayment = "pending_payment"
	// MarketOrderPaid 已支付（线下确认收款后落定；终态=已获得该条目）
	MarketOrderPaid = "paid"
	// MarketOrderCancelled 已取消（买家撤单；终态）
	MarketOrderCancelled = "cancelled"
)

// marketOrderTransitions 订单状态跃迁表：待支付 → 已支付 / 已取消。
// paid/cancelled 是终态——没有"已支付再取消"（退款域不在本闭环，明确不设）。
var marketOrderTransitions = map[string]map[string]bool{
	MarketOrderPendingPayment: {MarketOrderPaid: true, MarketOrderCancelled: true},
	MarketOrderPaid:           {},
	MarketOrderCancelled:      {},
}

// MarketOrderTransitionAllowed 订单状态跃迁判定。
func MarketOrderTransitionAllowed(from, to string) bool {
	return marketOrderTransitions[from][to]
}

// MarketEntryOrder 目录条目购买订单（表 market_entry_orders）。
//
// 与 module_orders 同构的**线下收款**形态：用户端只建单 + 展示，
// 钱到账与否由审核台（管理端）MarkPaid 确认——本仓没有聚合支付通道，
// 把「收款确认」做成显式管理动作，而不是假装有一条不存在的支付回调。
type MarketEntryOrder struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// OrderNo 业务单号（展示/对账键，TM 前缀 + 时间戳 + 随机段）
	OrderNo string `gorm:"type:varchar(32);uniqueIndex;not null" json:"order_no"`
	// EntryID 购买哪条目录条目（market_catalog_entries.id，字符串外键语义，不回查）
	EntryID string `gorm:"type:varchar(64);index;not null" json:"entry_id"`
	// EntryName 下单时的条目名快照（条目改名/下架后订单页仍读得懂）
	EntryName string `gorm:"type:varchar(128);not null" json:"entry_name"`
	// BuyerID 买家（user id）
	BuyerID int64 `gorm:"index;not null" json:"buyer_id"`
	// AmountCents 下单时快照的售价（分）；0=免费领取单（直接落 paid）
	AmountCents int64  `gorm:"type:bigint;not null;default:0" json:"amount_cents"`
	Currency    string `gorm:"type:varchar(8);not null;default:'CNY'" json:"currency"`
	// Status pending_payment → paid | cancelled（见 marketOrderTransitions）
	Status string `gorm:"type:varchar(16);index;not null;default:'pending_payment'" json:"status"`
	// ContactPhone 线下收款联系方式（待支付单必填——运营联系不上买家=流程断链，
	// 对齐 module_market 的线下路径联系人约束）
	ContactPhone string `gorm:"type:varchar(32)" json:"contact_phone"`
	// Remark 买家备注
	Remark string `gorm:"type:varchar(255)" json:"remark"`

	// PaidBy/PaidAt 收款确认人/时点（审核台 MarkPaid 落）
	PaidBy int64      `json:"paid_by"`
	PaidAt *time.Time `json:"paid_at"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (MarketEntryOrder) TableName() string { return "market_entry_orders" }
