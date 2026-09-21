// quote.go 报价领域模型（T-P6-01 / W-3 报价域的第一层）
//
// 这张表和商机一样今天**不存在**：全仓对"报价"的表达只到 order_drafts.unit_price
// 与 total_amount 两个孤立数字（判定 B 的草稿域），既没有版本、也没有行项目 ——
// 客户还一次价，上一版报价就在草稿里被覆盖掉了。本卡把"报价是一件有版本链的事"
// 第一次放进 schema。
//
// 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑 GORM AutoMigrate，
// 与 T-P2-01/04/05/06、T-P3-01/03、T-P4-01 七次实测同源；
// 卡面写的 `v3_49_0_quote_migration.go` 因此不产出，理由与落点回灌在执行结果里）。
//
// 本卡只交付**列、值域与版本链的形状**：
//   - 报价的生成（模板 / 行项目 / RAG 话术）在 T-P6-02；
//   - 发送前必经的审批检查点在 T-P6-03 —— 所以本表**不带**任何审批列（判据见下）；
//   - 折扣阈值的二次审批档位在 T-P6-04，阈值住在 ltc.config，不住在这张表里。
package model

import "time"

// Quote 报价版本行（表 quotes）
//
// 一行 = **一个版本**，不是一张报价单。这是本卡最容易在建表时走歪的一步：
// 若把 quote_id 当主键、version 当可变列，"改价"就是一次 UPDATE，
// 而 LTC-12 要的谈判过程（客户还了三次价，每一次报的是什么）当场蒸发，
// 且没有任何一层能事后发现 —— 库里只剩最后一版，它看起来完全正常。
//
// 列宽取向沿用商机那一套（判据来自上下游，不是随手定的）：
//   - 会被原样抄进别的表的键（opportunity_id → sales_events.varchar(64)）：取下游宽度；
//   - 指向本仓自生成主键的列（quote_id / source_id）：与 quotes.id 同型；
//   - 取值可枚举的列（status）：定宽 varchar(16)。
type Quote struct {
	// ID 版本行的行键（形如 q_<unixnano>_<seq>，由 T-P6-02 的生成器产出）。
	//
	// 刻意不用自增 serial：它会被抄进 quote_line_items.quote_row_id，
	// 而"行项目属于哪一版"是这张表的全部意义，引用键必须稳定且多实例不撞。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// QuoteID 一张报价单的逻辑号，跨版本**重复出现**（v1/v2/v3 共用一个 quote_id）。
	//
	// 它与 version 组成复合唯一索引 uq_quotes_quote_version：这一条索引就是 AC①
	// "多版本共存 + 版本单调 + 不重复"在库里的全部实现 —— 不需要触发器、不需要约定，
	// 插第二个 v2 会被 PG 直接拒掉。
	//
	// 因此它**不能**自带单列唯一索引（那等于"一个 quote_id 只能有一行"，
	// 版本链当场建不起来），也不能建成匿名 uniqueIndex：
	// GORM 会把两个匿名索引各建成一个单列索引，复合键根本不存在，而测试全绿。
	QuoteID string `gorm:"type:varchar(64);uniqueIndex:uq_quotes_quote_version" json:"quote_id"`

	// OpportunityID 来源商机。报价的first读法就是"这个商机报过几版"，故建索引。
	//
	// 可空吗——不：没有商机就没有报价（W-3 的链条是 线索→商机→报价→订单→回款），
	// 但本卡不建外键（同商机与审批表的判据：跨写路径的引用一旦建 FK，
	// "商机还没落库"就变成"报价建不出来"，而那是两个域的时序问题，不是一件事）。
	// 值域与存在性由 T-P6-02 的生成器负责，这层只保证宽度与可查。
	OpportunityID string `gorm:"type:varchar(64);index" json:"opportunity_id"`

	// Version 链上位置，从 1 起（QuoteVersionFirst）。一行写定后不再变 ——
	// 它能不变是因为本表**没有**"改写版本"的方法：仓储只有追加与状态跃迁两条写路径
	// （见 internal/repository/quote.go 的改写白名单）。
	//
	// 有符号 + not null + default 三条形状判据见 quote_test.go 的
	// TestQuoteVersionChainShape，其中"PG 里 NULL 互不相等"那条是唯一索引的命门。
	Version int64 `gorm:"not null;default:1;uniqueIndex:uq_quotes_quote_version" json:"version"`

	// Status 这一版的生命周期位置（值域见 QuoteStatuses）。
	//
	// 与商机 status 的分工：那一边答"这单还在不在跑"，这一边答"这一版报价走到哪了"。
	// 一版报价被拒不影响商机还活着，商机赢了也不代表某版报价被接受 ——
	// 合成一个字段的两条后果各有一处先例：漏斗与赢率塌成同一个数（T-P4-01 拦过），
	// 以及"客户拒了 v2"被读成"这单丢了"。
	//
	// **不建索引**（与 opportunity_id/source_id 相反）：本卡没有任何按状态捞的读方，
	// 而 P6-02/03 真要按状态筛的形状是 `WHERE opportunity_id = ? AND status = ?`——
	// 由 opportunity_id 的索引取行、status 只做过滤。预留索引与预留列同罪（同族判据见
	// internal/pkg/db/opportunity_migration_test.go 的索引用例）。
	Status string `gorm:"type:varchar(16)" json:"status"`

	// SourceID 这一版是从哪一版长出来的（空串 = 第一版）。
	//
	// AC② 的落点。只有 version 而没有 source_id，"版本链"就退化成"版本序列"：
	// v3 是不是真的接在 v2 之后，没人能从数据里读出来 —— 而 LTC-12 要的是可回溯的谈判过程。
	// 空串在这里是**有语义的合法值**（第一版没有来路），所以刻意不建带谓词的唯一索引，
	// 也不给它 NULL：两种"没有来路"会让读侧各写一遍判空。
	SourceID string `gorm:"type:text;index" json:"source_id"`

	// ValidUntil 有效期截止时间，可空 = 不设有效期（对公报价的常态）。
	//
	// 必须可空：零值时间(0001-01-01) 与 opportunities.expected_close_at 同罪 ——
	// "还没定价到什么时候"和"公元 1 年就过期了"在聚合里读成同一个数。
	// 时区：库里是 timestamptz，比较发生在读侧（T-P6-02/04），
	// 本卡不写任何"按天取整"的表达式，因为那条路径要先经过
	// check-date-bucket-tz 点名的显式时区处理。
	ValidUntil *time.Time `json:"valid_until"`

	// Currency 一张报价一个币种（ISO 4217 三位码），行项目上不另存。
	//
	// 默认值必须有，理由与商机同：金额不带币种不可算，而空串可算不出错只会在报表里。
	// 顺带一条本表独有的判据：**逐行各存币种会制造"混币报价"**，
	// 那种行的合计（T-P6-02 AC③）没有定义 —— 所以币种在头上，明细里禁列。
	Currency string `gorm:"type:varchar(3);default:'CNY'" json:"currency"`

	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 只在状态跃迁时走（见仓储的 UpdateStatus）：
	// 内容列不可变 ⇒ 这一列"多久没动过"读起来没有第二种解释。
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Quote) TableName() string { return "quotes" }

