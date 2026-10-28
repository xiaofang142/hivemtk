// Package migrations 迁移集合。
package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// G0SchemaMigration v3.86.2（G0 首批施工卡 D02-D05 的纯迁移链补齐）：
//
//	G0 批次（eea1f2e2）的新表/新列原依赖启动期 AutoMigrate（RegisterExtraModels），
//	纯迁移链恢复的库（make verify-restore / 迁移链重建）缺表缺列——
//	既有门 TestReachTablesMigration_UpBuildsModelReadableTables 抓到
//	reach_delayed_outbound 缺 claim_token/claimed_at/lease_until 三列。
//	本迁移把 G0 的 5 张新表 + 延迟出站租约三列收进正式迁移链（幂等 AutoMigrate：
//	已有表只补缺列，无破坏性 DDL）。
type G0SchemaMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*G0SchemaMigration)(nil)

// NewG0SchemaMigration 构造
func NewG0SchemaMigration(db *gorm.DB) *G0SchemaMigration {
	return &G0SchemaMigration{db: db}
}

func (m *G0SchemaMigration) Version() string { return "v3.86.2" }

func (m *G0SchemaMigration) Name() string {
	return "G0 施工卡 schema：5 新表 + reach_delayed_outbound 租约三列"
}

func (m *G0SchemaMigration) Description() string {
	return "reach_deferred_outbound / contact_consent_events / contact_preferences / " +
		"reach_quota_counters / reach_quota_reservations 五表（D02/D04/D05）+ " +
		"reach_delayed_outbound 补 claim_token/claimed_at/lease_until（D03）"
}

func (m *G0SchemaMigration) Up(ctx context.Context) error {
	if err := m.db.WithContext(ctx).AutoMigrate(
		&model.ReachDeferredOutbound{},
		&model.ContactConsentEvent{},
		&model.ContactPreference{},
		&model.ReachQuotaCounter{},
		&model.ReachQuotaReservation{},
		&model.DelayedOutboundReply{}, // D03 租约三列（claim_token/claimed_at/lease_until）
	); err != nil {
		return fmt.Errorf("automigrate G0 tables: %w", err)
	}
	return nil
}

func (m *G0SchemaMigration) Down(_ context.Context) error {
	// 不可回退：五表可能已承载业务数据（触达延迟队列/同意事实账/配额账），
	// 删表＝销毁在用数据（与 v3.26.0 降级不删表同一纪律）。
	return nil
}
