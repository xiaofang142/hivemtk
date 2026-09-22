// bill_migration_test.go T-P7-01 账单表在**真库**上的判据。
//
// 放在 internal/pkg/db 而不是 internal/model：建表事实源在这里（allModels() + AutoMigrate），
// 而 model 包 import 不了 testutil（testutil → internal/pkg/db → model 成环）。
// 索引/列型的判据形状与 helper（quoteIndexRow、colsOf）沿用 quote_migration_test.go 那一份，
// 同包内不重写一遍。
//
// 标签层那份为什么不够（本表有两条独有风险）：
//   - `uniqueIndex:uq_bills_quote_row` 是 AC①"一版报价一张账单"的唯一硬保证。
//     标签写成 `unique` 与写成 `uniqueIndex:名` 在 GORM 里都能建出唯一约束，
//     但**名字不同** ⇒ 仓储无法按约束名把"重复派生"与"别的唯一冲突"分开，
//     于是幂等复用那条路会去认一个不是它的错误。所以既查列集合也按名字点名它。
//   - `type:numeric(14,2)` 写在标签上不等于库里真是 numeric(14,2)：
//     AC② 要求账单金额与 quote_line_items.amount **同一个量程**，
//     差在宽度上不会报错，只会在大额那一单从"存进去"变成"存进去又四舍五入"。
package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// billWantColumns 卡面列清单在库里的真实列名（升序，理由见 quote 那份注释）。
var billWantColumns = []string{
	"amount", "created_at", "currency", "due_at", "id",
	"opportunity_id", "quote_id", "quote_row_id", "status", "updated_at",
}

func billRegisteredModels() []any {
	var out []any
	for _, m := range allModels() {
		if _, ok := m.(*model.Bill); ok {
			out = append(out, m)
		}
	}
	return out
}

// TestBillRegisteredInAllModels 建表登记：漏这一行的失败面不是"表没建"那么简单，
// 而是**全新部署不建表、代码全对**：派生账单那一步会在日志里留一句话，
// 而应收这张凭据从此没有过行（与 OrderDraft / HumanTask / Opportunity 四处同一判据）。
func TestBillRegisteredInAllModels(t *testing.T) {
	got := billRegisteredModels()
	if len(got) != 1 {
		t.Fatalf("bills 要登记进 allModels()，实际命中 %d 个：%v", len(got), got)
	}
}

