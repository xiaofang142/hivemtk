// payment.go 回款领域模型（T-P7-02 / N-6 回款域的第二层）
//
// 这张表今天**不存在**：全仓对"钱到账了没有"的表达只到 external_orders.pay_amount
// 那个 bigint（外部电商镜像、按元取整、且不带账单引用）。于是"这张应收收了多少、
// 还欠多少"这两问在系统里答不出来 —— bills 刻意不存"已收"那一格（判据见 model/bill.go：
// 求和发生在 payments 侧），本表就是那个求和的**唯一操作数**。
//
// 收款动作**不在**本域（X8/D-4：钱在外部电商/支付渠道到账）。本表记的是
// "渠道断言过的一笔到账"，一条 = 一笔，不是一条订单也不是一个账单。
//
// 建表登记走 internal/pkg/db 的 allModels()（与 bills / quotes 同一机制，
// 卡面写的 `v3_50_x_payment_migration.go` 因此不产出）。
//
// 与 bills 的分工，一句话：bills 答"我方主张收多少"，payments 答"渠道说收到了多少"。
// 两格之间的差就是欠额 —— 逾期扫描（T-P7-03）与赢单跃迁（T-P7-04）读的都是那个差，
// 所以本表的**幂等键**比多一列少一列重要得多：同一笔钱记两次，欠额会被记成已收。
package model

import "time"

// Payment 回款行（表 payments）
//
// 一行 = **渠道断言过的一笔到账**，且它钉死在一张账单上。
type Payment struct {
	// ID 回款行号（形如 p_<unixnano>_<seq>，由 service 层的生成器产出）。
	//
	// 卡面写的是 payment_id，本列命名随仓内五张同类表统一为 id（bills / quotes /
	// opportunities / approval_requests / human_tasks 无例外）：一张表里同时出现
	// `id` 与 `payment_id` 两种主键命名时，跨表的 JOIN 就没人写得对。
	// 不用自增 serial 的判据与 bills.id 同一条：这把号会进日志与对账导出，多实例不能撞。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// BillID 这张钱冲的是哪张应收（抄 bills.id，不回查）。
	//
	// varchar(64) 与 bills.id 的生成上界同宽（newBillKey 实测 35 字符）：
	// 窄了是截断，截断后的回款行查不回账单，而结清判据正是按 bill_id 求和。
	// **普通索引而非唯一**：分期付款是常态，一张账单允许多笔。
	BillID string `gorm:"type:varchar(64);index" json:"bill_id"`

	// Amount 这一笔收了多少，numeric(14,2) —— 与 bills.amount 同型同宽。
	//
	// **恒正**：方向由 Status 表达（见 PaymentAmountInRange）。允许负数的话，
	// 同一笔退款就有两种合法写法，求和侧读到哪一种是调用方当场的决定。
	Amount float64 `gorm:"type:numeric(14,2)" json:"amount"`

	// Currency 币种（ISO 4217 三位码），由渠道载荷给出，缺省随账单。
	//
	// 为什么不带这一格就不成：账单是 CNY 而这一笔是 USD 时，"收了多少"这个和
	// 没有定义。与 bills.currency 同一判据（混币的合计没有定义）。
	Currency string `gorm:"type:varchar(3);default:'CNY'" json:"currency"`

	// PaidAt 渠道侧的到账时点。
	//
	// 不可空（零值时间会被账龄读成"四千年前就到了"）：载荷没给时由 service 填**接收时刻**，
	// 并在响应面上如实标注（filled_paid_at=true），不假装那是渠道给的时间。
	PaidAt time.Time `json:"paid_at"`

	// ChannelRef 渠道流水号 —— 本表的**幂等键**，AC② 的全部物理形态。
	//
	// 唯一索引 uq_payments_channel_ref：平台重投三次只入账一次。不建它，
	// 三行都"合法"、账单会被推到 paid，只有对账那天看得见。
	//
	// varchar(128)：渠道流水号是有界字符串（支付宝/微信/银联的单号都在 64 内，
	// 留一倍余量），且它是外部可控文本 —— 不设上界就是让渠道用一条超长字符串
	// 占住一个唯一索引项。长度与字符集校验在仓储层，理由见 paymentChannelRefMaxLen。
	ChannelRef string `gorm:"type:varchar(128);uniqueIndex:uq_payments_channel_ref" json:"channel_ref"`

	// Status 这一笔现在算不算数（值域见 PaymentStatuses）。
	//
	// 两格的判据见 PaymentStatuses 的注释：一格"渠道说收到了"，一格"同一笔被冲掉了"。
	// **不建索引**：本表的读路径都先过 bill_id，按状态扫全表的路径今天不存在
	// （预留索引与预留列同罪，见 bills.status 上那条）。
	Status string `gorm:"type:varchar(16)" json:"status"`

	// Platform 带来这笔回款的渠道平台（抄回调路径上的 :platform，不采信载荷自述）。
	//
	// 凭据不留来路就没法回答渠道方与运营那句"这笔是哪单的钱"。平台名取自**路由参数**：
	// 它与签名串里的 platform 同源，载荷自己写的平台名不作数。
	Platform string `gorm:"type:varchar(50);index" json:"platform"`

	// OrderID 触发这笔回款的外部订单号（同上，取自载荷的 order_id）。
	//
	// 与 platform 合起来就是 external_orders 的来路键 —— 这一格只是把"哪条回调"
	// 留在凭据上，**不**据此反查账单：账单来路只有 bill_id 一条（判据见
	// TestPaymentCarriesNoQuoteOrOpportunityColumns，副本会漂）。
	OrderID string `gorm:"type:varchar(100);index" json:"order_id"`

	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 只有"被冲销"这一条路径会推它（金额、账单号、渠道号建后不可改）。
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Payment) TableName() string { return "payments" }

