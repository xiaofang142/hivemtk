// quote_migration_test.go T-P6-01 报价版本链在**真库**上的判据。
//
// 放在 internal/pkg/db 而不是 internal/model：建表事实源在这里（allModels() +
// GORM AutoMigrate），而 model 包 import 不了 testutil（testutil → internal/pkg/db → model 成环）。
//
// 为什么标签层那份不够（与商机同一句理由，但本表多一条）：
//   - `type:numeric(14,2)` 写在标签上不等于库里真建成了 numeric(14,2)；
//   - 更要命的是 `uniqueIndex:名字` —— 复合唯一索引是 AC① 的**唯一**硬保证，
//     而 GORM 对"两列各写一次同名 uniqueIndex"的处理是它自己拼一个复合索引，
//     一旦两列的名字写得差一个字符，它就建两个单列索引：**表能建、测试全绿、
//     同一版本可以插无数行**。所以必须到 pg_index 里点名列集合。
package db

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// quoteIndexRow 一条索引的判据载体：名字 + 列集合 + 两种约束性质。
//
// 提到包级是因为下面的 `colsOf` 也要它——匿名 struct 在签名里重写一遍，
// 两处字段顺序稍有差异就是一个"看着一样的类型"编译不过。
type quoteIndexRow struct {
	Name string
	Cols string
	Uniq bool
	Pkey bool
}

// quoteWantColumns / quoteLineWantColumns 卡面列清单在库里的真实列名（逐字比，多一列少一列都红）。
// 按列名升序写：读回来的是 `ORDER BY column_name`，两份顺序不一致会让"完全正确的表"
// 判成漂移，而用例的价值在于红的时候说的是缺了哪一列。
var quoteWantColumns = []string{
	"created_at", "currency", "id", "opportunity_id", "quote_id",
	"source_id", "status", "updated_at", "valid_until", "version",
}

var quoteLineWantColumns = []string{
	"amount", "created_at", "discount_percent", "line_no", "product_id",
	"quantity", "quote_row_id", "title", "unit_price",
}

func quoteRegisteredModels() []any {
	var out []any
	for _, m := range allModels() {
		switch m.(type) {
		case *model.Quote, *model.QuoteLineItem:
			out = append(out, m)
		}
	}
	return out
}

func TestQuoteRegisteredInAllModels(t *testing.T) {
	got := quoteRegisteredModels()
	if len(got) != 2 {
		t.Fatalf("quotes 与 quote_line_items 都要登记进 allModels()，实际命中 %d 个：%v", len(got), got)
	}
	// 只登记一张表是一种会发生的半吊子接法（改了 Quote 忘了行项目表），
	// 后果不是不建表那么简单：quotes 建了、明细没建 ⇒ 报价能存、行项目写不进去，
	// 而 P6-02 的合计用例连不上库时是 Skip 不是 Fail。
	var hasQuote, hasLine bool
	for _, m := range got {
		switch m.(type) {
		case *model.Quote:
			hasQuote = true
		case *model.QuoteLineItem:
			hasLine = true
		}
	}
	if !hasQuote || !hasLine {
		t.Fatalf("登记不全：quote=%v line=%v", hasQuote, hasLine)
	}
}

