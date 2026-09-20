package migrations

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.3.0 的建表 DDL 与 gorm 模型对不上：模型字段 BuiltIn 的真实列名是 built_in
// （model/integration_template.go:27 只把 is_built_in 挂在 JSON tag 上），
// 而迁移的 CREATE TABLE / CREATE INDEX 都写 is_built_in。
// 生产建表走 AutoMigrate ⇒ CREATE TABLE IF NOT EXISTS 整条空转，索引语句却照样红，
// 且首错即中止后面的 enabled 索引。

func indexDefinition(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	var def string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = ?`, name).Scan(&def).Error; err != nil {
		t.Fatalf("索引定义查询失败 %s: %v", name, err)
	}
	return def
}

func columnType(t *testing.T, db *gorm.DB, table, column string) (string, any) {
	t.Helper()
	var dataType string
	var maxLen any
	if err := db.Raw(`SELECT data_type, character_maximum_length FROM information_schema.columns
		WHERE table_name=? AND column_name=?`, table, column).Row().Scan(&dataType, &maxLen); err != nil {
		t.Fatalf("列类型查询失败 %s.%s: %v", table, column, err)
	}
	return dataType, maxLen
}

func TestLP1Migration_Meta(t *testing.T) {
	m := NewLP1Migration(nil)
	if m.Version() != "v3.3.0" {
		t.Errorf("Version()=%q want=v3.3.0", m.Version())
	}
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
}

// TestLP1Migration_UpMatchesModelColumns 在与生产同源的 AutoMigrate 基线上跑 Up：
// 迁移不得改坏模型列，且它声称建立的索引必须真的落在模型列上。
func TestLP1Migration_UpMatchesModelColumns(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntegrationTemplate{})
	ctx := context.Background()
	m := NewLP1Migration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	// 迁移建索引的列名必须与模型一致，否则要么索引没建、要么整条迁移中止
	for _, idx := range []string{
		"idx_integration_templates_platform",
		"idx_integration_templates_category",
		"idx_integration_templates_builtin",
		"idx_integration_templates_enabled",
	} {
		def := indexDefinition(t, db, idx)
		if def == "" {
			t.Errorf("Up 后索引 %s 应存在", idx)
		}
	}
	// 存在性之外再钉一次「索引指向哪一列」：把列名改回 is_built_in 时，
	// IF NOT EXISTS 会让重建静默跳过，只查存在性抓不到这种漂移。
	if def := indexDefinition(t, db, "idx_integration_templates_builtin"); !strings.HasSuffix(def, "(built_in)") {
		t.Errorf(`idx_integration_templates_builtin 应建在模型列 built_in 上，定义=%q`, def)
	}
	// 模型列不得被迁移改坏：AutoMigrate 基线 + Up 之后仍应可正常读写
	row := model.IntegrationTemplate{Code: "v330-probe", Platform: "dingtalk", Category: "erp",
		Name: "探针", Version: "1.0.0", BuiltIn: true}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("建模板失败: %v", err)
	}
	var got model.IntegrationTemplate
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	if !got.BuiltIn {
		t.Error("BuiltIn=true 未持久化：列名口径又漂了")
	}
	db.Unscoped().Delete(&model.IntegrationTemplate{}, row.ID)

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Down 后重建 Up() 应成功: %v", err)
	}
	// 表由本迁移自己建出来时，也必须建出模型认得的列名（否则 chain-only 库上 gorm 全程报错）
	if dt, _ := columnType(t, db, "integration_templates", "built_in"); dt == "" {
		t.Error("迁移自建表时也应产出 built_in 列")
	}
}
