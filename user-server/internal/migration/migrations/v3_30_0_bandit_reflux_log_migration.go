package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BanditRefluxLogMigration D04 配套：bandit_reflux_log 表
//
// 背景：BanditAllocator.UpdateReward 生产零调用——转化信号（feedback_events.reward）
// 落库后从不回流 bandit_arms，臂的后验不更新（自学习三重断链之一）。
// 回流 worker（service/feedback_loop/bandit_reward_reflux.go）需要两个防重复入账保证：
//  1. event 级：唯一索引 (event_id)——worker 重启/重扫同窗口时同事件只入账一次；
//  2. conversion 级：唯一索引 (session_id, sop_id, signal_key)——同会话同 SOP
//     重复成交去重（决策 D04"转化去重"防线，跨进程安全）。
type BanditRefluxLogMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BanditRefluxLogMigration)(nil)

func NewBanditRefluxLogMigration(db *gorm.DB) *BanditRefluxLogMigration {
	return &BanditRefluxLogMigration{db: db}
}

func (m *BanditRefluxLogMigration) Version() string { return "v3.30.0" }

func (m *BanditRefluxLogMigration) Name() string {
	return "bandit_reflux_log 表（奖励回流防重复入账）"
}

func (m *BanditRefluxLogMigration) Description() string {
	return "D04: 新建 bandit_reflux_log（event_id 唯一 + session/sop/signal 转化去重唯一索引）"
}

func (m *BanditRefluxLogMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS bandit_reflux_log (
		id BIGSERIAL PRIMARY KEY,
		event_id VARCHAR(64) NOT NULL,
		experiment_id VARCHAR(64) NOT NULL DEFAULT '',
		arm_key VARCHAR(100) NOT NULL DEFAULT '',
		signal_key VARCHAR(50) NOT NULL DEFAULT '',
		session_id VARCHAR(120) NOT NULL DEFAULT '',
		sop_id BIGINT NOT NULL DEFAULT 0,
		reward DECIMAL(6,3) NOT NULL DEFAULT 0,
		success BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_reflux_event UNIQUE (event_id),
		CONSTRAINT uk_reflux_conversion UNIQUE (session_id, sop_id, signal_key)
	)`).Error; err != nil {
		return err
	}
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_reflux_log_experiment ON bandit_reflux_log(experiment_id, created_at)`).Error
	return nil
}

func (m *BanditRefluxLogMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "bandit_reflux_log")
	return nil
}
