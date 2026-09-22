// payment_test.go T-P7-02 回款模型的标签层与值域层判据（不连库）。
//
// 分工与 bills / quotes / opportunities 三处先例一致：本文件拦"标签写错、列名漂了、
// 多带了一格不该在这张表的东西"；真库里的列型、索引形状、AutoMigrate 幂等落在
// internal/pkg/db/payment_migration_test.go（internal/model 引不动 testutil：
// testutil → internal/pkg/db → internal/model 成环）。
//
// 本卡三条 AC 在字段层的落点：
//
//	AC②「重复回调幂等（同 channel_ref 不重复入账）」
//	   → channel_ref 上**带不带唯一索引**就是这条 AC 的物理形态。没有它，
//	     平台重投三次就入账三次，而账单会自己跃迁到 paid —— 库里三行都"合法"，
//	     只有对账那天看得见。它必须落在库上而不是 Go 里的 map（与 uq_bills_quote_row 同判据）。
//	AC①「向后兼容旧载荷」
//	   → 本表**没有任何一条路径**由旧载荷被动写入：这一点在字段面上的体现是
//	     payments 不带 order_id 之外的"可选来源"列（下面 TestPaymentSchemaShape 逐格点名）。
//	AC③「签名校验沿用 T-P2-02 中间件」
//	   → 本卡不给 payments 开任何匿名入口，判据在 router/controller 两层。
package model

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm/schema"
)

// TestPaymentStatusVocabulary status 值域：恰好两格，且这两格就是"钱会不会离开我们"。
//
// confirmed = 渠道说这笔钱到账了；reversed = 同一笔被渠道冲销（退款/撤销）。
// 值域里刻意没有 pending：本表只记**渠道已经断言过**的资金事实，"我方发起了但渠道
// 还没回话"这件事系统里今天没有任何数据源能填它（收款动作在外部电商，X8/D-4），
// 写出这一格就是造一个永远没有写入方的状态。
// 也没有 refunded / partial —— 那是账单与订单镜像的词，回款行的粒度是"一笔"，
// 一笔要么算进结清要么不算。
func TestPaymentStatusVocabulary(t *testing.T) {
	want := []string{"confirmed", "reversed"}
	if !reflect.DeepEqual(PaymentStatuses, want) {
		t.Fatalf("状态值域漂移：期望 %v，实际 %v（加/改一格要先改卡面 AC 与结清算法，不能顺手）", want, PaymentStatuses)
	}
	for _, s := range PaymentStatuses {
		if !PaymentStatusKnown(s) {
			t.Errorf("%q 在值域里却不被 PaymentStatusKnown 认：两判据分家", s)
		}
	}
	// 别的域的词不许被认成本表状态：账单的 partial/paid、订单镜像的 paid/refunded
	// 与回款行的 confirmed 是三套生命周期，混用的后果不是报错而是"读成另一件事"。
	for _, bad := range []string{"", " confirmed", "CONFIRMED", "Confirmed", "pending",
		"open", "partial", "paid", "voided", "refunded", "cancelled", "reverse", "reversed "} {
		if PaymentStatusKnown(bad) {
			t.Errorf("未知状态 %q 被认成合法：值域校验失守", bad)
		}
	}
}

// TestPaymentCountsTowardSettlement 结清算法里"哪几格算钱"的唯一事实源。
//
// 仓储的求和 SQL 与本层任何内存复算都必须读这一格（PaymentStatusesCounted），
// 两边各写一遍字面量的后果与"报价头存合计"同一条：一处加了新状态，另一处静默不算。
func TestPaymentCountsTowardSettlement(t *testing.T) {
	if !PaymentCountsTowardSettlement(PaymentStatusConfirmed) {
		t.Error("confirmed 不计入结清：账单永远到不了 paid")
	}
	if PaymentCountsTowardSettlement(PaymentStatusReversed) {
		t.Error("reversed 计入结清：退款被记成收款，账单会被冲销的钱推到 paid")
	}
	for _, bad := range []string{"", "paid", "unknown"} {
		if PaymentCountsTowardSettlement(bad) {
			t.Errorf("未知状态 %q 被计入结清：结清金额会被一个没定义的东西抬高", bad)
		}
	}
	if len(PaymentStatusesCounted) == 0 {
		t.Fatal("计入结清的状态集为空：求和会退化成恒 0，账单永远欠着")
	}
	for _, s := range PaymentStatusesCounted {
		if !PaymentCountsTowardSettlement(s) {
			t.Errorf("%q 在计入集合里却不被判据认：两判据分家", s)
		}
	}
}

