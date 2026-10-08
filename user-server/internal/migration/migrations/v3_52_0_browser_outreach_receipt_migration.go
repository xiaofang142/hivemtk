package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserOutreachReceiptMigration v3.52.0：新建 browser_outreach_receipts 表（触达 P0 Chunk 5）。
//
// 修的事实：一次触达「验收」需要三样东西留在库里——截图、帖子链接、当时发出的文案原文。
// 今天这三样散在三处且都撑不起验收：final_screenshot_url 只有用户显式编排了 screenshot 步才有、
// 帖子链接只在 command_log 与 extracted_data 的 JSON 里、文案原文只有 hash。
// 于是「这条触达到底发出去了、发的是什么、长什么样」只能靠人去翻审计包。
//
// 表形状与 model.BrowserOutreachReceipt 标签对齐；IF NOT EXISTS 幂等
// （建表事实源另有 AutoMigrate，此处只保证存量库可达）。
// 刻意不加唯一约束：回执是交付物不是闸门，重复写入是「多留一次证据」而不是「少留一次事实」。
type BrowserOutreachReceiptMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserOutreachReceiptMigration)(nil)

func NewBrowserOutreachReceiptMigration(db *gorm.DB) *BrowserOutreachReceiptMigration {
	return &BrowserOutreachReceiptMigration{db: db}
}

func (m *BrowserOutreachReceiptMigration) Version() string { return "v3.52.0" }

func (m *BrowserOutreachReceiptMigration) Name() string {
	return "browser_outreach_receipts"
}

func (m *BrowserOutreachReceiptMigration) Description() string {
	return "浏览器自动化 v3.52.0：新建 browser_outreach_receipts 表（触达回执：截图+帖子链接+文案快照）"
}

func (m *BrowserOutreachReceiptMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	return m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_outreach_receipts (
		id BIGSERIAL PRIMARY KEY,
		task_id BIGINT NOT NULL,
		session_id BIGINT NOT NULL,
		platform VARCHAR(64) NOT NULL,
		action VARCHAR(64) NOT NULL,
		target_url TEXT NOT NULL,
		copy_hash VARCHAR(64) NOT NULL,
		copy_snapshot TEXT,
		screenshot_url VARCHAR(1024),
		evidence JSONB,
		verified BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error
}

// Down 不删表：回执行是对外交付的验收证据，删表等于让已验收的触达变成「没发生过」
// （口径同 v3.39/v3.42–v3.51）。
func (m *BrowserOutreachReceiptMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "browser_outreach_receipts")
	return nil
}