func quoteColumns(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()
	var cols []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns
		WHERE table_name = ? ORDER BY column_name`, table).Scan(&cols).Error; err != nil {
		t.Fatalf("查 %s 列集合失败: %v", table, err)
	}
	return cols
}

// TestQuoteAutoMigrate_Idempotent 建表可重跑：三张判据（不报错、列集合不变、存量行不丢）。
//
// 两张表一起跑：真实部署里它们是同一次 AutoMigrate 建出来的，
// 分两张表各测一次会漏掉"明细表的 quote_row_id 指向的版本行刚被重建"这种半状态。
func TestQuoteAutoMigrate_Idempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
		t.Fatalf("首次 AutoMigrate 失败: %v", err)
	}
	quoteFirst := quoteColumns(t, db, "quotes")
	if !reflect.DeepEqual(quoteFirst, quoteWantColumns) {
		t.Fatalf("quotes 列集合与卡面不符：\n  实际 %v\n  期望 %v", quoteFirst, quoteWantColumns)
	}
	lineFirst := quoteColumns(t, db, "quote_line_items")
	if !reflect.DeepEqual(lineFirst, quoteLineWantColumns) {
		t.Fatalf("quote_line_items 列集合与卡面不符：\n  实际 %v\n  期望 %v", lineFirst, quoteLineWantColumns)
	}

	until := testQuoteTime()
	seed := model.Quote{
		ID:            "q_seed_v1",
		QuoteID:       "QT-SEED",
		OpportunityID: "opp_seed",
		Version:       model.QuoteVersionFirst,
		Status:        model.QuoteStatusDraft,
		ValidUntil:    &until,
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入版本行失败: %v", err)
	}
	seedLine := model.QuoteLineItem{
		QuoteRowID: seed.ID, LineNo: 1, ProductID: "p-1", Title: "标准版席位 ×10",
		Quantity: 10, UnitPrice: 199.00, DiscountPercent: 5, Amount: 1890.50,
	}
	if err := db.Create(&seedLine).Error; err != nil {
		t.Fatalf("写入行项目失败: %v", err)
	}

	for i := 2; i <= 3; i++ {
		if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
			t.Fatalf("第 %d 次 AutoMigrate 应幂等: %v", i, err)
		}
	}
	if got := quoteColumns(t, db, "quotes"); !reflect.DeepEqual(got, quoteFirst) {
		t.Errorf("重跑迁移后 quotes 列集合变了：\n  前 %v\n  后 %v", quoteFirst, got)
	}
	if got := quoteColumns(t, db, "quote_line_items"); !reflect.DeepEqual(got, lineFirst) {
		t.Errorf("重跑迁移后明细表列集合变了：\n  前 %v\n  后 %v", lineFirst, got)
	}

	// 读回用独立零值 struct（复用 seed 会把已填字段并进 WHERE ⇒ record not found 假红）。
	var gotQuote model.Quote
	if err := db.First(&gotQuote, "id = ?", seed.ID).Error; err != nil {
		t.Fatalf("重跑迁移后版本行丢失: %v", err)
	}
	if gotQuote.Status != model.QuoteStatusDraft || gotQuote.Version != model.QuoteVersionFirst {
		t.Errorf("存量版本行被改写: %+v", gotQuote)
	}
	if gotQuote.Currency != model.QuoteCurrencyDefault {
		t.Errorf("currency 应落默认币种 %q，实际 %q（金额不带币种不可算）", model.QuoteCurrencyDefault, gotQuote.Currency)
	}
	if gotQuote.CreatedAt.IsZero() || gotQuote.UpdatedAt.IsZero() {
		t.Errorf("两个时间戳应有值（卡面 timestamps）: %+v", gotQuote)
	}

	// 未赋值的 valid_until 必须是 NULL：零值时间会被"过期报价"的聚合读成早已过期。
	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM quotes WHERE id = ? AND valid_until IS NULL`, "q_seed_v1").
		Scan(&nullCount).Error; err != nil || nullCount != 0 {
		t.Errorf("赋值过的 valid_until 不该是 NULL: err=%v n=%d", err, nullCount)
	}
	if err := db.Create(&model.Quote{
		ID: "q_seed_no_expiry", QuoteID: "QT-NOEXP", OpportunityID: "opp_seed",
		Version: model.QuoteVersionFirst, Status: model.QuoteStatusDraft,
	}).Error; err != nil {
		t.Fatalf("写入不设有效期的报价失败: %v", err)
	}
	if err := db.Raw(`SELECT COUNT(*) FROM quotes WHERE id = ? AND valid_until IS NULL`, "q_seed_no_expiry").
		Scan(&nullCount).Error; err != nil || nullCount != 1 {
		t.Errorf("未设有效期必须是 NULL（与「公元 1 年就过期」必须分得开）: err=%v n=%d", err, nullCount)
	}
	// source_id 空串是"第一版"的合法记号，不是"没赋值"⇒ 它不许是 NULL。
	var srcNull int64
	if err := db.Raw(`SELECT COUNT(*) FROM quotes WHERE id = ? AND source_id IS NULL`, "q_seed_v1").
		Scan(&srcNull).Error; err != nil || srcNull != 0 {
		t.Errorf("source_id 应是空串而不是 NULL（两种「没有来路」会让读侧各写一遍判空）: err=%v n=%d", err, srcNull)
	}
}