// TestPaymentSchemaShape 卡面列清单逐字钉住（多一列少一列都红）。
//
// 卡面写的是 payment(payment_id, bill_id, amount, paid_at, channel_ref, status)。
// 本表在卡面之外多四格，每格都有下游消费者，逐条：
//   - currency：钱不带币种不可与账单比对（与 bills.currency 同判据）。没有它，
//     一笔 USD 回款会被计进一张 CNY 账单的结清判据里；
//   - platform + order_id：这笔钱由哪条回调带来的。回款是**财务凭据**，
//     凭据不留来路就没法回答渠道方与运营那句"这笔是哪单的钱"；
//   - created_at / updated_at：与冲销动作的时点。
//
// ID 命名偏离卡面的 payment_id 而随仓内五张同类表统一为 id：判据见 payment.go 那一格注释。
func TestPaymentSchemaShape(t *testing.T) {
	st := reflect.TypeOf(Payment{})
	wantFields := []string{
		"ID", "BillID", "Amount", "Currency", "PaidAt",
		"ChannelRef", "Status", "Platform", "OrderID", "CreatedAt", "UpdatedAt",
	}
	for _, name := range wantFields {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在（卡面列清单/仓储写集合会跟着漂）", name)
		}
	}
	if st.NumField() != len(wantFields) {
		t.Errorf("字段数 %d ≠ 声明的 %d：多了的列没人声明、少了的列仓储会写空", st.NumField(), len(wantFields))
	}
	if got := (Payment{}).TableName(); got != "payments" {
		t.Fatalf("表名 %q，期望 payments", got)
	}

	if kindOf(t, st, "ID") != reflect.String {
		t.Errorf("ID 应为字符串行键，实际 %v", kindOf(t, st, "ID"))
	}
	if tag := gormTagOf(t, st, "ID"); !strings.Contains(tag, "primaryKey") {
		t.Errorf("ID 缺 primaryKey 标签：%s", tag)
	}
	if tag := gormTagOf(t, st, "Amount"); !strings.Contains(tag, "numeric(14,2)") {
		t.Errorf("amount 应为 numeric(14,2)（与 bills.amount 同型，否则结清比对发生在两个量程之间）：%s", tag)
	}
	if kindOf(t, st, "Amount") != reflect.Float64 {
		t.Errorf("Amount 应为 float64（与 bills.amount 同型），实际 %v", kindOf(t, st, "Amount"))
	}
	// paid_at 不可空：这一格是账龄与"哪天该停止催收"的读法，
	// 零值时间会被读成"这笔钱四千年前就到了"。缺省由 service 用接收时刻填并如实标注。
	if kindOf(t, st, "PaidAt") != reflect.Struct {
		t.Errorf("PaidAt 应为非空 time.Time，实际 %v", kindOf(t, st, "PaidAt"))
	}
	for _, name := range []string{"BillID", "Currency", "ChannelRef", "Status", "Platform", "OrderID"} {
		if kindOf(t, st, name) == reflect.Ptr {
			t.Errorf("%s 一是指针：会出现既非合法值又非空串的行", name)
		}
	}
}

// TestPaymentChannelRefIsIdempotencyKey AC② 的物理形态。
func TestPaymentChannelRefIsIdempotencyKey(t *testing.T) {
	st := reflect.TypeOf(Payment{})
	tag := gormTagOf(t, st, "ChannelRef")
	if uniqueIndexNamed(tag) != "uq_payments_channel_ref" {
		t.Errorf("channel_ref 应带**命了名**的唯一索引 uq_payments_channel_ref，实际标签 %q："+
			"匿名 uniqueIndex 或没有索引，重投三次就入账三次", tag)
	}
	// 这一列必须是 varchar 且有界：它是外部渠道给的字符串，既是幂等键又被回显进 API。
	// text 无界 + 不校验，等于让渠道用一条 10MB 的字符串占住一个索引项。
	if !strings.Contains(tag, "varchar(") {
		t.Errorf("channel_ref 应是有界 varchar（长度上限的用例见 repository 层）：%s", tag)
	}
}

// TestPaymentBillLinkIsQueryableButNotUnique 一张账单允许多笔回款。
//
// 两条相反的都拦：
//   - 没有索引 ⇒ 对账与催收都要按账单捞回款行，全表扫在凭证表上是给下一个 P8 埋雷；
//   - 建成唯一 ⇒ 分期付第二笔就插不进去，而那方向的坏法是"少记一笔到账"。
func TestPaymentBillLinkIsQueryableButNotUnique(t *testing.T) {
	st := reflect.TypeOf(Payment{})
	tag := gormTagOf(t, st, "BillID")
	if !strings.Contains(tag, "index") {
		t.Errorf("bill_id 缺普通索引（按账单捞回款行是读侧主路径）：%s", tag)
	}
	if strings.Contains(tag, "uniqueIndex") {
		t.Errorf("bill_id 建成了唯一索引 ⇒ 一张账单只能收一笔，分期付第二笔插不进去：%s", tag)
	}
	// 与 bills.id 同宽（64）：这一列是抄下来的账单号，窄了是截断，截断的回款行查不回账单。
	if !strings.Contains(tag, "varchar(64)") {
		t.Errorf("bill_id 应为 varchar(64)（与 bills.id 的生成上界同宽）：%s", tag)
	}
}

