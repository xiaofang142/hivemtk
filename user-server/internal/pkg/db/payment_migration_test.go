// payment_migration_test.go T-P7-02 回款表在**真库**上的判据。
//
// 与 bill_migration_test.go 同一分工：建表事实源在 internal/pkg/db（allModels() + AutoMigrate），
// 而 model 包 import 不了 testutil（testutil → pkg/db → model 成环）。
// helper（quoteColumns / quoteIndexRow / colsOf）沿用 quote 那一份，同包内不重写。
//
// 本表独有一条值得单独立判据的坏法：**列集合就是幂等判据本身**。
// payments 上没有 quote_id / opportunity_id 那两格，这不是漏写而是判据 ——
// 来路只有 bill_id 一条，抄一份报价号进资金表就是造出第二个事实源
// （账单号会变（重开一张）而报价号不会，两者不同时"这笔钱算在哪张应收上"就有了两套答案）。
// 所以列集合按**逐字全等**比：多一列与少一列同样红。
//
// 另一条：uq_payments_channel_ref 是 AC② 的物理形态，仓储那侧的
// ErrPaymentAlreadyRecorded 全靠按这个名字认 23505 —— 名字不在这里点名，
// 标签改成 `unique` 也能建出唯一约束，而错误串里的约束名变成 PG 拼的那个，
// 于是"同一笔钱重投"与"别的唯一冲突"在仓储里分不开，幂等复用那条路会去认一个不是它的错。
package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// paymentWantColumns 卡面列清单在库里的真实列名（升序 = information_schema 的读回顺序）。
var paymentWantColumns = []string{
	"amount", "bill_id", "channel_ref", "created_at", "currency", "id",
	"order_id", "paid_at", "platform", "status", "updated_at",
}

func paymentRegisteredModels() []any {
	var out []any
	for _, m := range allModels() {
		if _, ok := m.(*model.Payment); ok {
			out = append(out, m)
		}
	}
	return out
}

// testPaymentTime 显式时间戳：到账时点的断言不能靠"现在"（与 testBillTime 同一理由）。
func testPaymentTime() time.Time {
	return time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)
}

func paymentSeedRow(id, billID, channelRef string, amount float64) *model.Payment {
	return &model.Payment{
		ID: id, BillID: billID, Amount: amount, PaidAt: testPaymentTime(),
		ChannelRef: channelRef, Status: model.PaymentStatusConfirmed,
		Platform: "taobao", OrderID: "ord-" + channelRef,
	}
}

// TestPaymentRegisteredInAllModels 建表登记。
//
// 漏这一行的失败面与 bills 同一条而更静：回款入账是**渠道回调**驱动的，
// 没有交互用户在对面看着 —— 表没建时 webhook 那一步只回一个 500，
// 渠道重试三轮之后放弃，而"这张应收到底收过多少钱"永远答不上来。
func TestPaymentRegisteredInAllModels(t *testing.T) {
	got := paymentRegisteredModels()
	if len(got) != 1 {
		t.Fatalf("payments 要登记进 allModels()，实际命中 %d 个：%v", len(got), got)
	}
}

