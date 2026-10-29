// Package migrations 迁移集合。
package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// GeoAutopilotSingleRunMigration v3.87.0（006-geo-autopilot 审计轮）：
// geo_autopilot_runs 加部分唯一索引 (tenant_id) WHERE status='running'。
//
// 服务层的并发闸是「先查 HasRunning 再插入」两步，并发窗口内两个请求可同时通过
// 检查、各自落一条 running 行（TOCTOU）→ 双倍 LLM 成本 + 前端状态错乱。
// 数据库层约束是唯一可靠的收口；Create 把唯一冲突映射为 409 语义。
type GeoAutopilotSingleRunMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*GeoAutopilotSingleRunMigration)(nil)

// NewGeoAutopilotSingleRunMigration 构造
func NewGeoAutopilotSingleRunMigration(db *gorm.DB) *GeoAutopilotSingleRunMigration {
	return &GeoAutopilotSingleRunMigration{db: db}
}

func (m *GeoAutopilotSingleRunMigration) Version() string { return "v3.87.0" }

func (m *GeoAutopilotSingleRunMigration) Name() string {
	return "geo_autopilot_runs 单运行约束（每租户至多一条 running）"
}

func (m *GeoAutopilotSingleRunMigration) Description() string {
	return "CREATE UNIQUE INDEX uq_geo_autopilot_runs_running ON (tenant_id) WHERE status='running'" +
		"（服务层 HasRunning 两步检查存在 TOCTOU 窗口，DB 约束兜底）"
}

func (m *GeoAutopilotSingleRunMigration) Up(ctx context.Context) error {
	stmt := `CREATE UNIQUE INDEX IF NOT EXISTS uq_geo_autopilot_runs_running ` +
		`ON geo_autopilot_runs (tenant_id) WHERE status = 'running'`
	if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("创建 geo_autopilot_runs 单运行唯一索引失败: %w", err)
	}
	return nil
}

// Down 不可回退：删掉索引会重新打开并发双 running 的 TOCTOU 窗口。
func (m *GeoAutopilotSingleRunMigration) Down(_ context.Context) error {
	return nil
}