// PaymentStatuses 回款行生命周期，按序：confirmed → reversed。
//
// 为什么恰好这两格：
//   - confirmed：渠道断言这笔钱到账了（回调带来的那一条就是这个）；
//   - reversed：同一笔被渠道冲销（退款/撤销）。没有它，退掉的钱仍被计进结清，
//     一张实际只收到一半的账单会停在 paid —— 那正是要避免的假事实。
//
// 为什么**没有** pending：本表只记渠道已经断言过的资金事实。"我方发起了但渠道没回话"
// 今天没有任何数据源能填（收款动作在外部电商，X8/D-4），写出这格就是造一个
// 永远没有写入方的状态 —— 与 bills 刻意不建 overdue 同一判据。
//
// 为什么**没有** partial：那是账单的词（一张应收收了一半），粒度不同，别混进来。
var PaymentStatuses = []string{
	PaymentStatusConfirmed, PaymentStatusReversed,
}

// 回款状态字面值。值与名同形，理由与 bills 同一条：
// 日志、API 载荷与库里字符串是同一个词，中间隔一层映射迟早有一边漏值。
const (
	PaymentStatusConfirmed = "confirmed"
	PaymentStatusReversed  = "reversed"
)

// PaymentStatusesCounted 是**计入结清**的那些状态。
//
// 这一格是结清金额的唯一事实源：仓储的求和 SQL 与本层任何内存复算都从这里取条件，
// 两边各写一遍字面量的后果与"报价头存合计"同一条 —— 一处加了新状态，另一处静默不算。
var PaymentStatusesCounted = []string{PaymentStatusConfirmed}

// PaymentCurrencyDefault 建表默认币种，与列上的 DEFAULT 'CNY' 同一字面值，
// 且必须与 BillCurrencyDefault 是同一个词（用例逐字比）。
const PaymentCurrencyDefault = "CNY"

// PaymentAmountLimit numeric(14,2) 装得下的最大金额：12 位整数 + 2 位小数。
//
// 不拦的坏法不是"报错难看"而是**重试循环**：溢出会在 Create 那一步抛 SQL 错，
// 回调回 500，而渠道把 500 与"签名不对"看成同一件事 ⇒ 一条坏数据能被反复重投。
const PaymentAmountLimit = 1e12 - 0.01

// PaymentStatusKnown 报告 s 是否恰为值域内的某个字面值。逐字比、不做规范化（同 bills 判据）。
func PaymentStatusKnown(s string) bool {
	for _, v := range PaymentStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// PaymentCountsTowardSettlement 报告这一格状态的钱要不要算进"已收多少"。
// 判据读 PaymentStatusesCounted，不另写一遍字面量。
func PaymentCountsTowardSettlement(status string) bool {
	for _, v := range PaymentStatusesCounted {
		if v == status {
			return true
		}
	}
	return false
}

// PaymentAmountInRange 报告 amount 是不是一笔合法金额：**正数且不超列量程**。
//
// 符号与量程并在一格里判，因为两者的处置动作相同（拒收这条回款、订单镜像照写），
// 而分开两处判会出现"过了一关被另一关拦下却回同一个错"的形状。
func PaymentAmountInRange(amount float64) bool {
	if !(amount > 0) { // 反写以把 NaN 一并拒掉：NaN > 0 为假，但 `amount <= 0` 也为假
		return false
	}
	return amount <= PaymentAmountLimit
}

// paymentStatusTransitions 跃迁表：只有一条边 confirmed → reversed。
//
// 为什么反向不许（reversed → confirmed 无路）：同一笔渠道流水被冲掉之后又被"确认"，
// 说明渠道在拿旧流水号发新钱 —— 那是**新的一笔**，该给它一条新的 channel_ref。
// 就地翻回 confirmed 等于让一笔被冲销的钱重新算进结清，而库里没有任何一行说这件事发生过两次。
var paymentStatusTransitions = map[string][]string{
	PaymentStatusConfirmed: {PaymentStatusReversed},
	PaymentStatusReversed:  {},
}

// PaymentStatusCanTransit 报告 from→to 是不是表里的一条边。from 不在值域时回 false。
func PaymentStatusCanTransit(from, to string) bool {
	for _, next := range paymentStatusTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// PaymentStatusNext 返回某一格的出边（空切片 = 终态）。给读口的理由与 bills 同。
func PaymentStatusNext(from string) []string {
	return paymentStatusTransitions[from]
}