// QuoteLineItem 报价行项目（表 quote_line_items）
//
// 一行 = 某个**具体版本**的第 N 项。主键是 (quote_row_id, line_no) 复合键，
// 刻意不给它一个 surrogate id：
//
//	行项目没有独立身份，它的身份就是"哪一版的第几行"。有了自定 id，
//	下一步就有人写"更新第 3 行"，而 v1 与 v3 的第 3 行在库里成了同一行可改的记录 ——
//	"旧版不可变"（AC①）是这一列形状的直接推论，不是一条需要额外守的规矩。
//
// ProductID 是指针（可能没有目录项：手工加的行、下架的老品），
// Title/单价/数量全是**快照**：目录会改名改价，而报价是事后要重放的凭证。
type QuoteLineItem struct {
	// QuoteRowID 指向 quotes.id（版本行，不是 quote_id）。
	//
	// 若这里存 quote_id，"v1 的明细"与"v2 的明细"就共用一把键，
	// 读任何一版都会把全部版本的行项目捞回来 —— 合计会翻好几倍，而且没人报错。
	QuoteRowID string `gorm:"type:text;primaryKey" json:"quote_row_id"`

	// LineNo 版本内行序，从 1 起（与 Version 同为"从 1 起"，读侧不必记两套起点）。
	LineNo int64 `gorm:"primaryKey" json:"line_no"`

	// ProductID 引用商品目录（rag_products.id，那里是 size:64 ⇒ 同宽）。
	// 空串合法：手工补的行没有目录项。不建外键（同一域内第二句判据）。
	ProductID string `gorm:"type:varchar(64)" json:"product_id"`

	// Title 品名快照（含规格），text 不是定宽：目录里的名字会改，快照不该跟着改。
	Title string `gorm:"type:text" json:"title"`

	// Quantity / UnitPrice / Amount 全 numeric(·,2)。
	//
	// Amount 是**行净额**（已折后），由 T-P6-02 的生成器按
	// quantity × unit_price × (1 - discount_percent/100) 落一次；
	// 仓储不重算、报价头不存合计（两处各存一份必漂，判据见 quote_test.go）。
	Quantity  float64 `gorm:"type:numeric(12,2)" json:"quantity"`
	UnitPrice float64 `gorm:"type:numeric(14,2)" json:"unit_price"`

	// DiscountPercent 行级折扣（0–100，两位小数）。
	//
	// 住在**行上**而不是报价头上，是为了让 T-P6-04 的阈值判据只有一个算法：
	// 整单折扣率 = Σ折前 − Σ折后 / Σ折前，从明细推。头上存一份"折扣率"的话，
	// 它就是第二个事实源 —— 而行项目改了忘了改它，越界的折扣照样能报出去。
	DiscountPercent float64 `gorm:"type:numeric(5,2)" json:"discount_percent"`

	Amount float64 `gorm:"type:numeric(14,2)" json:"amount"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (QuoteLineItem) TableName() string { return "quote_line_items" }

// QuoteStatuses 报价版本的生命周期值域，按序：draft → sent → {accepted, rejected}，
// expired 是任何未收口状态的定时终局（判定权在 T-P6-02/03，本卡只定义词表）。
//
// 不含 won/lost/paid：那三格分别属于商机与账单域，各有一处事实源（见结构体注释）。
var QuoteStatuses = []string{
	QuoteStatusDraft, QuoteStatusSent, QuoteStatusAccepted, QuoteStatusRejected, QuoteStatusExpired,
}

// 报价状态字面值。值与名同形，是为了让日志、API 载荷与库里的字符串是同一个词 ——
// 中间隔一层映射迟早会有一边漏掉某个值，而漏掉的方向是"读成别的状态"而不是报错。
const (
	QuoteStatusDraft    = "draft"
	QuoteStatusSent     = "sent"
	QuoteStatusAccepted = "accepted"
	QuoteStatusRejected = "rejected"
	QuoteStatusExpired  = "expired"
)

// QuoteCurrencyDefault 建表默认币种，与列上的 DEFAULT 'CNY' 同一字面值。
// 两处必须同源：仓储在 UPDATE 路径上补空币种时取的是这里，取成别的值就是两套默认。
const QuoteCurrencyDefault = "CNY"

// QuoteVersionFirst 链上第一个版本的编号。
//
// 从 1 而不是 0 起：0 保留给"行还没赋值"这个形状，仓储据此分辨
// "创建第一版"与"忘了给版本"（后者是调用方的错，必须报错而不是悄悄当成前者）。
const QuoteVersionFirst int64 = 1

// QuoteStatusKnown 报告 s 是否恰为值域内的某个字面值。
//
// 逐字比、不做规范化：大小写与空格是运营手填与外部系统回传的常态，
// 规范化会把 "DRAFT" 与 "draft" 认成同一件事，而审批侧与漏斗侧看到的是两批人填的。
// 本函数属**值域表**，不是业务校验 —— 校验（哪个状态能跳到哪个）在 T-P6-03。
func QuoteStatusKnown(s string) bool {
	for _, v := range QuoteStatuses {
		if v == s {
			return true
		}
	}
	return false
}
