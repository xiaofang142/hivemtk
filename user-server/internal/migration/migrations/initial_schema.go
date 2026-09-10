package migrations

import (
	"context"
	"fmt"
	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// InitialSchemaMigration 初始版本迁移(原 V110Migration 重命名,语义化命名)
// 私域部署: 单租户,无 merchant_id 字段
type InitialSchemaMigration struct {
	db *gorm.DB
}

// NewInitialSchemaMigration 创建初始版本迁移
func NewInitialSchemaMigration(db *gorm.DB) *InitialSchemaMigration {
	return &InitialSchemaMigration{db: db}
}

// Version 返回版本号
func (m *InitialSchemaMigration) Version() string {
	return "v1.1.0"
}

// Name 返回迁移名称
func (m *InitialSchemaMigration) Name() string {
	return "初始版本迁移"
}

// Description 返回迁移描述
func (m *InitialSchemaMigration) Description() string {
	return "创建基础表结构"
}

// Up 执行升级
func (m *InitialSchemaMigration) Up(ctx context.Context) error {
	return nil
}

// Down 执行降级
func (m *InitialSchemaMigration) Down(ctx context.Context) error {
	return nil
}

var _ migration.Migration = (*InitialSchemaMigration)(nil)

// MarketingFlowSchemaMigration 营销自动化迁移(原 V130Migration 重命名)
type MarketingFlowSchemaMigration struct {
	db *gorm.DB
}

// NewMarketingFlowSchemaMigration 创建营销流程迁移
func NewMarketingFlowSchemaMigration(db *gorm.DB) *MarketingFlowSchemaMigration {
	return &MarketingFlowSchemaMigration{db: db}
}

// Version 返回版本号
func (m *MarketingFlowSchemaMigration) Version() string {
	return "v1.3.0"
}

// Name 返回迁移名称
func (m *MarketingFlowSchemaMigration) Name() string {
	return "营销自动化"
}

// Description 返回迁移描述
func (m *MarketingFlowSchemaMigration) Description() string {
	return "添加营销流程、触发器、动作表"
}

// Up 执行升级
func (m *MarketingFlowSchemaMigration) Up(ctx context.Context) error {
	m.db.Exec(`
		CREATE TABLE IF NOT EXISTS marketing_flows (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(100),
			description VARCHAR(500),
			status VARCHAR(20) DEFAULT 'draft',
			trigger_type VARCHAR(20),
			trigger_config TEXT,
			flow_data TEXT,
			version INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)

	m.db.Exec(`
		CREATE TABLE IF NOT EXISTS flow_executions (
			id BIGSERIAL PRIMARY KEY,
			flow_id BIGINT,
			trigger_id VARCHAR(50),
			user_id VARCHAR(50),
			status VARCHAR(20),
			current_node VARCHAR(50),
			execution_data TEXT,
			started_at TIMESTAMP,
			completed_at TIMESTAMP,
			error_message TEXT
		)
	`)

	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_marketing_flows_status ON marketing_flows(status)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_flow_executions_flow ON flow_executions(flow_id)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_flow_executions_user ON flow_executions(user_id)")

	return nil
}

// Down 执行降级
func (m *MarketingFlowSchemaMigration) Down(ctx context.Context) error {
	m.db.Exec("DROP TABLE IF EXISTS flow_executions")
	m.db.Exec("DROP TABLE IF EXISTS marketing_flows")
	return nil
}

var _ migration.Migration = (*MarketingFlowSchemaMigration)(nil)

// RegisterMigrations 注册所有迁移
// 如果检测到重复版本号会 panic（开发期错误，必须立即修复）
func RegisterMigrations(registry *migration.MigrationRegistry, db *gorm.DB) {
	register := func(m migration.Migration) {
		if err := registry.Register(m); err != nil {
			panic(fmt.Sprintf("迁移注册失败: %v", err))
		}
	}

	register(NewInitialSchemaMigration(db))
	register(NewMarketingFlowSchemaMigration(db))
	register(NewUnmultitenantSchemaMigration(db))
	register(NewMerchantIDNullableMigration(db))
	register(NewWecomWebhookFieldsMigration(db))
	register(NewAIAgentSchemaMigration(db))
	register(NewAIAgentExtensionMigration(db))
	register(NewKnowledgeVectorMigration(db))
	register(NewRagHybridMigration(db))
	register(NewSOPExecutorMigration(db))
	register(NewConfidenceMigration(db))
	register(NewRagMonitoringMigration(db))
	register(NewAmountMoneyMigration(db))
	register(NewHumanizeEvaluatorMigration(db))
	register(NewFeedbackLoopMigration(db))
	register(NewAuthSecurityMigration(db))
	register(NewComplianceDEMigration(db))
	register(NewHP1Migration(db))
	register(NewLP1Migration(db))
	register(NewADomainP1Migration(db))
	register(NewShortLinkColumnsMigration(db))
	register(NewMP1Migration(db))
	register(NewLLMUsageRecordsMigration(db))
	register(NewAssetBundleMigration(db))
	register(NewLLMRoutingLogsMigration(db))
	register(NewLLMRoutingLogsExtendMigration(db))
	register(NewAgentAssetBindingMigration(db))
	register(NewUserBlacklistMigration(db))
	register(NewBridgeAccountMigration(db))
	register(NewWorkflowVisualOrchestratorMigration(db))
	register(NewMultilingualI18nMigration(db))
	register(NewMultilingualI18nP13Migration(db))
	register(NewKnowledgeWeightMigration(db))
	register(NewTelegramPollingLockMigration(db))
	register(NewAIPerfFAQSOPLayerMigration(db))
	register(NewAIAgentKBBindingMigration(db))
	register(NewKBUnificationMigration(db))
	register(NewRagSafetyAuditDropMigration(db))
	register(NewRagAlertsDropMigration(db))
	register(NewBridgeChannelNormalizeMigration(db))
	register(NewBridgeChannelUnifyV2Migration(db))
	register(NewBridgeChannelUnifyV3_18_1Migration(db))
	register(NewCustomerSessionUpdatedAtMigration(db))
	register(NewKnowledgeSearchLogProductOptionalMigration(db))
	register(NewDropCdpAutoReplyMigration(db))
	register(NewLiveCodeClickLogsMigration(db))
	register(NewBridgeMetricsMigration(db))
	register(NewCustomerIDStandardizeMigration(db))
	register(NewSoftDeleteMigration(db))
	register(NewEnumOptimizeMigration(db))
	register(NewIntentMergeMigration(db))
	register(NewSessionIDLengthMigration(db))
	register(NewAlertRuleMigration(db))
	register(NewUnifiedIDWidenMigration(db))
	register(NewSOPTimerSinkColumnsMigration(db))
	register(NewSOPHeatmapIndexMigration(db))
	register(NewEmailSmtpPasswordEncryptMigration(db))
	register(NewSOPExecutedNodesMigration(db))
	register(NewBanditRefluxLogMigration(db))
	register(NewEmbeddingSourceMigration(db))
	register(NewHandoffOutcomeMigration(db))
	register(NewAgentCheckpointMigration(db))
	register(NewChurnScoreMigration(db))
	register(NewTelegramGroupGateMigration(db))
	register(NewAdminPasswordGuardMigration(db))
	register(NewBrowserAutomationMigration(db))
	register(NewBrowserCommandLogMigration(db))
	register(NewBrowserTaskPlatformMigration(db))
}
