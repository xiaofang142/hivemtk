package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// TelegramGroupGateMigration v3.35.0：TG 群组入群管控（方案 A 入群申请审批 / 方案 B 禁言解锁）
type TelegramGroupGateMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*TelegramGroupGateMigration)(nil)

func NewTelegramGroupGateMigration(db *gorm.DB) *TelegramGroupGateMigration {
	return &TelegramGroupGateMigration{db: db}
}

func (m *TelegramGroupGateMigration) Version() string { return "v3.35.0" }

func (m *TelegramGroupGateMigration) Name() string {
	return "telegram_group_gates / telegram_group_members（TG 入群管控）"
}

func (m *TelegramGroupGateMigration) Description() string {
	return "方案A: 申请审批+私聊激活放行; 方案B: 进群禁言+私聊解锁; 含成员验证台账"
}

func (m *TelegramGroupGateMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS telegram_group_gates (
		id BIGSERIAL PRIMARY KEY,
		account_id BIGINT NOT NULL,
		chat_id VARCHAR(64) NOT NULL,
		chat_title VARCHAR(255) DEFAULT '',
		mode VARCHAR(32) NOT NULL DEFAULT 'mute_unlock',
		enabled BOOLEAN NOT NULL DEFAULT FALSE,
		verify_ttl_min INT NOT NULL DEFAULT 10,
		welcome_msg TEXT DEFAULT '',
		verify_msg TEXT DEFAULT '',
		ai_agent_enabled BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_tg_gate_account_chat UNIQUE (account_id, chat_id)
	)`).Error; err != nil {
		return err
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS telegram_group_members (
		id BIGSERIAL PRIMARY KEY,
		account_id BIGINT NOT NULL,
		chat_id VARCHAR(64) NOT NULL,
		user_id VARCHAR(64) NOT NULL,
		username VARCHAR(128) DEFAULT '',
		full_name VARCHAR(128) DEFAULT '',
		join_status VARCHAR(32) NOT NULL DEFAULT 'pending',
		join_mode VARCHAR(32) DEFAULT '',
		authorized BOOLEAN NOT NULL DEFAULT FALSE,
		authorized_at TIMESTAMP,
		verify_token VARCHAR(64) DEFAULT '',
		expires_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_tg_gate_member UNIQUE (account_id, chat_id, user_id)
	)`).Error; err != nil {
		return err
	}
	if err := m.db.WithContext(ctx).Exec(
		`CREATE INDEX IF NOT EXISTS idx_tg_gate_members_status ON telegram_group_members(join_status)`).Error; err != nil {
		return err
	}
	return m.db.WithContext(ctx).Exec(
		`CREATE INDEX IF NOT EXISTS idx_tg_gate_members_token ON telegram_group_members(verify_token)`).Error
}

func (m *TelegramGroupGateMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS telegram_group_members`).Error; err != nil {
		return err
	}
	return m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS telegram_group_gates`).Error
}
