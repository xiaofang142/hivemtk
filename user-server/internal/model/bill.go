// bill.go 账单领域模型（T-P7-01 / N-6 回款域的第一层）
//
// 这张表今天**不存在**：全仓对"该收多少钱"的表达只到 external_orders.pay_amount
// 那个 bigint（外部电商镜像，且不带报价引用）。于是"报价发出去了、客户接了、
// 该收多少、收没收到"这四问在系统里一个都答不出 —— M3 的回款域从这一行开始。
//
// 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑 GORM AutoMigrate，
// 与 T-P2-01/04/05/06、T-P3-01/03、T-P4-01、T-P6-01 八次实测同源；
// 卡面写的 `v3_50_0_bill_migration.go` 因此不产出，理由与落点回灌在执行结果里）。
//
// 收款动作**不在**本域（X8/D-4：钱在外部电商到账）。本表记的是"我方主张应收多少、
// 到期没有、收清没有"，即 LTC-15/16 要的那本状态账。
//
// 本卡只交付**列、值域与"由哪一版报价派生"的形状**：
//   - 派生的算法与触发点在 T-P7-01 的 service 层（同卡交付）；
//   - 回款行（payment）在 T-P7-02 —— 所以本表**不带**任何"已收金额"列（判据见下）；
//   - 逾期扫描与催收升级在 T-P7-03 —— 所以本表**不带** overdue 这一格状态。
package model

import "time"

