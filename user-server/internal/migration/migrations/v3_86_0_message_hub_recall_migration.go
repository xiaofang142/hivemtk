package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// MessageHubRecallMigration v3.86.0（B2 撤回状态格，2026-10-26）：
// message_hub 加 recalled_at / recall_error 两列，撤回从"平台侧做完了、
// 本地什么都没记"变成可反查、可幂等。
//
// 背景（OPEN_ITEMS B2）：平台撤回通道（TG deleteMessage / 飞书 DELETE messages /
// 企微 recall）在 integration_reach_adapter.go 早就接通了，缺的是回执落点 ——
// 一次撤回成功之后，hub 行上看不出"这条撤过"，同一行再点一次就会**再打一遍平台 API**。
// 平台对重复撤回的回应并不一致（TG 报 message to delete not found、飞书报已撤回、
// 企微报消息不存在），所以幂等必须落在本地：recalled_at 非空即终态，不再出网。
//
// 为什么不在 status 上加 'recalled'：status 是发送状态机（pending→sent/delivered/
// send_failed），撤回是"发出去之后有没有撤掉"的另一个维度。混进一列会把投递事实
// 抹掉，也会让 UpdateDeliveryStatus 的乱序回执保护（send_failed 不回翻）失效。
//
// 无回填：撤回事实只可能由本迁移之后的请求产生，历史行 recalled_at 恒为 NULL
// 才是真话——"不知道撤没撤过"不许写成"没撤过"以外的任何值。
//
// 幂等可重入：ADD COLUMN IF NOT EXISTS；重复执行为空操作。
type MessageHubRecallMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*MessageHubRecallMigration)(nil)

// NewMessageHubRecallMigration 构造
func NewMessageHubRecallMigration(db *gorm.DB) *MessageHubRecallMigration {
	return &MessageHubRecallMigration{db: db}
}

func (m *MessageHubRecallMigration) Version() string { return "v3.86.0" }

func (m *MessageHubRecallMigration) Name() string { return "message_hub 撤回状态格" }

func (m *MessageHubRecallMigration) Description() string {
	return "message_hub 加 recalled_at（撤回成功的终态时刻，非空即幂等）与 " +
		"recall_error（最近一次失败原因）两列；无回填，历史行的撤回状态保持未知"
}

func (m *MessageHubRecallMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable("message_hub") {
		return nil // 全新部署：AutoMigrate 建表时模型字段已带这两列
	}

	if !db.Migrator().HasColumn(messageHubColumnProbe{}, "recalled_at") {
		if err := db.Exec(
			`ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS recalled_at timestamptz`).Error; err != nil {
			return fmt.Errorf("加 recalled_at 列失败: %w", err)
		}
	}
	if !db.Migrator().HasColumn(messageHubColumnProbe{}, "recall_error") {
		// NOT NULL DEFAULT ''：GORM 写零值行时不会给 NULL，读侧按 = '' 判"没有失败原因"
		// 比 IS NULL 少一层分支；存量行由 DEFAULT 一次补齐。
		if err := db.Exec(
			`ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS recall_error varchar(200) NOT NULL DEFAULT ''`).Error; err != nil {
			return fmt.Errorf("加 recall_error 列失败: %w", err)
		}
	}
	return nil
}

func (m *MessageHubRecallMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 两列都由模型声明（AutoMigrate 持有）：降级不销毁在用对象
	// （down_drop_policy.go 同一取舍——多留两个可空/带默认值的列不破坏功能，
	// 删掉则当前进程永不补回，而撤回事实一旦丢了就再也查不回来）。
	declineColumnDrop(m.Version(), "message_hub.recalled_at")
	declineColumnDrop(m.Version(), "message_hub.recall_error")
	return nil
}
