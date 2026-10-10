package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// EmbeddingGlobalOverrideToConfigParamMigration v3.54.0：把「全局 Embedding 提供商覆盖」
// 的存放位置从遗留 KV 表搬到参数中心，并把**管理员已经配好的那份值**带过去。
//
// 搬的原因：这张表（key/value 两列，没有缓存也没有管理端写路径）是 D12 判为遗留的那一套，
// 而 embedding 的全局指向是这份配置里唯一还在直写它的业务项 —— 读侧每次多一层自建 60s 缓存，
// 写侧是一条没有来路记录的 INSERT … ON CONFLICT。参数中心那侧有值类型校验、缓存失效和变更审计。
//
// 不搬数据会怎样：读路径换口之后，旧行里那份 base_url/model/加密后的 api_key 不再被任何人读到，
// 线上表现为向量服务**静默退回本地栈**——小内存服务器上就是哈希伪向量兜底（allowFallback 那条），
// 检索召回会变差，而日志里只有一条 "使用默认回落"。
//
// 旧行怎么处理（与 v3.53.0 不同的一处）：那张表只有 key/value 两列，没有地方写"已废弃"这种说明，
// 而值里带着密文的 API Key，直接删等于把凭证抹掉、回滚也拿不回来。所以这里改的是**键名**：
// 搬完之后把旧行的 key 换成一个自解释的墓碑名，值原样不动。效果是任何还在按旧键名读的代码
// 读到"没有这一行"（与刚升级的状态一致），而翻表的人一眼能看到值搬去了哪。
type EmbeddingGlobalOverrideToConfigParamMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*EmbeddingGlobalOverrideToConfigParamMigration)(nil)

func NewEmbeddingGlobalOverrideToConfigParamMigration(db *gorm.DB) *EmbeddingGlobalOverrideToConfigParamMigration {
	return &EmbeddingGlobalOverrideToConfigParamMigration{db: db}
}

func (m *EmbeddingGlobalOverrideToConfigParamMigration) Version() string { return "v3.54.0" }

func (m *EmbeddingGlobalOverrideToConfigParamMigration) Name() string {
	return "全局 Embedding 覆盖搬入参数中心（embedding.global_override）"
}

func (m *EmbeddingGlobalOverrideToConfigParamMigration) Description() string {
	return "把遗留表 system_config_kv 里 embedding_global 的当前值复制到参数中心 " +
		"embedding/global_override（缺行则补建），并把旧行的键名换成墓碑名"
}

const (
	embeddingLegacyKey          = "embedding_global"
	embeddingLegacyKeyTombstone = "embedding_global.moved_to_config_params.v3.54.0"
	embeddingParamGroup         = "embedding"
	embeddingParamKey           = "global_override"
	embeddingParamName          = "全局 Embedding 提供商覆盖（整份 JSON）"
	embeddingParamDesc          = "整份 JSON：{enabled,base_url,api_key,model}；本行由 v3.54.0 从遗留表 system_config_kv 搬来，定义列后续由参数种子对齐"
)

func (m *EmbeddingGlobalOverrideToConfigParamMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 两张表任缺其一就无从搬运：缺 system_config_kv 说明这套部署没写过全局 embedding 覆盖，
	// 缺 config_params 说明参数中心还没建起来，此时建行归种子流程管而不是这里。
	for _, table := range []string{"system_config_kv", "config_params"} {
		if !m.db.WithContext(ctx).Migrator().HasTable(table) {
			return nil
		}
	}

	// ① 参数行缺失 → 用旧行的值补建（ON CONFLICT DO NOTHING 让已存在的行原样保留）。
	if err := m.db.WithContext(ctx).Exec(
		`INSERT INTO config_params
			(param_group, key, name, description, value_type, param_value, default_value, created_at, updated_at)
		 SELECT $1, $2, $3, $4, 'string', k.value, k.value, NOW(), NOW()
		   FROM system_config_kv k
		  WHERE k.key = $5 AND k.value <> ''
		 ON CONFLICT (param_group, key) DO NOTHING`,
		embeddingParamGroup, embeddingParamKey, embeddingParamName, embeddingParamDesc, embeddingLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("backfill embedding/global_override from legacy KV failed: %w", err)
	}

	// ② 参数行已存在且与旧行不同 → 覆盖值搬家。相等则不写，避免每次启动都刷一遍 updated_at。
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE config_params cp
		   SET param_value = k.value, updated_at = NOW()
		  FROM system_config_kv k
		 WHERE cp.param_group = $1 AND cp.key = $2
		   AND k.key = $3 AND k.value <> '' AND k.value <> cp.param_value`,
		embeddingParamGroup, embeddingParamKey, embeddingLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("copy legacy embedding_global value into config_params failed: %w", err)
	}

	// ③ 旧行换墓碑名（值不动）。NOT EXISTS 让重复执行为零效果：墓碑已经在位就说明搬过。
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE system_config_kv
		   SET key = $1, updated_at = NOW()
		  WHERE key = $2
		    AND NOT EXISTS (SELECT 1 FROM system_config_kv t WHERE t.key = $1)`,
		embeddingLegacyKeyTombstone, embeddingLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("retire legacy embedding_global row failed: %w", err)
	}
	return nil
}

// Down 不做数据回滚：参数中心的值是管理台上正在被编辑的活数据，把它"回滚"成搬来之前的快照
// 会覆盖掉升级之后运维已经做过的改动，那是净损失。这里只把旧行的键名换回来，
// 让回滚后的进程仍能从遗留表读到那份值（键名之外的内容与升级前逐字相同）。
func (m *EmbeddingGlobalOverrideToConfigParamMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.WithContext(ctx).Migrator().HasTable("system_config_kv") {
		return nil
	}
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE system_config_kv
		   SET key = $1, updated_at = NOW()
		  WHERE key = $2
		    AND NOT EXISTS (SELECT 1 FROM system_config_kv t WHERE t.key = $1)`,
		embeddingLegacyKey, embeddingLegacyKeyTombstone,
	).Error; err != nil {
		return fmt.Errorf("restore legacy embedding_global key failed: %w", err)
	}
	return nil
}
