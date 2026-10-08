package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ConfigParamGroupKeyUniqueMigration v3.47.0：把 config_params 的唯一键从「单列 key」
// 改成设计本意的「(param_group, key) 复合」。
//
// 修的事实：模型里 Group 与 Key 的语义是「group + key 联合唯一」，读取侧
// （ConfigParamRepository 的 GetByGroupKey / UpdateValue）也确实按两列过滤，但列标签
// 只给 Key 挂了 `uniqueIndex:idx_group_key`（Group 挂的是普通 `index`）。实测存量库：
//
//	idx_group_key | CREATE UNIQUE INDEX idx_group_key ON public.config_params USING btree (key)
//
// 于是不同 group 的同名 key 在**库里**是互斥的。今天 114 条种子恰好没有重名，所以没人
// 看见它坏；而 SeedConfigParams 的插入失败只打一行 Warnf 就 continue —— 下一次有人加
// `reach.max_attempts` 而 `bridge.max_attempts` 已存在时，那条参数会**静默不存在**：
// 管理台看不到它，代码里的 GetDuration 永远读到 fallback，改配置页面也没有任何反应。
// 换句话说这一版索引把「加参数」变成了要看运气的操作。
//
// 方向性：复合唯一严格弱于单列唯一，同数据下必定可建，因此 Up 不需要处理冲突数据。
type ConfigParamGroupKeyUniqueMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*ConfigParamGroupKeyUniqueMigration)(nil)

func NewConfigParamGroupKeyUniqueMigration(db *gorm.DB) *ConfigParamGroupKeyUniqueMigration {
	return &ConfigParamGroupKeyUniqueMigration{db: db}
}

func (m *ConfigParamGroupKeyUniqueMigration) Version() string { return "v3.47.0" }

func (m *ConfigParamGroupKeyUniqueMigration) Name() string {
	return "config_params 唯一键改为 (param_group, key) 复合"
}

func (m *ConfigParamGroupKeyUniqueMigration) Description() string {
	return "DROP 单列唯一索引 idx_group_key 并重建为 (param_group, key) 复合唯一，" +
		"让「同名 key 不同 group」成为合法参数行，与读取侧的过滤条件一致"
}

func (m *ConfigParamGroupKeyUniqueMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("config_params") {
		return nil // 全新库由模型标签直接建出复合唯一索引，此处无物可改
	}
	// 两条语句分开 Exec：多语句串在预编译协议下会被拒。
	if err := m.db.WithContext(ctx).Exec(`DROP INDEX IF EXISTS idx_group_key`).Error; err != nil {
		return fmt.Errorf("drop legacy idx_group_key failed: %w", err)
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_key ON config_params (param_group, key)`).Error; err != nil {
		return fmt.Errorf("create composite idx_group_key failed: %w", err)
	}
	return nil
}

// Down 回成单列唯一索引：这是**收紧**约束，存量里已有跨 group 同名 key 时 PG 会直接报错。
// 报错就是正确答案——那说明降级会把正在用的参数行判成冲突数据，不该由迁移静默删行来"成功"。
func (m *ConfigParamGroupKeyUniqueMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("config_params") {
		return nil
	}
	if err := m.db.WithContext(ctx).Exec(`DROP INDEX IF EXISTS idx_group_key`).Error; err != nil {
		return fmt.Errorf("drop composite idx_group_key failed: %w", err)
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_key ON config_params (key)`).Error; err != nil {
		return fmt.Errorf("restore legacy single-column idx_group_key failed: %w", err)
	}
	return nil
}