// TestPaymentCarriesNoQuoteOrOpportunityColumns 回款只钉账单，不钉报价/商机。
//
// 卡面的列清单里就没有这两格，但真正会写歪的方向是"顺手抄一份方便查"：
// 那等于同一份来路存三处（bills 有、quotes 有、payments 又抄一份），
// 账单被作废或报价换版时 payments 里那份副本没人改，对账就会读到两个答案。
// 要按报价/商机看回款，走 bills 关联（读侧就在那条路上）。
func TestPaymentCarriesNoQuoteOrOpportunityColumns(t *testing.T) {
	st := reflect.TypeOf(Payment{})
	for _, name := range []string{"QuoteID", "QuoteRowID", "OpportunityID", "SettledAmount", "OutstandingAmount"} {
		if _, ok := st.FieldByName(name); ok {
			t.Errorf("payments 多带了一格 %s：来路只有 bill_id 一条（判据见本用例注释），派生数字不落列", name)
		}
	}
}

// TestPaymentAmountInRange 金额恒正、且必须落在列装得下的范围内。
//
// 允许负数金额的后果：同一笔退款有两种合法写法（负数 + confirmed / 正数 + reversed），
// 求和侧读到哪一种是调用方当场的决定，而结清判据正要读那个和。
// 不设上界的后果：numeric(14,2) 溢出会在 Create 那一步报 SQL 错，整条回调 500，
// 而渠道看到的 500 与"签名不对"是同一个动作（重试），一次坏数据能把它顶进重试循环。
// 判据放在模型层的理由：这一格是**列的量程**，与"账单金额从行项目算出来"那类业务判据不同源。
func TestPaymentAmountInRange(t *testing.T) {
	for _, ok := range []float64{0.01, 1, 369.99, PaymentAmountLimit} {
		if !PaymentAmountInRange(ok) {
			t.Errorf("%v 应在合法金额范围内：正数且不超过 numeric(14,2) 的上界", ok)
		}
	}
	for _, bad := range []float64{0, -0.01, -100, PaymentAmountLimit * 2} {
		if PaymentAmountInRange(bad) {
			t.Errorf("%v 不该被认成合法金额（符号或量程）", bad)
		}
	}
}

// TestPaymentStatusTransitionIsOneWay 冲销单向、不可逆冲。
func TestPaymentStatusTransitionIsOneWay(t *testing.T) {
	if !PaymentStatusCanTransit(PaymentStatusConfirmed, PaymentStatusReversed) {
		t.Error("confirmed→reversed 不许：渠道冲销这笔钱时，系统里没有任何一条路能把它标掉")
	}
	if PaymentStatusCanTransit(PaymentStatusReversed, PaymentStatusConfirmed) {
		t.Error("reversed→confirmed 不许：同一笔钱被标两次方向，求和侧就有两个答案")
	}
	if len(PaymentStatusNext(PaymentStatusReversed)) != 0 {
		t.Error("reversed 应是终态（要恢复只能由渠道给一条新 channel_ref 的回款）")
	}
	if PaymentStatusCanTransit(PaymentStatusConfirmed, PaymentStatusConfirmed) {
		t.Error("零位移跃迁不算跃迁")
	}
	if PaymentStatusCanTransit("paid", PaymentStatusReversed) {
		t.Error("起点不在值域内不许被认成合法跃迁")
	}
}

// TestPaymentJSONNamesMatchColumns 出参 json 键与库列名逐格相同（同 bills / quotes 判据）。
func TestPaymentJSONNamesMatchColumns(t *testing.T) {
	st := reflect.TypeOf(Payment{})
	table := Payment{}.TableName()
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		col := paymentColumnName(f, table)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" {
			t.Errorf("Payment.%s 没有 json 键：出参会变成 Go 字段名驼峰，与列名两套", f.Name)
			continue
		}
		if name != col {
			t.Errorf("Payment.%s 的 json 键 %q ≠ 列名 %q", f.Name, name, col)
		}
	}
}

// paymentColumnName 算出 AutoMigrate 会把这一列落成什么名字（同 billColumnName 判据）。
func paymentColumnName(f reflect.StructField, table string) string {
	if tag := f.Tag.Get("gorm"); tag != "" {
		for _, part := range strings.Split(tag, ";") {
			if strings.HasPrefix(part, "column:") {
				return strings.TrimPrefix(part, "column:")
			}
		}
	}
	return schema.NamingStrategy{}.ColumnName(table, f.Name)
}

// TestPaymentCurrencyDefaultIsBillsCurrencyDefault 两张表的缺省币种必须是同一个词。
//
// 漂了之后的形状：账单落成 CNY 而回款落成别的，结清比对两边各说各话。
// 与 TestBillKeyColumnWidths 里那条跨表比字面量的用例同一取向 ——
// 跨表的常量一旦分家，编译器不会报错，只有对账会。
func TestPaymentCurrencyDefaultIsBillsCurrencyDefault(t *testing.T) {
	if PaymentCurrencyDefault != BillCurrencyDefault {
		t.Errorf("回款缺省币种 %q ≠ 账单缺省币种 %q", PaymentCurrencyDefault, BillCurrencyDefault)
	}
	tag := gormTagOf(t, reflect.TypeOf(Payment{}), "Currency")
	if !strings.Contains(tag, "varchar(3)") || !strings.Contains(tag, PaymentCurrencyDefault) {
		t.Errorf("currency 应为 varchar(3) 且默认值就是那个常量：%s", tag)
	}
}
