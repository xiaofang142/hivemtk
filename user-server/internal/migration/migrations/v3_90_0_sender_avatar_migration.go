package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// SenderAvatarMigration v3.90.0（M-2 客户头像链路，2026-11-11）：
// message_hub / inbox_conversations 各加可空 sender_avatar 列。
//
// 背景（2026-11-10 审计 M-2）：桥端昵称链路早已完备（sender_name 全链路落库），
// 头像零实现 —— content script 不提取、协议无字段、库表无列、管理台无处展示。
// 本迁移补齐存储半项：消息行存「这条消息发送者的头像 URL」，会话行存「客户头像」
// （随 last message upsert 按「新值优先、空值保旧」刷新，与 customer_name 同语义）。
//
// 列宽 varchar(500)：与 media_url 同款（model.MaxMediaURLLen），平台签名头像 URL
// （抖音 p3-sign…&x-expires=… 等）同样会超 500 字符，入库前由 TruncateMediaURL 收口。
//
// 无回填：头像只可能由本迁移之后的上行产生，历史行 sender_avatar 恒为空 ——
// 读侧（管理台）空头像降级为既有默认形态，不报错。
//
// 幂等可重入：ADD COLUMN IF NOT EXISTS；重复执行为空操作。
type SenderAvatarMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*SenderAvatarMigration)(nil)

// NewSenderAvatarMigration 构造
func NewSenderAvatarMigration(db *gorm.DB) *SenderAvatarMigration {
	return &SenderAvatarMigration{db: db}
}

func (m *SenderAvatarMigration) Version() string { return "v3.90.0" }

func (m *SenderAvatarMigration) Name() string { return "message_hub/inbox_conversations.sender_avatar" }

func (m *SenderAvatarMigration) Description() string {
	return "message_hub 与 inbox_conversations 各加可空 sender_avatar varchar(500) 列（M-2 客户头像链路存储半项）；无回填，历史行头像保持为空"
}

func (m *SenderAvatarMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if db.Migrator().HasTable("message_hub") &&
		!db.Migrator().HasColumn(messageHubColumnProbe{}, "sender_avatar") {
		if err := db.Exec(
			`ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS sender_avatar varchar(500)`).Error; err != nil {
			return fmt.Errorf("加 message_hub.sender_avatar 列失败: %w", err)
		}
	}
	if db.Migrator().HasTable("inbox_conversations") &&
		!db.Migrator().HasColumn(inboxConversationColumnProbe{}, "sender_avatar") {
		if err := db.Exec(
			`ALTER TABLE inbox_conversations ADD COLUMN IF NOT EXISTS sender_avatar varchar(500)`).Error; err != nil {
			return fmt.Errorf("加 inbox_conversations.sender_avatar 列失败: %w", err)
		}
	}
	return nil
}

func (m *SenderAvatarMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 两列都由模型声明（AutoMigrate 持有）：降级不销毁在用对象
	// （v3.86.0 recall 迁移同一取舍——多留可空列不破坏功能，删掉则当前进程永不补回）。
	declineColumnDrop(m.Version(), "message_hub.sender_avatar")
	declineColumnDrop(m.Version(), "inbox_conversations.sender_avatar")
	return nil
}

// inboxConversationColumnProbe 只为 Migrator().HasColumn 服务（按 TableName 判列存在性）。
type inboxConversationColumnProbe struct{}

func (inboxConversationColumnProbe) TableName() string { return "inbox_conversations" }
