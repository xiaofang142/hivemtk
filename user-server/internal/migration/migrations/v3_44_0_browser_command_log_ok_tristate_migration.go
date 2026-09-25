package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserCommandLogOkTriStateMigration v3.44.0：审计流的 ok 收成三态（批20b / A2）。
//
// v3.38.0 建表时 ok 是 `BOOLEAN NOT NULL DEFAULT FALSE`，模型也就只能是 bool。
// 代码侧改成 *bool（nil=此帧不携带结论）后，库里两件事必须先解决：
//  1. NOT NULL —— 新代码第一次给 command 帧写 nil 就报错，整条审计链路当场断；
//  2. DEFAULT FALSE —— gorm 见「有默认值 + 零值」会把该列从 INSERT 里摘掉交给库填，
//     于是 nil 被折成 false：下发帧从「无结论」变成「结论为否」，比原来的假绿更误导。
//     （列默认与代码默认口径的一致性要求见 v3.43.0 的 confirm_wait_sec。）
//
// 再补一次存量回填：历史 command 帧的 ok 全是写入方传的字面量 true（不是观测值），
// 不回填的话监控页对每一次下发都画 ✓，而 ✓ 在那一行上没有任何含义——
// 「消灭一列混装」只对新行生效等于没消灭。回填只碰 direction='command'，
// event/judge 帧的 ok 是真观测，一个都不动。
type BrowserCommandLogOkTriStateMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserCommandLogOkTriStateMigration)(nil)

func NewBrowserCommandLogOkTriStateMigration(db *gorm.DB) *BrowserCommandLogOkTriStateMigration {
	return &BrowserCommandLogOkTriStateMigration{db: db}
}

func (m *BrowserCommandLogOkTriStateMigration) Version() string { return "v3.44.0" }

func (m *BrowserCommandLogOkTriStateMigration) Name() string {
	return "browser_command_log_ok_tristate"
}

func (m *BrowserCommandLogOkTriStateMigration) Description() string {
	return "浏览器自动化 v3.44.0：browser_command_log.ok 放开 NOT NULL 与默认值（三态：nil=该帧不携带结论），并把历史 command 帧的常量 true 回填为无结论"
}

// okNullBackfillBatch 回填单批行数：与 pruneBatchRows 同理由——首次上线时待回填行数
// 等于历史 command 帧全量，一条语句吃完就是长事务（锁表）。
const okNullBackfillBatch = 5000

func (m *BrowserCommandLogOkTriStateMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("browser_command_log") {
		return nil // 表还没建（全新库由 v3.38.0 + 模型标签负责），此处无物可改
	}
	stmts := []string{
		`ALTER TABLE browser_command_log ALTER COLUMN ok DROP DEFAULT`,
		`ALTER TABLE browser_command_log ALTER COLUMN ok DROP NOT NULL`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	for {
		res := m.db.WithContext(ctx).Exec(`UPDATE browser_command_log SET ok = NULL
			WHERE id IN (SELECT id FROM browser_command_log
				WHERE direction = 'command' AND ok IS NOT NULL ORDER BY id LIMIT ?)`,
			okNullBackfillBatch)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
}

// Down 不动可空性与存量：把 ok 改回 NOT NULL 必须先把每一行 NULL 折成一个布尔值，
// 那是给「无结论」补一个编出来的结论——降级撤销自己的变更可以，伪造审计事实不行（口径同
// v3.42/v3.43 的「降级不销毁在用对象」）。列本身仍被模型持有，故此处只声明不改。
func (m *BrowserCommandLogOkTriStateMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "browser_command_log.ok 的 NOT NULL 与默认值")
	return nil
}
