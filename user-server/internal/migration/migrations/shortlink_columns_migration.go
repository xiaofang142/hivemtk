package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ShortLinkColumnsMigration short_links 字段补齐迁移
type ShortLinkColumnsMigration struct {
	db *gorm.DB
}

func NewShortLinkColumnsMigration(db *gorm.DB) *ShortLinkColumnsMigration {
	return &ShortLinkColumnsMigration{db: db}
}

func (m *ShortLinkColumnsMigration) Version() string { return "v2.11.1" }

func (m *ShortLinkColumnsMigration) Name() string {
	return "short_links 字段补齐 (title/description/domain_id/password/expire_time)"
}

func (m *ShortLinkColumnsMigration) Description() string {
	return "补齐 short_links 缺失列，修复短链创建 500（column title does not exist）"
}

func (m *ShortLinkColumnsMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("short_links") {
		return nil
	}

	stmts := []string{
		// 幂等三态（2026-11-12 修）：目标列不存在且源列存在才 RENAME；
		// 两列并存（AutoMigrate 已按模型补了 expire_time、bootstrap 旧库还留着
		// expire_at）时不再 RENAME——那会 42701「column expire_time already exists」，
		// 让整条迁移链在此卡死、后续迁移全部不执行（本地 8401 实测 task 32~35 连环失败）。
		// 并存态改为把 expire_at 的历史值合并进 expire_time（expire_time 空行才回填），
		// expire_at 列保留不销毁（与 v3.92.0 filename 同一取舍：遗留列留而不删）。
		`DO $$ BEGIN
      IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='short_links' AND column_name='expire_at')
         AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='short_links' AND column_name='expire_time') THEN
        ALTER TABLE short_links RENAME COLUMN expire_at TO expire_time;
      END IF;
    END $$;`,
		`DO $$ BEGIN
      IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='short_links' AND column_name='expire_at')
         AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='short_links' AND column_name='expire_time') THEN
        UPDATE short_links SET expire_time = expire_at WHERE expire_time IS NULL AND expire_at IS NOT NULL;
      END IF;
    END $$;`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS expire_time TIMESTAMP`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS title VARCHAR(255)`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS description TEXT`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS domain_id BIGINT`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS password VARCHAR(255)`,
		`ALTER TABLE short_links ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,
	}
	if err := execAllShortLink(ctx, m.db, stmts); err != nil {
		return fmt.Errorf("补齐 short_links 字段失败: %w", err)
	}
	return nil
}

// Down 执行降级（不删除列，避免误删数据；如需回滚请手动处理）
func (m *ShortLinkColumnsMigration) Down(ctx context.Context) error {
	return nil
}

func execAllShortLink(ctx context.Context, db *gorm.DB, stmts []string) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	for _, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("exec failed (%s): %w", sql, err)
		}
	}
	return nil
}

var _ migration.Migration = (*ShortLinkColumnsMigration)(nil)
