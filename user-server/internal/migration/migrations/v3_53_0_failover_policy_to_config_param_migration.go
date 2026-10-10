package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// FailoverPolicyToConfigParamMigration v3.53.0：把 LLM 降级策略的存放位置从
// 遗留 KV 表搬到参数中心，并把**运维在旧行里改过的值**带过去。
//
// 搬的原因（配置层统一）：system_kv_config 既没有缓存也没有管理端写路径，读侧
// （internal/aiagent/llm/provider_failover.go 的 LoadPolicy）原本是每 tick 一次的裸 SQL；
// 现在那条读路径走参数中心的 60s 缓存，改策略在管理台上就能做。
//
// 不搬数据会怎样：LoadPolicy 换了读取口之后，旧行里的手工覆盖值不会再被任何人读到。
// 存量库里 `llm_provider_failover` 这行是 m_p1 迁移种下的，管理员如果直接 UPDATE 过它
// （那张表没有 UI，改法只有手搓 SQL，而这正是它唯一的改法），升级后候选 provider 列表、
// 熔断阈值、兜底话术会**静默退回默认值**——线上表现为降级行为变了，日志里一个字都没有。
//
// 幂等与安全边界：
//   - 旧行不存在（全新库、或没跑过 m_p1）→ 三条语句分别是"按默认值建行 / 无匹配行 / 无匹配行"，
//     净效果为零；
//   - 参数行已存在且值与旧行相同 → 第二条的 `<>` 条件不成立，不写；
//   - 参数行不存在（seed 失败的那次启动）→ 第一条用旧行的值把参数行补出来，
//     名字/说明等定义列后续由 SeedConfigParams 的 syncParamDef 对齐当前种子；
//   - 旧行**不删**：它是历史事实，且删掉会让"我明明改过它怎么不生效了"无从考古。
//     取而代之的是把它的 description 标成已废弃，让下一个直接翻表的人一眼看到生效位置在哪。
type FailoverPolicyToConfigParamMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*FailoverPolicyToConfigParamMigration)(nil)

func NewFailoverPolicyToConfigParamMigration(db *gorm.DB) *FailoverPolicyToConfigParamMigration {
	return &FailoverPolicyToConfigParamMigration{db: db}
}

func (m *FailoverPolicyToConfigParamMigration) Version() string { return "v3.53.0" }

func (m *FailoverPolicyToConfigParamMigration) Name() string {
	return "LLM 降级策略搬入参数中心（agent_llm.provider_failover_policy）"
}

func (m *FailoverPolicyToConfigParamMigration) Description() string {
	return "把遗留表 system_kv_config 里 llm_provider_failover 的当前值复制到参数中心 " +
		"agent_llm/provider_failover_policy（缺行则补建），并把旧行的 description 标为已废弃"
}

const (
	failoverLegacyKey       = "llm_provider_failover"
	failoverParamGroup      = "agent_llm"
	failoverParamKey        = "provider_failover_policy"
	failoverParamName       = "LLM 降级策略（整份 JSON）"
	failoverParamDesc       = "按部署覆盖 ProviderFailover 的整份策略；本行由 v3.53.0 从遗留表 system_kv_config 搬来，定义列后续由参数种子对齐"
	failoverLegacyDeprecate = "已废弃：本行自 v3.53.0 起不再被读取，生效策略在参数中心 agent_llm/provider_failover_policy（当前值已搬到那一行）"
)

func (m *FailoverPolicyToConfigParamMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 两张表任缺其一就无从搬运：缺 system_kv_config 说明这套部署没跑过 p1（没有旧值），
	// 缺 config_params 说明参数中心还没建起来，此时应当由种子流程负责建行而不是这里。
	for _, table := range []string{"system_kv_config", "config_params"} {
		if !m.db.WithContext(ctx).Migrator().HasTable(table) {
			return nil
		}
	}

	// ① 参数行缺失 → 用旧行的值补建（ON CONFLICT DO NOTHING 让已存在的行原样保留）。
	// 与种子 DefaultValue 逐字相同的那份 JSON 由参数中心的种子表负责，这里只搬"旧行里是什么"。
	if err := m.db.WithContext(ctx).Exec(
		`INSERT INTO config_params
			(param_group, key, name, description, value_type, param_value, default_value, created_at, updated_at)
		 SELECT $1, $2, $3, $4, 'string', k.value, k.value, NOW(), NOW()
		   FROM system_kv_config k
		  WHERE k.key = $5 AND k.value <> ''
		 ON CONFLICT (param_group, key) DO NOTHING`,
		failoverParamGroup, failoverParamKey, failoverParamName, failoverParamDesc, failoverLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("backfill agent_llm/provider_failover_policy from legacy KV failed: %w", err)
	}

	// ② 参数行已存在且与旧行不同 → 覆盖值搬家。相等则不写，避免每次启动都刷一遍 updated_at。
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE config_params cp
		   SET param_value = k.value, updated_at = NOW()
		  FROM system_kv_config k
		 WHERE cp.param_group = $1 AND cp.key = $2
		   AND k.key = $3 AND k.value <> '' AND k.value <> cp.param_value`,
		failoverParamGroup, failoverParamKey, failoverLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("copy legacy llm_provider_failover value into config_params failed: %w", err)
	}

	// ③ 旧行标废弃：只改说明，值原样留着当历史快照。NOT LIKE 让重复执行为零效果。
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE system_kv_config
		   SET description = $1, updated_at = NOW()
		  WHERE key = $2 AND description NOT LIKE '已废弃%'`,
		failoverLegacyDeprecate, failoverLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("deprecate legacy llm_provider_failover row failed: %w", err)
	}
	return nil
}

// Down 不做数据回滚，理由与同类"搬家"迁移一致：
// 旧行从头到尾没有被删或改值（只改了 description），参数中心的值则是管理台上正在被编辑的
// 活数据——把它"回滚"成搬来之前的快照，会覆盖掉升级之后运维已经做过的改动，那是净损失。
// 因此 Down 只把废弃说明摘掉，让旧行回到"看起来仍可能是生效位置"的中性状态。
func (m *FailoverPolicyToConfigParamMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.WithContext(ctx).Migrator().HasTable("system_kv_config") {
		return nil
	}
	if err := m.db.WithContext(ctx).Exec(
		`UPDATE system_kv_config
		   SET description = 'LLM Provider 降级策略配置（v3.53.0 起读路径在参数中心，本行为历史快照）',
		       updated_at = NOW()
		  WHERE key = $1 AND description LIKE '已废弃%'`,
		failoverLegacyKey,
	).Error; err != nil {
		return fmt.Errorf("restore legacy llm_provider_failover description failed: %w", err)
	}
	return nil
}
