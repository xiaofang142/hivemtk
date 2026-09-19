// sales_event_ltc_columns_test.go T-P2-04：`sales_events` 的 LTC 预留列（R-5）。
//
// 这两列（opportunity_id / quote_id）今天**没有任何生产者**，所以本文件不测"业务读写对不对"，
// 只测三件将来一定会绊到人、而今天没人会撞上的事：
//  1. 列的形状（可空 / varchar(64) / **不带索引**）——索引是 model 注释 c) 条，改它必须是自觉动作；
//  2. 值经真库来回一次不会被悄悄改写（64 个汉字的边界尤其要试）；
//  3. NULL 与 空串 是两层历史，而经 GORM 读回时它们会塌成同一个空串 ⇒ 只能按 SQL 判。
package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// ltcOwner 给每个用例一个专属 owner：NewTestDB 用的是**包级共享**的测试库，
// 同包其它用例（sales_event_stats_test.go 那批）也会往 sales_events 里写行，
// owner 不隔离的话"恰好 1 行"这类断言会随用例执行顺序抖。
func ltcOwner(name string) string { return "ltc-col-" + name }

func setupLTCColumnTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.SalesEvent{})
}

func ltcEvent(owner string, occurredAt time.Time) *model.SalesEvent {
	return &model.SalesEvent{
		EventType:  model.SalesEventTypeOrder,
		OwnerID:    owner,
		OrderID:    "ltc-col-order",
		OccurredAt: occurredAt,
	}
}

func TestSalesEventLTCColumns_SchemaShape(t *testing.T) {
	database := setupLTCColumnTestDB(t)

	type columnShape struct {
		Column   string `gorm:"column:column_name"`
		Nullable string `gorm:"column:is_nullable"`
		DataType string `gorm:"column:data_type"`
		MaxLen   int    `gorm:"column:max_len"`
	}
	var shapes []columnShape
	// 只信 information_schema：GORM 的 varchar 标签和 PG 的 text 在 SELECT 里都装得下
	// 64 个字符，可空性与列宽这两件事在查询结果里根本分不出来。
	if err := database.Raw(`
		SELECT column_name, is_nullable, data_type,
		       COALESCE(character_maximum_length, 0) AS max_len
		FROM information_schema.columns
		WHERE table_name = 'sales_events'
		  AND column_name IN ('opportunity_id', 'quote_id')
		ORDER BY column_name`).
		Scan(&shapes).Error; err != nil {
		t.Fatalf("查 information_schema 失败：%v", err)
	}
	if len(shapes) != 2 {
		t.Fatalf("两列应当都存在，实际查到 %d 列：%+v", len(shapes), shapes)
	}
	for _, s := range shapes {
		if s.Nullable != "YES" {
			t.Errorf("%s 必须可空（AC①：不回填就不能强迫存量行有值），实际 is_nullable=%s", s.Column, s.Nullable)
		}
		if s.DataType != "character varying" {
			t.Errorf("%s 列型应为 character varying，实际 %s", s.Column, s.DataType)
		}
		if s.MaxLen != 64 {
			t.Errorf("%s 长度应为 64（与 order_id/draft_id 同规格），实际 %d", s.Column, s.MaxLen)
		}
	}
}

// TestSalesEventLTCColumns_NoIndexYet 钉住 model 注释 c) 条：今天**不建**索引。
// P4 的商机时间线真的按 opportunity_id 查了就会红 —— 那是**预期中的红**：加索引时必须
// 同时改掉 a)/c) 两条注释与本用例，别留一个"注释说没有、库里已经有了"的现场。
func TestSalesEventLTCColumns_NoIndexYet(t *testing.T) {
	database := setupLTCColumnTestDB(t)

	var indexed int64
	if err := database.Raw(`
		SELECT COUNT(*) FROM pg_indexes
		WHERE tablename = 'sales_events'
		  AND (indexdef ILIKE '%opportunity_id%' OR indexdef ILIKE '%quote_id%')`).
		Scan(&indexed).Error; err != nil {
		t.Fatalf("查 pg_indexes 失败：%v", err)
	}
	if indexed != 0 {
		t.Fatalf("sales_events 上出现了引用 LTC 预留列的索引（%d 条）⇒ 与 model 注释 c) 条冲突："+
			"要么随 P4 一并把注释改成事实，要么这就是无人查询的死索引", indexed)
	}
}

