package migrations

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.47.0 改的是**索引形状**，不是列：AutoMigrate 会按新版模型标签直接建出复合唯一索引，
// 所以用例必须先手动把 schema 还原成迁移前的单列唯一索引（等于在本用例里重建"存量库"），
// 否则 Up 什么都没做也能全绿——而这里要证的正是"存量库升上来才拿得到跨 group 同名 key"。

func idxGroupKeyDef(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var def string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes
		WHERE tablename='config_params' AND indexname='idx_group_key'`).Row().Scan(&def); err != nil {
		t.Fatalf("读 idx_group_key 失败: %v", err)
	}
	return def
}

func TestConfigParamGroupKeyUniqueMigration_Meta(t *testing.T) {
	m := NewConfigParamGroupKeyUniqueMigration(nil)
	if m.Version() != "v3.47.0" {
		t.Errorf("Version()=%q want=v3.47.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestConfigParamGroupKeyUniqueMigration_NilDB(t *testing.T) {
	m := NewConfigParamGroupKeyUniqueMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestConfigParamGroupKeyUniqueMigration_UpAndShape(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	if db == nil {
		t.Fatal("测试库不可达：迁移往返无法判定（不 Skip，跳过等于没锁）")
	}
	ctx := context.Background()
	m := NewConfigParamGroupKeyUniqueMigration(db)

	// 预置迁移前 schema：单列 (key) 唯一索引 + 一行存量数据。
	if err := db.Exec(`DROP INDEX IF EXISTS idx_group_key`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_group_key ON config_params (key)`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败: %v", err)
	}
	if strings.Contains(idxGroupKeyDef(t, db), "param_group") {
		t.Fatal("预置没生效：本用例测的是存量库（单列唯一索引），不是新版 schema")
	}
	seed := func(group, key string) error {
		return db.Exec(`INSERT INTO config_params
			(param_group, key, name, value_type, param_value, default_value)
			VALUES (?, ?, 't', 'int', '1', '1')`, group, key).Error
	}
	if err := seed("bridge", "max_attempts"); err != nil {
		t.Fatalf("存量种子失败: %v", err)
	}
	// 迁移前的库确实拦得住跨 group 同名 key —— 这一句同时是"预置生效"的正证据。
	if err := seed("reach", "max_attempts"); err == nil {
		t.Fatal("预置的单列唯一索引没拦住跨 group 同名 key，本用例测的不是被修的那件事")
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	def := idxGroupKeyDef(t, db)
	if !strings.Contains(def, "(param_group, key)") {
		t.Errorf(`idx_group_key 应是 (param_group, key) 复合唯一索引, got=%s`, def)
	}
	if !strings.Contains(def, "UNIQUE") {
		t.Errorf("idx_group_key 必须仍是唯一索引（复合不等于普通索引）, got=%s", def)
	}

	// 升上来的库必须放得下跨 group 同名 key：这正是本迁移要买到的行为。
	if err := seed("reach", "max_attempts"); err != nil {
		t.Errorf("Up 后仍写不进跨 group 同名 key: %v", err)
	}
	// 但同 group 同名 key 依旧必须拦：复合唯一不等于放弃唯一。
	if err := seed("bridge", "max_attempts"); err == nil {
		t.Error("Up 后同 group 同 key 的第二行写进来了 —— 唯一约束被放宽过头")
	}

	// Down 是收紧约束：库里已经有跨 group 同名 key，PG 必须报错而不是"静默删行让它成功"。
	if err := m.Down(ctx); err == nil {
		t.Error("存在冲突数据时 Down 应报错（降级不该靠删参数行来兑现）")
	}
	// Down 会先 DROP 再 CREATE，失败时索引已经被摘掉；这里补一次 Up 把 schema 还回去，
	// 否则同包后面的用例拿到的是"无唯一约束"的库。
	if err := m.Up(ctx); err != nil {
		t.Fatalf("收尾还原 schema 失败: %v", err)
	}
	if !strings.Contains(idxGroupKeyDef(t, db), "(param_group, key)") {
		t.Errorf("收尾后 idx_group_key 应回到复合唯一形状, got=%s", idxGroupKeyDef(t, db))
	}

	var _ migration.Migration = m
}
