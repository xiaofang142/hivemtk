package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserAuditPruneCutoffSourceMigration v3.46.0：裁剪留痕补一列「这个界从哪来」（批23 / §7.28 八-3）。
//
// 修的事实：`browser_audit_prune_runs` 有 `cutoff`（界是多少）却没有 `cutoff_source`
// （界是谁定的）。于是运维把 `BROWSER_AUDIT_RETENTION_DAYS` 从 90 改成 7、等一天再改回来，
// 库里留下的读数与一次完全正常的年度裁剪**逐字节同形**——批22 立的口径「裁剪不等于证据消失」
// 只管住了「被删的那些行存在过」，管不住「按什么界删的」这件事本身不留痕。
//
// 列形状沿用 v3.43/v3.45 的「列缺省＝代码缺省」：`NOT NULL DEFAULT ”`。存量行确实没有
// 来源可写（那次扫描时代码还没有这个概念），回填成任何字符串都是伪造历史；因此
// 「空串 = 本列之前的时代」是显式语义，而**新**扫描由 PruneBefore 的前置条件保证非空
// （空源直接拒绝执行、一行不删），不靠这一列的默认值兜。
type BrowserAuditPruneCutoffSourceMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserAuditPruneCutoffSourceMigration)(nil)

func NewBrowserAuditPruneCutoffSourceMigration(db *gorm.DB) *BrowserAuditPruneCutoffSourceMigration {
	return &BrowserAuditPruneCutoffSourceMigration{db: db}
}

func (m *BrowserAuditPruneCutoffSourceMigration) Version() string { return "v3.46.0" }

func (m *BrowserAuditPruneCutoffSourceMigration) Name() string {
	return "browser_audit_prune_run_cutoff_source"
}

func (m *BrowserAuditPruneCutoffSourceMigration) Description() string {
	return "浏览器自动化 v3.46.0：browser_audit_prune_runs 增加 cutoff_source 列，让「按什么界裁剪」与「界是多少」一起留痕"
}

func (m *BrowserAuditPruneCutoffSourceMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("browser_audit_prune_runs") {
		return nil // 表还没建（全新库由模型标签负责），此处无物可改
	}
	return m.db.WithContext(ctx).Exec(`ALTER TABLE browser_audit_prune_runs
		ADD COLUMN IF NOT EXISTS cutoff_source VARCHAR(64) NOT NULL DEFAULT ''`).Error
}

// Down 不丢列：留痕表里多一列不影响老代码读取，反过来丢掉它就把「界从哪来」这个事实
// 永久删掉了（口径同 v3.39/v3.42/v3.43/v3.44/v3.45 的「降级不销毁在用列」）。
func (m *BrowserAuditPruneCutoffSourceMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "browser_audit_prune_runs.cutoff_source")
	return nil
}