func TestSalesEventLTCColumns_RoundTripThroughRepository(t *testing.T) {
	database := setupLTCColumnTestDB(t)
	repo := NewSalesEventRepositoryWithDB(database)
	ctx := context.Background()
	owner := ltcOwner("roundtrip")

	// 64 个汉字 = 192 字节，正好卡在 varchar(64) 上：PG 数的是**字符**不是字节，
	// 这条如果被截断或报错，说明 P4 的 ID 生成规则不能按字节估长度。
	chinese64 := strings.Repeat("中", 64)
	row := ltcEvent(owner, time.Now())
	row.OpportunityID = chinese64
	row.QuoteID = "quote-88"
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("写入 64 汉字值失败：%v", err)
	}

	list, err := repo.ListByType(ctx, model.SalesEventTypeOrder, owner, 0)
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if len(list) != 1 {
		t.Fatalf("owner 过滤后应恰好 1 行，实际 %d 行", len(list))
	}
	got := list[0]
	if got.QuoteID != "quote-88" {
		t.Errorf("quote_id 读回不一致：写入 %q 实际 %q", "quote-88", got.QuoteID)
	}
	if n := len([]rune(got.OpportunityID)); n != 64 || got.OpportunityID != chinese64 {
		t.Errorf("opportunity_id 应原样存回 64 个汉字，实际 %d 个字符（字节 %d）", n, len(got.OpportunityID))
	}
}

// TestSalesEventLTCColumns_NullVsEmptyStringAreTwoLayers 是 b) 条口径的实跑版：
// 迁移前形态的行（绕开 GORM、完全不提及这两列）读回是 **NULL**，新行经 GORM 写入是 **空串**。
// 两者业务含义不同（"事件发生在有商机概念之前" vs "这次事件没有商机"），
// 而 GORM 把它们都摊平成 Go 的空串 ⇒ 想区分只能在 SQL 里判。
func TestSalesEventLTCColumns_NullVsEmptyStringAreTwoLayers(t *testing.T) {
	database := setupLTCColumnTestDB(t)
	repo := NewSalesEventRepositoryWithDB(database)
	ctx := context.Background()
	owner := ltcOwner("layers")
	occurredAt := time.Now()

	legacyOrderID := "ltc-col-legacy"
	freshOrderID := "ltc-col-fresh"

	if err := database.Exec(`
		INSERT INTO sales_events (event_type, owner_id, order_id, occurred_at, created_at)
		VALUES (?, ?, ?, ?, now())`,
		model.SalesEventTypeOrder, owner, legacyOrderID, occurredAt).Error; err != nil {
		t.Fatalf("插入存量样例行失败：%v", err)
	}
	fresh := ltcEvent(owner, occurredAt)
	fresh.OrderID = freshOrderID
	if err := repo.Create(ctx, fresh); err != nil {
		t.Fatalf("GORM 写入新行失败：%v", err)
	}

	type layer struct {
		OrderID string `gorm:"column:order_id"`
		IsNULL  bool   `gorm:"column:is_null"`
		IsEmpty bool   `gorm:"column:is_empty"`
	}
	var rows []layer
	if err := database.Raw(`
		SELECT order_id, opportunity_id IS NULL AS is_null, opportunity_id = '' AS is_empty
		FROM sales_events WHERE owner_id = ? ORDER BY order_id`, owner).
		Scan(&rows).Error; err != nil {
		t.Fatalf("分层查询失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应有两行（一行 NULL、一行空串），实际 %d 行：%+v", len(rows), rows)
	}
	byOrder := map[string]layer{}
	for _, r := range rows {
		byOrder[r.OrderID] = r
	}
	if legacy := byOrder[legacyOrderID]; !legacy.IsNULL || legacy.IsEmpty {
		t.Errorf("迁移前形态的行应为 opportunity_id IS NULL，实际 is_null=%v is_empty=%v",
			legacy.IsNULL, legacy.IsEmpty)
	}
	if fr := byOrder[freshOrderID]; fr.IsNULL || !fr.IsEmpty {
		t.Errorf("经 GORM 写入的新行应为 opportunity_id = ''（string 零值），实际 is_null=%v is_empty=%v",
			fr.IsNULL, fr.IsEmpty)
	}

	// 同一条数据经 GORM 读回来，两种形态就分不出了 —— 这不是 bug，是 string 型字段的
	// 必然结果；写在这里是给"顺手加个 IS NULL 过滤"的人第一次看到它的地方。
	list, err := repo.ListByType(ctx, model.SalesEventTypeOrder, owner, 0)
	if err != nil {
		t.Fatalf("GORM 读回失败：%v", err)
	}
	if len(list) != 2 {
		t.Fatalf("GORM 应能读到两行，实际 %d 行", len(list))
	}
	for _, r := range list {
		if r.OpportunityID != "" {
			t.Errorf("GORM 把 NULL 与 '' 都读成空串是预期的，实际 %s 读到 %q", r.OrderID, r.OpportunityID)
		}
	}
}
