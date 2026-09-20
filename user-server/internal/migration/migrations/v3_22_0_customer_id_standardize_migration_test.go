package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.22.0 从 information_schema.columns 扫列时把 character_maximum_length 扫进非空 int，
// 而 text 型 customer_id 该字段为 NULL ⇒ Scan 直接报错，迁移在第一条 ALTER 之前整体中止，
// 于是「统一 varchar(64) 消除 JOIN 隐式转换」这件事从来没做过。

type v322TextRow struct {
	ID         uint   `gorm:"primaryKey"`
	CustomerID string // 无 size ⇒ gorm 建为 text，character_maximum_length 为 NULL
}

func (v322TextRow) TableName() string { return "v322_text_rows" }

type v322VarcharRow struct {
	ID         uint   `gorm:"primaryKey"`
	CustomerID string `gorm:"type:varchar(64)"`
}

func (v322VarcharRow) TableName() string { return "v322_varchar_rows" }

func columnComment(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var comment string
	if err := db.Raw(`SELECT COALESCE(col_description(c.oid, a.attnum), '')
		FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
		WHERE c.relname = ? AND a.attname = 'customer_id'`, table).
		Scan(&comment).Error; err != nil {
		t.Fatalf("列注释查询失败 %s: %v", table, err)
	}
	return comment
}

func TestCustomerIDStandardizeMigration_Meta(t *testing.T) {
	m := NewCustomerIDStandardizeMigration(nil)
	if m.Version() != "v3.22.0" {
		t.Errorf("Version()=%q want=v3.22.0", m.Version())
	}
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err != nil {
		t.Errorf("Down() 应可空操作: %v", err)
	}
}

func TestCustomerIDStandardizeMigration_UpHandlesNullMaxLength(t *testing.T) {
	db := testutil.NewTestDB(t, &v322TextRow{}, &v322VarcharRow{})
	ctx := context.Background()

	// 预置条件：迁移要处理的正是这种「text 列，maxlen 为 NULL」的形状
	if dt, ml := columnType(t, db, "v322_text_rows", "customer_id"); dt != "text" || ml != nil {
		t.Fatalf("预置不成立：v322_text_rows.customer_id 应为 text/NULL，got %v/%v", dt, ml)
	}

	m := NewCustomerIDStandardizeMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	// 断言只针对「单次 Up 的结果」：把幂等复跑放在断言之后，否则第二次 Up 会把
	// 「ALTER 后没写注释」这类分支缺陷用已收敛的列补出来，变异就杀不掉（实测踩过）。
	for _, tbl := range []string{"v322_text_rows", "v322_varchar_rows"} {
		if dt, ml := columnType(t, db, tbl, "customer_id"); dt != "character varying" || ml != int64(64) {
			t.Errorf("%s.customer_id 应收敛为 varchar(64)，got %v/%v", tbl, dt, ml)
		}
		// 收敛过的列都要带上口径说明：改类型不带注释，下一个人只会看到又一个 varchar(64)
		if got := columnComment(t, db, tbl); got == "" {
			t.Errorf("%s.customer_id 应带统一口径注释", tbl)
		}
	}

	// 迁移只应作用于 customer_id，不得顺手改坏同表其他列
	if dt, _ := columnType(t, db, "v322_text_rows", "id"); dt == "" {
		t.Error("id 列不应被迁移改动")
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}
	for _, tbl := range []string{"v322_text_rows", "v322_varchar_rows"} {
		if dt, ml := columnType(t, db, tbl, "customer_id"); dt != "character varying" || ml != int64(64) {
			t.Errorf("二次 Up 后 %s.customer_id 应仍为 varchar(64)，got %v/%v", tbl, dt, ml)
		}
	}
}
