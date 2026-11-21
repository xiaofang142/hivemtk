package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
)

// Owner-token migration coverage is isolated to the explicitly authorized synthetic PostgreSQL.
func TestKnowledgeReindexOwnerMigration(t *testing.T) {
	dsn := os.Getenv("FA12_PG_DSN")
	if dsn == "" {
		t.Skip("FA12_PG_DSN 未设置：仅使用明确授权的合成 PostgreSQL")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Host != "/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x" || cfg.Port != 55412 || cfg.User != "fa12_runner" || cfg.Database != "fa12_synthetic" {
		t.Fatal("拒绝连接非授权合成 PostgreSQL")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	basePool, err := base.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = basePool.Close() })
	schema := fmt.Sprintf("fa13_owner_migration_%d", time.Now().UnixNano())
	if err := base.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Errorf("清理本测试 schema 失败: %v", err)
		}
	})
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.KnowledgeDocument{}); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn("knowledge_documents", "reindex_owner_token") {
		if err := db.Migrator().DropColumn("knowledge_documents", "reindex_owner_token"); err != nil {
			t.Fatal(err)
		}
	}
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.101.0")
	if !ok {
		t.Fatal("注册迁移中缺少 v3.101.0 knowledge document owner token")
	}
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatalf("owner token 迁移首次执行失败: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("owner token 迁移幂等重入失败: %v", err)
	}
	if !db.Migrator().HasColumn("knowledge_documents", "reindex_owner_token") {
		t.Fatal("迁移后缺少 reindex_owner_token 列")
	}
	if err := db.Exec(`INSERT INTO knowledge_documents (tenant_id, product_id, title, reindex_owner_token) VALUES (413, 'owner-migration-test', 'owner', 'generation-a')`).Error; err != nil {
		t.Fatalf("写入 owner token 兼容数据失败: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down 应保留 owner token 且不报错: %v", err)
	}
	if !db.Migrator().HasColumn("knowledge_documents", "reindex_owner_token") {
		t.Fatal("Down 不得删除处理中 owner 信息")
	}
}