func TestPaymentAutoMigrate_ShapeAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Payment{}); err != nil {
		t.Fatalf("首次 AutoMigrate 失败: %v", err)
	}
	first := quoteColumns(t, db, "payments")
	if !reflect.DeepEqual(first, paymentWantColumns) {
		t.Fatalf("payments 列集合与卡面不符：\n  实际 %v\n  期望 %v", first, paymentWantColumns)
	}

	// 币种故意留空：DEFAULT 'CNY' 与 model.PaymentCurrencyDefault 必须同源（AC② 混币即无和）。
	if err := db.Create(paymentSeedRow("p_seed_1", "b_seed", "ref-seed-1", 120.55)).Error; err != nil {
		t.Fatalf("写入回款行失败: %v", err)
	}

	for i := 2; i <= 3; i++ {
		if err := db.AutoMigrate(&model.Payment{}); err != nil {
			t.Fatalf("第 %d 次 AutoMigrate 应幂等: %v", i, err)
		}
	}
	if got := quoteColumns(t, db, "payments"); !reflect.DeepEqual(got, first) {
		t.Errorf("重跑迁移后 payments 列集合变了：\n  前 %v\n  后 %v", first, got)
	}

	var got model.Payment // 独立零值 struct：复用夹具会把已填字段并进 WHERE
	if err := db.First(&got, "id = ?", "p_seed_1").Error; err != nil {
		t.Fatalf("重跑迁移后回款行丢失: %v", err)
	}
	if got.Status != model.PaymentStatusConfirmed || got.Amount != 120.55 || got.BillID != "b_seed" {
		t.Errorf("存量回款行被改写: %+v", got)
	}
	if got.Currency != model.PaymentCurrencyDefault {
		t.Errorf("currency 应落默认币种 %q，实际 %q（不带币种的回款无法并入结清）", model.PaymentCurrencyDefault, got.Currency)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("两个时间戳应有值（卡面 timestamps）: %+v", got)
	}

	// 行号是 text 主键：它会进日志与对账导出，自增在大促批量重投下会被读成"第 N 笔钱"。
	var pkCols []string
	if err := db.Raw(`SELECT a.attname FROM pg_index x
		JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = ANY(x.indkey)
		WHERE x.indrelid = 'payments'::regclass AND x.indisprimary
		ORDER BY a.attnum`).Scan(&pkCols).Error; err != nil {
		t.Fatalf("查主键失败: %v", err)
	}
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Fatalf("payments 主键列集合 %v，期望 [id]", pkCols)
	}
	var idType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_name = 'payments' AND column_name = 'id'`).Scan(&idType).Error; err != nil {
		t.Fatalf("查 id 列型失败: %v", err)
	}
	if idType != "text" {
		t.Errorf("payments.id 类型 %q，期望 text（自生成行号，与 bills.id 同型）", idType)
	}
}

// TestPaymentChannelRefIsTheIdempotencyKey AC② 的库级形态，四臂。
//
// ③ 是本用例的全部价值：分期与部分退款都是"一张应收多笔钱"，
// 若有人把幂等键误做成 (bill_id) 或 (bill_id, channel_ref)，①② 两臂照样绿，
// 而线上症状是"第二笔尾款记不进去" —— 那是一笔钱掉了，不是报错。
func TestPaymentChannelRefIsTheIdempotencyKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Payment{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	indexes := tableIndexes(t, db, "payments")
	var refIdx *quoteIndexRow
	for k := range indexes {
		switch indexes[k].Name {
		case "uq_payments_channel_ref":
			refIdx = &indexes[k]
		case "idx_payments_bill_id":
			if indexes[k].Uniq {
				t.Errorf("bill_id 上有唯一索引 %s：一张应收只允许一笔回款，分期与尾款记不进去", indexes[k].Name)
			}
		}
		if indexes[k].Cols == "bill_id" && indexes[k].Uniq {
			t.Errorf("bill_id 上有单列唯一索引 %s：分期付款是常态", indexes[k].Name)
		}
	}
	if refIdx == nil {
		t.Fatalf("payments 上没有名为 uq_payments_channel_ref 的索引，实得 %v —— "+
			"标签写成 `unique` 也能建唯一约束，但约束名由 PG 拼，仓储认不到它就分不开\"重投\"与\"别的冲突\"",
			colsOf(indexes))
	}
	if refIdx.Cols != "channel_ref" {
		t.Errorf("uq_payments_channel_ref 的列集合是 %q，期望 channel_ref（幂等键是渠道流水号，不是我们的行号）", refIdx.Cols)
	}
	if !refIdx.Uniq || refIdx.Pkey {
		t.Errorf("该索引必须唯一且不是主键，实际 uniq=%v pkey=%v", refIdx.Uniq, refIdx.Pkey)
	}

	if err := db.Create(paymentSeedRow("p_i_1", "b_installments", "ref-i-1", 100)).Error; err != nil {
		t.Fatalf("第一笔回款写入失败: %v", err)
	}
	// ③ 同一张应收的第二笔（另一个流水号）必须进得来。
	if err := db.Create(paymentSeedRow("p_i_2", "b_installments", "ref-i-2", 69.99)).Error; err != nil {
		t.Errorf("同一张应收的第二笔被拒了（分期并存不成立）: %v", err)
	}
	// ② 同一个流水号重投必须被拒，且报的是**那条约束名**。
	err := db.Create(paymentSeedRow("p_i_3", "b_other", "ref-i-1", 100)).Error
	if err == nil {
		t.Fatal("同一渠道流水号入了两次：AC② 的库级保证失守，一笔钱会被算成两笔")
	}
	if !strings.Contains(err.Error(), "23505") || !strings.Contains(err.Error(), refIdx.Name) {
		t.Errorf("重投报的不是那条唯一约束（错误 %v，期望含 23505 与 %s）：仓储据此分幂等", err, refIdx.Name)
	}
	// ④ 冲销那一笔之后同一号仍不许复活第二行（值域里没有 pending，见 model/payment.go）。
	if err := db.Create(paymentSeedRow("p_i_4", "b_installments", "ref-i-3", 1)).Error; err != nil {
		t.Errorf("第三个流水号被拒了: %v", err)
	}
	var n int64
	if err := db.Model(&model.Payment{}).Where("bill_id = ?", "b_installments").Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 3 {
		t.Errorf("b_installments 名下 %d 行，期望 3（一笔一票，行数就是笔数）", n)
	}
}

// TestPaymentAmountSharesRangeWithBills 结清与被结清的对象必须在同一个量程里。
//
// 差在宽度上不会报错，只会在大额那一单从"存进去"变成"存进去又四舍五入"，
// 而 AC② 要的正是 amount 与 Σpayments.amount 逐分对得上。
func TestPaymentAmountSharesRangeWithBills(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Payment{}, &model.Bill{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	type numRow struct {
		Precision int
		Scale     int
	}
	read := func(table, col string) numRow {
		t.Helper()
		var got numRow
		if err := db.Raw(`SELECT numeric_precision AS precision, numeric_scale AS scale
			FROM information_schema.columns WHERE table_name = ? AND column_name = ?`, table, col).
			Scan(&got).Error; err != nil {
			t.Fatalf("查 %s.%s 列型失败: %v", table, col, err)
		}
		return got
	}
	pay, bill := read("payments", "amount"), read("bills", "amount")
	if pay != bill {
		t.Errorf("payments.amount 与 bills.amount 不同型：回款 (%d,%d) vs 账单 (%d,%d) —— "+
			"结清要逐分对上，两边本来就该是同一个量程",
			pay.Precision, pay.Scale, bill.Precision, bill.Scale)
	}
	if pay.Precision != 14 || pay.Scale != 2 {
		t.Errorf("payments.amount 应为 numeric(14,2)，实际 (%d,%d)", pay.Precision, pay.Scale)
	}
}

// TestPaymentBillIDIsVarchar64AndIndexed 抄过来的账单号那一格有两个坏法，各一臂。
//
// ① 宽度：bills.id 自己是 **text**（无长度上界），所以"两列同宽"在库里没有可比对象 ——
// 真正的判据是"这一格的声明宽度不小于 newBillKey 的生成上界"（model/payment.go 写的是 64，
// 实测行号 35 字符）。把它钉成 64 而不是"≥ 35"：改窄成任何更小的数都是在给截断开门，
// 而截断之后的回款行按 bill_id 求和时匹配不上那张账单 —— 钱记了，结清不动，且无一行报错。
// ② 索引：结清判据每次读都按 bill_id 聚合，没索引就是每读一次对账扫一次全表。
func TestPaymentBillIDIsVarchar64AndIndexed(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Payment{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	var kind string
	var width int
	if err := db.Raw(`SELECT data_type, COALESCE(character_maximum_length, 0)
		FROM information_schema.columns WHERE table_name = 'payments' AND column_name = 'bill_id'`).
		Row().Scan(&kind, &width); err != nil {
		t.Fatalf("查 payments.bill_id 列型失败: %v", err)
	}
	if kind != "character varying" {
		t.Errorf("payments.bill_id 类型 %q，期望 character varying", kind)
	}
	if width != 64 {
		t.Errorf("payments.bill_id 宽度 %d，期望 64（bills.id 的生成上界，见 newBillKey）：改窄就是让账单号被截断", width)
	}
	idx, ok := tableIndexOf(t, db, "payments", "idx_payments_bill_id")
	if !ok {
		t.Error("payments.bill_id 上没有索引：结清按它求和，无索引就是每次对账扫全表")
	} else if idx.Uniq {
		t.Error("payments.bill_id 上的索引是唯一：一张应收只允许一笔回款，分期与尾款记不进去")
	}
}

// tableIndexes / tableIndexOf 读一张表的索引形状（名字 + 列集合 + 是否唯一）。
// SQL 与 quote_migration_test.go 里那一处同源，这里带名字条件以便逐枚点名。
func tableIndexes(t *testing.T, db *gorm.DB, table string) []quoteIndexRow {
	t.Helper()
	var indexes []quoteIndexRow
	if err := db.Raw(`SELECT
			i.relname AS name,
			(SELECT string_agg(a.attname, '+' ORDER BY k.ord)
			   FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
			   JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum) AS cols,
			x.indisunique AS uniq, x.indisprimary AS pkey
		FROM pg_index x JOIN pg_class i ON i.oid = x.indexrelid
		WHERE x.indrelid = ?::regclass`, table).
		Scan(&indexes).Error; err != nil {
		t.Fatalf("查 %s 的 pg_index 失败: %v", table, err)
	}
	return indexes
}

func tableIndexOf(t *testing.T, db *gorm.DB, table, name string) (quoteIndexRow, bool) {
	t.Helper()
	for _, idx := range tableIndexes(t, db, table) {
		if idx.Name == name {
			return idx, true
		}
	}
	return quoteIndexRow{}, false
}
