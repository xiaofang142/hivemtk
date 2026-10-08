package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserOutreachDedupeMigration v3.50.0：新建 browser_outreach_dedupe 表（触达 P0 Chunk 3）。
//
// 修的事实：write_claims 的去重键是任务内 (task_id, text_hash)——同一个用户在不同任务里
// 对同一篇笔记发同一条评论，两条腿彼此看不见，于是重复打扰发生。跨任务触达去重必须落库
// （Executor 进程级单例禁内存计数），四元组 (platform, target_url, action, copy_hash) 唯一。
//
// 表形状与 model.BrowserOutreachDedupe 标签对齐；IF NOT EXISTS 幂等
// （建表事实源另有 AutoMigrate，此处只保证存量库可达）。
type BrowserOutreachDedupeMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserOutreachDedupeMigration)(nil)

func NewBrowserOutreachDedupeMigration(db *gorm.DB) *BrowserOutreachDedupeMigration {
	return &BrowserOutreachDedupeMigration{db: db}
}

func (m *BrowserOutreachDedupeMigration) Version() string { return "v3.50.0" }

func (m *BrowserOutreachDedupeMigration) Name() string {
	return "browser_outreach_dedupe"
}

func (m *BrowserOutreachDedupeMigration) Description() string {
	return "浏览器自动化 v3.50.0：新建 browser_outreach_dedupe 表（跨任务触达去重四元组唯一键）"
}

func (m *BrowserOutreachDedupeMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	return m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_outreach_dedupe (
		id BIGSERIAL PRIMARY KEY,
		platform VARCHAR(64) NOT NULL,
		target_url TEXT NOT NULL,
		action VARCHAR(64) NOT NULL,
		copy_hash VARCHAR(64) NOT NULL,
		task_id BIGINT NOT NULL,
		session_id BIGINT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT uk_outreach_dedupe UNIQUE (platform, target_url, action, copy_hash)
	)`).Error
}

// Down 不删表：去重行是「已触达」这一事实的唯一持久证据，
// 删表等于让所有历史触达重新变成可打扰对象（口径同 v3.39/v3.42–v3.49）。
func (m *BrowserOutreachDedupeMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "browser_outreach_dedupe")
	return nil
}