// Bill 账单行（表 bills）
//
// 一行 = **一张应收**，且它钉死在某一版报价上。这是本卡最容易在建表时走歪的一步：
// 卡面列清单写的是 quote_id，而 quotes 里那一列是**逻辑报价号、跨版本重复出现**
// （v1/v2/v3 共用一个 quote_id，见 quote.go 的 uq_quotes_quote_version）。
// 拿它当派生的幂等键，等于"一张报价单只能开一张账单"——客户还价后又接受新版时，
// 第二张账单插不进去，而插不进去的方向是"少了一张应收"，那正好是财务上最贵的一种错。
// 所以本表两把键都带：quote_id 是查询维度，quote_row_id 是**版本行的行键**、幂等键。
type Bill struct {
	// ID 账单号（形如 b_<unixnano>_<seq>，由 service 层的生成器产出）。
	//
	// 刻意不用自增 serial：这把键会被抄进 payments.bill_id（T-P7-02），
	// 与 quotes.id 同一判据 —— 引用键必须多实例不撞。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// QuoteID 逻辑报价号（quotes.quote_id），查询维度："这张报价单开过几张账单"。
	//
	// varchar(64) 与 quotes.quote_id 同宽：它是被原样抄下来的那一列，
	// 比上游宽是浪费、比上游窄是截断（截断后的账单查不回报价）。
	//
	// **不能**建唯一索引：见结构体头部的判据。
	QuoteID string `gorm:"type:varchar(64);index" json:"quote_id"`

	// QuoteRowID 派生它的那一版报价的**行键**（quotes.id）。
	//
	// 唯一索引 uq_bills_quote_row 是 AC①"账单由已成交报价派生"在库里的全部硬保证：
	// 同一版被点两次"客户已接受"时，第二张插不进去，服务据此走"复用已有那张"。
	// 不命名的 uniqueIndex 在这里够用（单列），但命名是为了真库用例能按索引名点名它
	// —— 与 quotes 那张表的同一取向。
	QuoteRowID string `gorm:"type:text;uniqueIndex:uq_bills_quote_row" json:"quote_row_id"`

	// OpportunityID 来源商机，从报价行上原样抄下来（不回查报价）。
	//
	// 抄的理由与 sales_events 同源：账单是事后要对账的凭据，读它不该依赖
	// "那一版报价的商机列还没被人改过"。不建外键（跨域时序判据，同 quotes）。
	OpportunityID string `gorm:"type:varchar(64);index" json:"opportunity_id"`

	// Amount 应收金额，numeric(14,2) —— 与 quote_line_items.amount 同型同宽。
	//
	// 值由 service 从**那一版的行净额**相加得出（复用 quoteSumAmount，不另写一个循环），
	// 这就是 AC② 的全部内容：账单金额与报价合计是同一个算法的同一个结果。
	// 头一上存这份合计不与"报价头不存合计"矛盾：报价头存合计会让版本链上的每一版
	// 各有一份可改的数字；账单则**就是**那一版的合计的凭证，它必须是数。
	Amount float64 `gorm:"type:numeric(14,2)" json:"amount"`

	// Currency 币种（ISO 4217 三位码），从报价行原样抄。
	//
	// 一张账单一个币种，与报价同一判据（混币的合计没有定义）。
	// 默认值必须有：金额不带币种不可算，而给存量表补 NOT NULL 列没默认会直接失败。
	Currency string `gorm:"type:varchar(3);default:'CNY'" json:"currency"`

	// DueAt 账期截止，可空 = **账期未定**。
	//
	// 空是合法值而不是偷懒：卡面与本仓都没有"付款条件"这一格（报价模板里的
	// valid_days 是**报价有效期**，不是"多少天内付款"），凭空补一个默认 30 天
	// 会被运营读成合同条款。
	//
	// 空值的后果必须点名，否则它会变成静默语义：T-P7-03 的逾期扫描判据是
	// `status IN (open, partial) ∧ due_at IS NOT NULL ∧ due_at < now`，
	// 账期未定的账单**不参与扫描**、但要在视图里被数出来（"没有逾期"与
	// "没法定逾期"是两件事，合成一件的那天，催收就再也看不见这批单）。
	// 那一句"要被数出来"由 BillRepository.ScanOverdue 的 Undated 兑现（T-P7-03）。
	//
	// 时区：库里是 timestamptz，比较发生在读侧，本层不写任何按天取整的表达式
	// （那条路径要先经过 check-date-bucket-tz 点名的显式时区处理）。
	//
	// 索引：本列跟着 status 进那条复合索引，理由写在 Status 那一格。
	DueAt *time.Time `gorm:"index:idx_bills_status_due,priority:2" json:"due_at"`

	// Status 这张应收走到哪了（值域见 BillStatuses）。
	//
	// 与报价 status、商机 status 的分工：报价答"这一版客户接没接"，商机答"这单还在不在跑"，
	// 本列答"这笔钱收清没有"。三格各有一处事实源 —— 合成一格的两条后果在 T-P4-01
	// （漏斗与赢率塌成同一个数）与 quotes（accepted 不等于 paid）各拦过一次。
	//
	// T-P7-01 交付时这里写的是"**不建索引**：本卡没有按状态捞的读方"（原话留在
	// TestBillKeyedToExactlyOneQuoteVersion 的用例注释里）。读方在 T-P7-03 真的来了：
	// BillRepository.ScanOverdue 每轮按 `status IN (…) ∧ due_at < cutoff` 捞，
	// bills 从此是唯一一张会被定时任务反复带谓词扫的凭证表 —— 那一格按它自己许下的
	// 条件兑现成复合索引。索引名只说列（status_due）不说 chased：部分索引能省体积，
	// 但会把催收集抄进 DDL，而值域的事实源只有下面那个 BillStatusesChased 一处。
	Status string `gorm:"type:varchar(16);index:idx_bills_status_due,priority:1" json:"status"`

	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 只在状态跃迁时走（仓储只有"派生"与"跃迁"两条写路径）：
	// 金额列建后即不可改，所以这一列"多久没动过"读起来没有第二种解释。
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Bill) TableName() string { return "bills" }

// BillStatuses 账单生命周期值域，按序：open → partial → paid，
// voided 是人工收口（开错了、或客户退单后不再主张）。
//
// 不含 overdue：逾期是"查询时 due_at 与今天的比较"，不是一次跃迁。做成状态就要有人
// 每天 UPDATE 一遍，cron 漏跑的那天，库里所有过期单都还是 open，而催收读到的是
// "没有逾期"这个**假事实**（假事实比缺数据贵：它不需要任何人去查第二遍）。
//
// 不含 sent/pushed：账单推送客户属高危出域（新规划 §风险表"账单推送=高"），
// 那一刻它作为新的 subject_type 进 approval_requests，不在本表开列。
var BillStatuses = []string{
	BillStatusOpen, BillStatusPartial, BillStatusPaid, BillStatusVoided,
}

// 账单状态字面值。值与名同形，理由与 quotes 同一条：
// 日志、API 载荷与库里字符串是同一个词，中间隔一层映射迟早有一边漏值。
const (
	BillStatusOpen    = "open"
	BillStatusPartial = "partial"
	BillStatusPaid    = "paid"
	BillStatusVoided  = "voided"
)

