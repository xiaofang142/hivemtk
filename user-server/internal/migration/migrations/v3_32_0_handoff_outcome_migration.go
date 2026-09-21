package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// HandoffOutcomeMigration D20：会话 outcome 追踪列（借鉴 Chatwoot ConversationOutcomeTracker）
//
// handoff_at/handoff_reason/first_human_reply_at 三字段构成转人工 episode 的最小闭环：
// 解决率 = 有 handoff_at 且 first_human_reply_at-handoff_at 后会话完结的比例，
// 直接喂 13 自学习与运营看板。
type HandoffOutcomeMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*HandoffOutcomeMigration)(nil)

func NewHandoffOutcomeMigration(db *gorm.DB) *HandoffOutcomeMigration {
	return &HandoffOutcomeMigration{db: db}
}

func (m *HandoffOutcomeMigration) Version() string { return "v3.32.0" }

func (m *HandoffOutcomeMigration) Name() string {
	return "customer_sessions 补 outcome 追踪列（handoff 三时间戳）"
}

func (m *HandoffOutcomeMigration) Description() string {
	return "D20: handoff_at/handoff_reason/first_human_reply_at — 转人工 episode 追踪与解决率度量"
}

func (m *HandoffOutcomeMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE customer_sessions ADD COLUMN IF NOT EXISTS handoff_at TIMESTAMP`,
		`ALTER TABLE customer_sessions ADD COLUMN IF NOT EXISTS handoff_reason VARCHAR(200)`,
		`ALTER TABLE customer_sessions ADD COLUMN IF NOT EXISTS first_human_reply_at TIMESTAMP`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_handoff_at ON customer_sessions(handoff_at)`,
	}
	for _, q := range stmts {
		if err := m.db.WithContext(ctx).Exec(q).Error; err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return nil
}

func (m *HandoffOutcomeMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineIndexDrop(m.Version(), "idx_sessions_handoff_at")
	declineColumnDrop(m.Version(), "customer_sessions.first_human_reply_at",
		"customer_sessions.handoff_reason", "customer_sessions.handoff_at")
	return nil
}
