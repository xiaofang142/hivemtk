package migrations

import (
	"context"
	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// WecomWebhookFieldsMigration Phase 1: 为 wecom_accounts 表添加 webhook 接收和 智能体字段
// 独立部署：单租户，无 merchant_id
type WecomWebhookFieldsMigration struct {
	db *gorm.DB
}

// NewWecomWebhookFieldsMigration 创建迁移实例
func NewWecomWebhookFieldsMigration(db *gorm.DB) *WecomWebhookFieldsMigration {
	return &WecomWebhookFieldsMigration{db: db}
}

// Version 返回版本号
func (m *WecomWebhookFieldsMigration) Version() string {
	return "v2.1.1"
}

// Name 返回迁移名称
func (m *WecomWebhookFieldsMigration) Name() string {
	return "企业微信 webhook + 智能体字段"
}

// Description 返回迁移描述
func (m *WecomWebhookFieldsMigration) Description() string {
	return "为 wecom_accounts 表添加 callback_token、encoding_aes_key、webhook_enabled、webhook_path、ai_agent_enabled 字段，打通企微用户全链路入站+AI 自动回复闭环"
}

// Up 执行迁移
func (m *WecomWebhookFieldsMigration) Up(ctx context.Context) error {
	if !m.db.Migrator().HasTable("wecom_accounts") {
		return nil
	}
	if !m.db.Migrator().HasColumn(&struct {
		CallbackToken  string `gorm:"type:varchar(100)"`
		EncodingAESKey string `gorm:"type:varchar(200)"`
		WebhookEnabled bool   `gorm:"default:false"`
		WebhookPath    string `gorm:"type:varchar(200)"`
		AIAgentEnabled bool   `gorm:"default:false"`
	}{}, "CallbackToken") {
		if err := m.db.Exec("ALTER TABLE wecom_accounts ADD COLUMN IF NOT EXISTS callback_token VARCHAR(100)").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasColumn(&struct {
		EncodingAESKey string `gorm:"type:varchar(200)"`
	}{}, "EncodingAESKey") {
		if err := m.db.Exec("ALTER TABLE wecom_accounts ADD COLUMN IF NOT EXISTS encoding_aes_key VARCHAR(200)").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasColumn(&struct {
		WebhookEnabled bool `gorm:"default:false"`
	}{}, "WebhookEnabled") {
		if err := m.db.Exec("ALTER TABLE wecom_accounts ADD COLUMN IF NOT EXISTS webhook_enabled BOOLEAN DEFAULT FALSE").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasColumn(&struct {
		WebhookPath string `gorm:"type:varchar(200)"`
	}{}, "WebhookPath") {
		if err := m.db.Exec("ALTER TABLE wecom_accounts ADD COLUMN IF NOT EXISTS webhook_path VARCHAR(200)").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasColumn(&struct {
		AIAgentEnabled bool `gorm:"default:false"`
	}{}, "AIAgentEnabled") {
		if err := m.db.Exec("ALTER TABLE wecom_accounts ADD COLUMN IF NOT EXISTS ai_agent_enabled BOOLEAN DEFAULT FALSE").Error; err != nil {
			return err
		}
	}
	return nil
}

// Down 回滚
func (m *WecomWebhookFieldsMigration) Down(ctx context.Context) error {
	if !m.db.Migrator().HasTable("wecom_accounts") {
		return nil
	}
	declineColumnDrop(m.Version(), "wecom_accounts.ai_agent_enabled", "wecom_accounts.webhook_path",
		"wecom_accounts.webhook_enabled", "wecom_accounts.encoding_aes_key", "wecom_accounts.callback_token")
	return nil
}

var _ migration.Migration = (*WecomWebhookFieldsMigration)(nil)