// BillCurrencyDefault 建表默认币种，与列上的 DEFAULT 'CNY' 同一字面值，
// 且与 QuoteCurrencyDefault 同源（用例 TestBillKeyColumnWidths 逐字比）。
const BillCurrencyDefault = "CNY"

// BillStatusesChased **参与催收扫描**的那几格：open（一分未收）与 partial（收了一部分仍欠）。
//
// 与 PaymentStatusesCounted 同族同形状：逾期扫描的 SQL 的 IN 列表由这一格生成，
// service 层不另拼一份字面量。两处各写一遍的后果与"报价头存合计"同一条 ——
// 一处加了新状态，另一处静默不催，而"少催"这条方向没有任何人会察觉。
//
// paid/voided 在集外的理由各自不同：已结清的不该再被打扰，已作废的那张主张已经收回。
// `overdue` 不在值域里，因此也不可能出现在这里（它为什么不能做成状态，见本文件
// BillStatuses 的头注释与 T-P7-03 的扫描判据）。
var BillStatusesChased = []string{
	BillStatusOpen, BillStatusPartial,
}

// BillChasedStatusKnown 报告 s 是否是参与催收的那两格之一（逐字比、不规范化）。
func BillChasedStatusKnown(s string) bool {
	for _, v := range BillStatusesChased {
		if v == s {
			return true
		}
	}
	return false
}

// BillStatusKnown 报告 s 是否恰为值域内的某个字面值。
//
// 逐字比、不做规范化：账单状态会被外部系统与运营手填两侧碰，
// 规范化等于把 "OPEN" 与 "open" 认成同一件事，而写进去的是两个不同的字符串。
func BillStatusKnown(s string) bool {
	for _, v := range BillStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// billStatusTransitions 跃迁表：谁能到谁。**本表只声明、不执行**（执行在仓储的 CAS，
// 决定权在 `WHERE id = ? AND status = ?` 那一条，见 repository/bill.go）。
//
// 这张表在 T-P7-02 被放宽过一次，放宽的方向与理由要一起留在表上，否则下一个人会
// 以为那三条回退边是随手加的：T-P7-01 交付时 paid / partial 只进不出，那句的完整
// 前提是"没有任何一处能算出已收多少"—— 那时 payments 还不存在，"改一下 paid"
// 只能凭人手，而人手改凭证正是当初那句注释要挡的东西。
// T-P7-02 把结清判据实现成 Σ 计入结清的回款行之后，status 成了那个和的**函数**：
// 一笔钱被渠道冲销、欠额回来了而账单还写着已结清，那才是要挡的"两头对不上"。
//
// 出边逐条：
//   - open→partial / open→paid：一次性付清是常态，不该被迫先过 partial；
//   - partial→paid：回款累计到位（判据=Σ 计入结清 ≥ amount）；
//   - paid→partial / paid→open、partial→open：回款被冲销，欠额回来（见上面那段）；
//   - open/partial→voided：人工收口，催收与对账都要能表达"这张不再主张"。
//
// 仍然禁的一侧是 voided：**作废不可逆** —— 要重来只能派生新的一张，留下两张说过程
// （与 approval_requests 的"改判无路"同一形状：历史行不被覆盖，是这几张凭证类表共同的取向）。
// paid→voided 同样禁：结清过的应收要作废，等于用一格状态盖掉一次真实发生过收付的历史。
var billStatusTransitions = map[string][]string{
	BillStatusOpen:    {BillStatusPartial, BillStatusPaid, BillStatusVoided},
	BillStatusPartial: {BillStatusPaid, BillStatusOpen, BillStatusVoided},
	BillStatusPaid:    {BillStatusPartial, BillStatusOpen},
	BillStatusVoided:  {},
}

// BillStatusCanTransit 报告 from→to 是不是表里的一条边。
//
// from 不在值域时回 false（而不是"当成 open"）：未知状态既不是"还没收"也不是"收清了"，
// 猜哪一侧都会在猜错那天对不上账 —— 与报价发送侧对审批行状态的同一口径。
func BillStatusCanTransit(from, to string) bool {
	for _, next := range billStatusTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// BillStatusNext 返回某一格的出边（副本无所谓：调用方只读长度与内容）。
// 空切片 = 终态。单独给一个读口的理由与 opportunity 那边同：
// 跃迁表是**表**，不是散落在 if 里的字面值，否则"改判无路"这件事没人能测。
func BillStatusNext(from string) []string {
	return billStatusTransitions[from]
}