func TestBillAutoMigrate_ShapeAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Bill{}); err != nil {
		t.Fatalf("首次 AutoMigrate 失败: %v", err)
	}
	first := quoteColumns(t, db, "bills")
	if !reflect.DeepEqual(first, billWantColumns) {
		t.Fatalf("bills 列集合与卡面不符：\n  实际 %v\n  期望 %v", first, billWantColumns)
	}

	due := testBillTime()
	seed := model.Bill{
		ID: "b_seed_1", QuoteID: "QT-SEED", QuoteRowID: "q_seed_v1",
		OpportunityID: "opp_seed", Amount: 1890.50, DueAt: &due,
		Status: model.BillStatusOpen,
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入账单行失败: %v", err)
	}
	// 账期未定那一格：必须真是 NULL，零值时间在库里是公元 1 年。
	if err := db.Create(&model.Bill{
		ID: "b_seed_no_due", QuoteID: "QT-SEED2", QuoteRowID: "q_seed_v2",
		OpportunityID: "opp_seed", Amount: 100, Status: model.BillStatusOpen,
	}).Error; err != nil {
		t.Fatalf("写入不设账期的账单失败: %v", err)
	}

	for i := 2; i <= 3; i++ {
		if err := db.AutoMigrate(&model.Bill{}); err != nil {
			t.Fatalf("第 %d 次 AutoMigrate 应幂等: %v", i, err)
		}
	}
	if got := quoteColumns(t, db, "bills"); !reflect.DeepEqual(got, first) {
		t.Errorf("重跑迁移后 bills 列集合变了：\n  前 %v\n  后 %v", first, got)
	}

	// 读回用独立零值 struct（复用 seed 会把已填字段并进 WHERE ⇒ record not found 假红）。
	var got model.Bill
	if err := db.First(&got, "id = ?", seed.ID).Error; err != nil {
		t.Fatalf("重跑迁移后账单行丢失: %v", err)
	}
	if got.Status != model.BillStatusOpen || got.Amount != 1890.50 || got.QuoteRowID != "q_seed_v1" {
		t.Errorf("存量账单行被改写: %+v", got)
	}
	if got.Currency != model.BillCurrencyDefault {
		t.Errorf("currency 应落默认币种 %q，实际 %q（金额不带币种不可算）", model.BillCurrencyDefault, got.Currency)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("两个时间戳应有值（卡面 timestamps）: %+v", got)
	}

	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM bills WHERE id = ? AND due_at IS NULL`, "b_seed_no_due").
		Scan(&nullCount).Error; err != nil || nullCount != 1 {
		t.Errorf("未赋值的 due_at 必须是 NULL: err=%v n=%d（期望 1）", err, nullCount)
	}
	if err := db.Raw(`SELECT COUNT(*) FROM bills WHERE id = ? AND due_at IS NULL`, seed.ID).
		Scan(&nullCount).Error; err != nil || nullCount != 0 {
		t.Errorf("赋值过的 due_at 不该是 NULL: err=%v n=%d", err, nullCount)
	}
}

// TestBillUniqueIndexIsOnVersionRowKey AC① 的库级形态，四臂：
// ① 名为 uq_bills_quote_row 的唯一索引存在且列集合就是 quote_row_id；
// ② 同一版的第二张账单被拒，且报的是**那条约束名**（仓储据此分幂等）；
// ③ 同一 quote_id 的不同版本必须允许并存（否则还价后接受 v2 时开不出第二张应收）；
// ④ quote_id 上不许有单列唯一索引。
func TestBillUniqueIndexIsOnVersionRowKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Bill{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	var indexes []quoteIndexRow
	if err := db.Raw(`SELECT
			i.relname AS name,
			(SELECT string_agg(a.attname, '+' ORDER BY k.ord)
			   FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
			   JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum) AS cols,
			x.indisunique AS uniq, x.indisprimary AS pkey
		FROM pg_index x JOIN pg_class i ON i.oid = x.indexrelid
		WHERE x.indrelid = 'bills'::regclass`).
		Scan(&indexes).Error; err != nil {
		t.Fatalf("查 pg_index 失败: %v", err)
	}
	var rowIdx *quoteIndexRow
	for k := range indexes {
		if indexes[k].Name == "uq_bills_quote_row" {
			rowIdx = &indexes[k]
		}
		if indexes[k].Cols == "quote_id" && indexes[k].Uniq {
			t.Errorf("quote_id 上有单列唯一索引 %s：一张报价单只能开一张账单，还价后接受 v2 就没了应收", indexes[k].Name)
		}
	}
	if rowIdx == nil {
		t.Fatalf("bills 上没有名为 uq_bills_quote_row 的索引，实得 %v —— "+
			"标签写成 `unique` 也能建唯一约束，但约束名由 PG 拼，仓储认不到它就没法分\"重复派生\"与\"别的冲突\"",
			colsOf(indexes))
	}
	if rowIdx.Cols != "quote_row_id" {
		t.Errorf("uq_bills_quote_row 的列集合是 %q，期望 quote_row_id（幂等键必须是版本行键，不是逻辑号）", rowIdx.Cols)
	}
	if !rowIdx.Uniq || rowIdx.Pkey {
		t.Errorf("该索引必须唯一且不是主键，实际 uniq=%v pkey=%v", rowIdx.Uniq, rowIdx.Pkey)
	}

	insert := func(id, quoteID, rowID string) error {
		return db.Create(&model.Bill{
			ID: id, QuoteID: quoteID, QuoteRowID: rowID,
			OpportunityID: "opp_uq", Amount: 10, Status: model.BillStatusOpen,
		}).Error
	}
	if err := insert("u_v1", "QT-U", "q_u_v1"); err != nil {
		t.Fatalf("第一张账单插入失败: %v", err)
	}
	if err := insert("u_v2", "QT-U", "q_u_v2"); err != nil {
		t.Errorf("同一报价单第二版的账单被拒了（多版并存不成立）: %v", err)
	}
	err := insert("u_dup", "QT-U-OTHER", "q_u_v1")
	if err == nil {
		t.Fatal("同一版的第二张账单竟然插进去了：AC① 的库级保证失守")
	}
	if !strings.Contains(err.Error(), "23505") || !strings.Contains(err.Error(), rowIdx.Name) {
		t.Errorf("重复派生报的不是那条唯一约束（错误 %v，期望含 23505 与 %s）：仓储无法据此分幂等", err, rowIdx.Name)
	}
}

// TestBillAmountSharesRangeWithQuoteLines AC② 的量程前提：账单金额列与报价行净额同型。
func TestBillAmountSharesRangeWithQuoteLines(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Bill{}, &model.Quote{}, &model.QuoteLineItem{}); err != nil {
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
	bill := read("bills", "amount")
	line := read("quote_line_items", "amount")
	if bill != line {
		t.Errorf("bills.amount 与 quote_line_items.amount 不同型：账单 (%d,%d) vs 报价行 (%d,%d) —— "+
			"对账一致要求两边本来就是同一个量程", bill.Precision, bill.Scale, line.Precision, line.Scale)
	}
	if bill.Precision != 14 || bill.Scale != 2 {
		t.Errorf("amount 应为 numeric(14,2)，实际 (%d,%d)", bill.Precision, bill.Scale)
	}
}

// TestBillIDIsTextPrimaryKey 账单号是自生成的字符串键（它会被抄进 payments.bill_id）。
func TestBillIDIsTextPrimaryKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Bill{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	var pkCols []string
	if err := db.Raw(`SELECT a.attname FROM pg_index x
		JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = ANY(x.indkey)
		WHERE x.indrelid = 'bills'::regclass AND x.indisprimary
		ORDER BY a.attnum`).Scan(&pkCols).Error; err != nil {
		t.Fatalf("查主键失败: %v", err)
	}
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Fatalf("bills 主键列集合 %v，期望 [id]", pkCols)
	}
	var dataType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_name = 'bills' AND column_name = 'id'`).Scan(&dataType).Error; err != nil {
		t.Fatalf("查 id 列型失败: %v", err)
	}
	if dataType != "text" {
		t.Errorf("bills.id 类型 %q，期望 text（自生成行键，与 quotes.id 同型；bigserial 在多实例下会撞）", dataType)
	}
}

// testBillTime 显式时间戳：账期断言不能靠"现在"（与 testQuoteTime 同一理由 ——
// 冻结的 now 加固定偏移会让用例自带日历引信）。
func testBillTime() time.Time {
	return time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
}