// TestQuoteCompositeUniqueIndexIsReallyComposite AC① 的库级判据，四臂。
//
// 这四臂缺任一臂都算没测到：
//  1. pg_index 里那一条的**列集合**是 quote_id+version 且唯一 ——
//     两列名字写差一个字符时 GORM 建的是两个单列索引，表能建、插入也照样重复；
//  2. 同一 quote_id 的不同版本共存（这是"多版本"而不是"一版一号"）；
//  3. 同一 (quote_id, version) 第二次插入被拒，且报的就是那条索引（23505 + 索引名）——
//     报错不点名索引的话，"版本重复"与"主键撞了"在 service 侧会走成两条不同处置；
//  4. 不同 quote_id 用同一个版本号必须允许 —— 反过来若被拒，说明索引建成了单列 version
//     唯一，那是"全站版本号不许重复"，第二张报价单的第二版就插不进去。
func TestQuoteCompositeUniqueIndexIsReallyComposite(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
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
		WHERE x.indrelid = 'quotes'::regclass`).
		Scan(&indexes).Error; err != nil {
		t.Fatalf("查 pg_index 失败: %v", err)
	}
	var composite *quoteIndexRow
	for k := range indexes {
		if indexes[k].Cols == "quote_id+version" {
			composite = &indexes[k]
		}
	}
	if composite == nil {
		t.Fatalf("quotes 上没有 (quote_id, version) 的复合索引，实得 %v —— "+
			"两列的 uniqueIndex 名字写不一致时 GORM 会各建一个单列索引，AC① 就没了", colsOf(indexes))
	}
	if !composite.Uniq || composite.Pkey {
		t.Errorf("复合索引必须唯一且不是主键，实际 uniq=%v pkey=%v", composite.Uniq, composite.Pkey)
	}
	// 单列 quote_id 唯一索引是这条 AC 的反面（"一版一号"），必须不存在。
	for _, i := range indexes {
		if i.Cols == "quote_id" && i.Uniq {
			t.Errorf("quote_id 上另有单列唯一索引 %s：同一报价的第二版就插不进去", i.Name)
		}
		if i.Cols == "version" && i.Uniq {
			t.Errorf("version 上另有单列唯一索引 %s：那等于全站版本号不许重复", i.Name)
		}
	}

	// ②共存 ＋ ③重复被拒。
	insert := func(id, quoteID string, version int64) error {
		return db.Create(&model.Quote{
			ID: id, QuoteID: quoteID, OpportunityID: "opp_idx",
			Version: version, Status: model.QuoteStatusDraft,
		}).Error
	}
	if err := insert("i_v1", "QT-I", 1); err != nil {
		t.Fatalf("第一版插入失败: %v", err)
	}
	if err := insert("i_v2", "QT-I", 2); err != nil {
		t.Errorf("同一报价的第二版被拒了：多版本共存(AC①)不成立: %v", err)
	}
	if err := insert("i_other_v1", "QT-J", 1); err != nil {
		t.Errorf("另一张报价复用版本号 1 被拒：索引建成了单列 version 唯一: %v", err)
	}
	err := insert("i_dup", "QT-I", 2)
	if err == nil {
		t.Fatal("重复 (quote_id, version) 竟然插进去了：AC① 的库级保证失守")
	}
	if !strings.Contains(err.Error(), "23505") || !strings.Contains(err.Error(), composite.Name) {
		t.Errorf("重复版本报的不是那条复合唯一索引（SQLSTATE/索引名 %v，期望含 23505 与 %s）：%v",
			err, composite.Name, err)
	}
}

// colsOf 把索引清单压成 "名字(列集合)" 一行一个，只进失败信息。
//
// 红因必须把**实际**索引形状打出来：本用例的失败形态是"GORM 静默建了两个单列索引"，
// 光说"没找到复合索引"不指向修法。
func colsOf(indexes []quoteIndexRow) []string {
	var out []string
	for _, i := range indexes {
		out = append(out, i.Name+"("+i.Cols+")")
	}
	return out
}

// TestQuoteLineItemPrimaryKeyIsPerVersionRow 行项目的身份 = 哪一版的第几行。
//
// 三臂：① 主键列集合真是 (quote_row_id, line_no)；② 同版本重复行号被拒；
// ③ **不同版本**用同一行号必须允许（否则"第二版的第一行"插不进去，版本链直接断在明细上）。
// ③ 是本用例的全部价值所在：若明细表建了 surrogate id 主键 + line_no 单列唯一，
// ①②都能过，而 v2 的第一行永远插不进去。
func TestQuoteLineItemPrimaryKeyIsPerVersionRow(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	var pkCols []string
	if err := db.Raw(`SELECT a.attname FROM pg_index x
		JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = ANY(x.indkey)
		WHERE x.indrelid = 'quote_line_items'::regclass AND x.indisprimary
		ORDER BY a.attnum`).Scan(&pkCols).Error; err != nil {
		t.Fatalf("查主键失败: %v", err)
	}
	// 按 attnum 排 = 按建表列序排（不是按索引内列序，那个在 indoption 里）。
	// 明细表的声明顺序是 quote_row_id 在前，故期望顺序也是。
	if len(pkCols) != 2 || pkCols[0] != "quote_row_id" || pkCols[1] != "line_no" {
		t.Errorf("明细表主键应是 (quote_row_id, line_no) 两列，实得 %v", pkCols)
	}
	add := func(rowID string, no int64) error {
		return db.Create(&model.QuoteLineItem{
			QuoteRowID: rowID, LineNo: no, Title: "行", Quantity: 1, UnitPrice: 1, Amount: 1,
		}).Error
	}
	if err := add("q_a", 1); err != nil {
		t.Fatalf("第一版第 1 行插入失败: %v", err)
	}
	if err := add("q_b", 1); err != nil {
		t.Errorf("不同版本用同一行号被拒：版本链断在明细上: %v", err)
	}
	if err := add("q_a", 1); err == nil {
		t.Fatal("同一版本的重复行号插进去了：主键没建住")
	}
}

// TestQuoteNumericColumnsAreReallyNumeric 金额与数量的真库列型（两表四列 + 整表无浮点）。
//
// 精度按 numeric_precision/numeric_scale 断，不是按 data_type=numeric 断：
// 无精度的 numeric 会带着隐式舍入规则进来，聚合侧看不出来。
func TestQuoteNumericColumnsAreReallyNumeric(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	for _, tc := range []struct {
		table, column    string
		precision, scale int
	}{
		{"quote_line_items", "quantity", 12, 2},
		{"quote_line_items", "unit_price", 14, 2},
		{"quote_line_items", "discount_percent", 5, 2},
		{"quote_line_items", "amount", 14, 2},
	} {
		var dataType string
		// numeric_precision 用 NullInt64 接：列存在但类型不是 numeric 时这一列是 NULL，
		// 直接扫进 int 会先以"converting NULL to int"报出来，把一个本该说
		// "类型不对"的红变成一个看不懂的类型错误。
		var precision, scale sql.NullInt64
		err := db.Raw(`SELECT data_type, numeric_precision, numeric_scale
			FROM information_schema.columns
			WHERE table_name = ? AND column_name = ?`, tc.table, tc.column).
			Row().Scan(&dataType, &precision, &scale)
		if err != nil {
			t.Fatalf("%s.%s 读不到: %v", tc.table, tc.column, err)
		}
		if dataType != "numeric" {
			t.Errorf("%s.%s 类型 %q，期望 numeric", tc.table, tc.column, dataType)
			continue
		}
		if !precision.Valid || !scale.Valid {
			t.Errorf("%s.%s 是 numeric 却无精度：无精度的 numeric 带隐式舍入规则，聚合侧看不出来", tc.table, tc.column)
			continue
		}
		if precision.Int64 != int64(tc.precision) || scale.Int64 != int64(tc.scale) {
			t.Errorf("%s.%s 精度 numeric(%d,%d)，期望 (%d,%d)",
				tc.table, tc.column, precision.Int64, scale.Int64, tc.precision, tc.scale)
		}
	}
	// 定点数还得能**原样读回**：numeric(14,2) 存 0.01 与存 1e16 都"类型正确"，
	// 但前者才是报价明细要的（分位不丢）。
	if err := db.Create(&model.QuoteLineItem{
		QuoteRowID: "q_num", LineNo: 1, Title: "分位", Quantity: 3,
		UnitPrice: 19999.99, DiscountPercent: 12.34, Amount: 52199.94,
	}).Error; err != nil {
		t.Fatalf("写入分位行失败: %v", err)
	}
	var back model.QuoteLineItem
	if err := db.First(&back, "quote_row_id = ? AND line_no = ?", "q_num", 1).Error; err != nil {
		t.Fatalf("读回分位行失败: %v", err)
	}
	if back.UnitPrice != 19999.99 || back.Amount != 52199.94 || back.DiscountPercent != 12.34 {
		t.Errorf("定点列读回漂了: unit=%v amount=%v disc=%v", back.UnitPrice, back.Amount, back.DiscountPercent)
	}

	var floatCols []string
	if err := db.Raw(`SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_name IN ('quotes', 'quote_line_items')
		  AND data_type IN ('double precision', 'real', 'float4', 'float8')`).
		Scan(&floatCols).Error; err != nil {
		t.Fatalf("查浮点列失败: %v", err)
	}
	if len(floatCols) > 0 {
		t.Errorf("报价两表出现浮点列 %v：金额与数量一律 numeric", floatCols)
	}
}

// TestQuoteHasNoTenantColumnAtDBLevel X3 禁区（单商户，ADR-014 已 Simplified）的真库判据。
func TestQuoteHasNoTenantColumnAtDBLevel(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Quote{}, &model.QuoteLineItem{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	tenantish := []string{"tenant", "org_id", "corp", "workspace", "account_id", "namespace", "scope"}
	for _, table := range []string{"quotes", "quote_line_items"} {
		for _, col := range quoteColumns(t, db, table) {
			for _, banned := range tenantish {
				if strings.Contains(col, banned) {
					t.Errorf("%s.%s 含租户味道的 %q：X3 禁区", table, col, banned)
				}
			}
		}
	}
}

// testQuoteTime 显式时间戳：有效期断言不能靠"现在"，
// 宿主机时区与 PG 会话时区不一致时（本仓的日期边界裂脑先例），
// 一个相对 now 的偏移会让用例只在某几个 UTC 小时里红。
func testQuoteTime() time.Time {
	return time.Date(2026, 10, 15, 2, 30, 0, 0, time.UTC)
}
