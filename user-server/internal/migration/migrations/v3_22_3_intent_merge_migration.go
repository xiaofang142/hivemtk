package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

type IntentMergeMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*IntentMergeMigration)(nil)

func NewIntentMergeMigration(db *gorm.DB) *IntentMergeMigration {
	return &IntentMergeMigration{db: db}
}

func (m *IntentMergeMigration) Version() string { return "v3.22.3" }

func (m *IntentMergeMigration) Name() string { return "intent_records 与 intent_logs 整合" }

func (m *IntentMergeMigration) Description() string {
	return "合并 intent_records 与 intent_logs 两张意图表，统一为 intent_records"
}

func (m *IntentMergeMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	var intentRecordsExists bool
	err := m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'intent_records')`,
	).Scan(&intentRecordsExists).Error
	if err != nil {
		return fmt.Errorf("检查 intent_records 表失败: %w", err)
	}

	if !intentRecordsExists {
		return fmt.Errorf("intent_records 表不存在，无法合并")
	}

	// 统一列结构无条件幂等保证：新库（无 intent_logs）同样需要这些列，
	// 否则精细意图（IntentLog→intent_records 映射视图）写入会缺列失败。
	addColumns := map[string]string{
		"method":    `ADD COLUMN IF NOT EXISTS method varchar(16) NOT NULL DEFAULT 'llm'`,
		"reasoning": `ADD COLUMN IF NOT EXISTS reasoning text`,
		"trace_id":  `ADD COLUMN IF NOT EXISTS trace_id varchar(64)`,
		"timestamp": `ADD COLUMN IF NOT EXISTS timestamp timestamptz`,
		"source":    `ADD COLUMN IF NOT EXISTS source varchar(32) NOT NULL DEFAULT 'sales'`,
	}

	for col, def := range addColumns {
		sql := fmt.Sprintf(`ALTER TABLE intent_records %s`, def)
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("添加列 intent_records.%s 失败: %w", col, err)
		}
	}

	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_intent_records_trace_id ON intent_records (trace_id)`)
	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_intent_records_timestamp ON intent_records (timestamp)`)
	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_intent_records_source ON intent_records (source)`)
	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_intent_records_type_source ON intent_records (intent_type, source)`)

	var intentLogsExists bool
	err = m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'intent_logs')`,
	).Scan(&intentLogsExists).Error
	if err != nil {
		return fmt.Errorf("检查 intent_logs 表失败: %w", err)
	}

	if !intentLogsExists {
		return nil
	}

	var mergedCount int64
	m.db.WithContext(ctx).Model(&struct{}{}).
		Table("intent_records").
		Where("source = ?", "fine_grained").
		Count(&mergedCount)

	if mergedCount == 0 {

		insertSQL := `
			INSERT INTO intent_records (session_id, customer_id, raw_text, intent_type, intent_subtype,
				confidence, method, latency_ms, reasoning, trace_id, timestamp, created_at, source)
			SELECT
				il.session_id,
				il.customer_id,
				il.message,
				il.intent_major,
				il.intent_minor,
				il.confidence,
				il.method,
				il.latency_ms,
				COALESCE(il.reasoning, ''),
				il.trace_id,
				il.timestamp,
				il.created_at,
				'fine_grained'
			FROM intent_logs il
			WHERE NOT EXISTS (
				SELECT 1 FROM intent_records ir
				WHERE ir.session_id = il.session_id
				  AND ir.created_at = il.created_at
				  AND ir.source = 'fine_grained'
			)
		`
		result := m.db.WithContext(ctx).Exec(insertSQL)
		if result.Error != nil {
			return fmt.Errorf("合并 intent_logs 数据失败: %w", result.Error)
		}
	}

	_ = m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS intent_logs`)

	return nil
}

func (m *IntentMergeMigration) Down(ctx context.Context) error {
	return nil
}
