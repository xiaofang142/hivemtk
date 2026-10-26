package migrations

import (
	"context"
	"fmt"
	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// InitialSchemaMigration 初始版本迁移
// 私域部署: 单租户,无 merchant_id 字段
type InitialSchemaMigration struct {
	db *gorm.DB
}

func NewInitialSchemaMigration(db *gorm.DB) *InitialSchemaMigration {
	return &InitialSchemaMigration{db: db}
}

func (m *InitialSchemaMigration) Version() string {
	return "v1.1.0"
}

func (m *InitialSchemaMigration) Name() string {
	return "初始版本迁移"
}

func (m *InitialSchemaMigration) Description() string {
	return "创建基础表结构"
}

func (m *InitialSchemaMigration) Up(ctx context.Context) error {
	return nil
}

func (m *InitialSchemaMigration) Down(ctx context.Context) error {
	return nil
}

var _ migration.Migration = (*InitialSchemaMigration)(nil)

// MarketingFlowSchemaMigration 营销自动化迁移
type MarketingFlowSchemaMigration struct {
	db *gorm.DB
}

func NewMarketingFlowSchemaMigration(db *gorm.DB) *MarketingFlowSchemaMigration {
	return &MarketingFlowSchemaMigration{db: db}
}

func (m *MarketingFlowSchemaMigration) Version() string {
	return "v1.3.0"
}

func (m *MarketingFlowSchemaMigration) Name() string {
	return "营销自动化"
}

func (m *MarketingFlowSchemaMigration) Description() string {
	return "添加营销流程、触发器、动作表"
}

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

func (m *MarketingFlowSchemaMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "flow_executions", "marketing_flows")
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
	register(NewCustomerOwnerAgentMigration(db))
	register(NewReachTablesMigration(db))
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
	register(NewBrowserAuditRetryTzMigration(db))
	register(NewOperationLogEncryptFieldsMigration(db))
	register(NewBrowserRequireConfirmMigration(db))
	register(NewBrowserWriteLedgerConfirmBudgetMigration(db))
	register(NewBrowserCommandLogOkTriStateMigration(db))
	register(NewMessageHubOutboundPushBudgetMigration(db))
	register(NewBrowserAuditPruneCutoffSourceMigration(db))
	register(NewConfigParamGroupKeyUniqueMigration(db))
	register(NewBrowserTaskCopyTextMigration(db))
	register(NewBrowserProfileHealthMigration(db))
	register(NewBrowserOutreachDedupeMigration(db))
	register(NewBrowserTaskCampaignBudgetMigration(db))
	register(NewBrowserOutreachReceiptMigration(db))
	register(NewFailoverPolicyToConfigParamMigration(db))
	register(NewEmbeddingGlobalOverrideToConfigParamMigration(db))
	register(NewMenuGrantCeilingPruneMigration(db))
	register(NewClueGeoCaptureTypeSplitMigration(db))
	register(NewTenantTableMigration(db))
	// 生产装配点：不得链 .withTables(...)（那是测试专用口子，会把"真库内省"退化成
	// "只处理指定表"）。TestTenantColumnBatchRegistration 钉住这一条。
	register(NewTenantColumnBatchMigration(db))
	register(NewTenantUniqueIndexMigration(db))
	register(NewTenantGlobalEmailUniqueMigration(db))
	register(NewAggregationWatermarkPKMigration(db))
	// v3.62.0：LLM 输入/输出分列计价与 provider 来源（模块化改造 · 任务五）
	register(NewLLMSeparatePricingMigration(db))
	// v3.63.0：tenants 静默合规采集三列
	register(NewTenantInitComplianceColumnsMigration(db))
	// v3.64.0：订阅镜像表
	register(NewTenantSubscriptionMirrorMigration(db))
	// v3.65.0：system_config_kv 租户分域（平台键迁至 platform_config_kv，主键复合化）
	register(NewPlatformKVTenantSplitMigration(db))
	// v3.66.0：站点配置一租户一行；功能开关唯一键降为租户内
	register(NewTenantSiteConfigAndFeatureFlagMigration(db))
	// v3.67.0：摘除浏览器定时触发器的软删残留行（它们占着 (tenant_id, task_id) 唯一键位，
	// 让同一任务再也挂不上新触发器）
	register(NewBrowserCronTriggerTombstonePruneMigration(db))
	// v3.68.0：A/B 实验结果唯一键从 (tenant_id, experiment_id) 恢复为
	// (tenant_id, experiment_id, variant_id)——单列唯一会让多变异实验算不出第二条结果
	register(NewABExperimentResultUniqueKeyMigration(db))
	// v3.69.0：bandit_arms 唯一键从 (tenant_id, arm_key) 恢复为
	// (tenant_id, experiment_id, arm_key)——arm_key 只在实验内唯一
	register(NewBanditArmUniqueKeyMigration(db))
	// v3.70.0：channel_agent_bindings 唯一键加入 chat_id 维度——同一账号下的
	// telegram 群组要能各自建绑定，而 account 级默认仍只允许一条
	register(NewChannelBindingChatIDUniqueKeyMigration(db))
	// v3.71.0：geo_daily_stats 唯一键 (tenant_id, stat_date, engine, intent)——
	// 模型原标签建出的是非唯一索引，ON CONFLICT 在全新库上 42P10
	register(NewGeoDailyStatUniqueKeyMigration(db))
	// v3.72.0：geo_entity_relations 唯一键 (tenant_id, entity_a_id, entity_b_id, relation)
	// ——先按该键去重再建，修复并发抽取写重复关系边；顺带删除 source_doc_id 僵尸列
	register(NewGeoEntityRelationUniqueKeyMigration(db))
	// v3.73.0：geo_index_trackings 唯一键 (tenant_id, article_id, engine)——先按该键
	// 去重再建，保证验证结果幂等写入（原「先 UPDATE 再 INSERT」并发写重复行）
	register(NewGeoIndexTrackingUniqueKeyMigration(db))
	// v3.74.0：geo_keyword_groups 唯一键 (tenant_id, name)——先去重再建，
	// 修复话题聚类并发 upsert 建出同名分组 / Save 复活软删除行
	register(NewGeoKeywordGroupUniqueKeyMigration(db))
	// v3.75.0：站内消息通知模块——建 notification_reads 回执表（平台消息
	// 按人已读），notifications 加 level/source_msg_id/source 列与
	// (tenant_id, source_msg_id) 幂等部分唯一索引
	register(NewInAppNotificationMigration(db))
	// v3.76.0：churn_predictions / customer_rfm 的存量孤儿行（tenant_id=0）
	// 归入默认租户 1 —— 那批行不属于任何租户，谁都读不到，等于隐身；
	// 写入侧已改为按主键定向更新，不再产生新孤儿行
	register(NewTenantOrphanRowRepairMigration(db))
	// v3.77.0：sop_state_memories.session_id 加 NOT NULL（D-6）——
	// 带失败前置检查：存在 NULL 行时报错不执行（处置决定留给运维，不静默删数据）
	register(NewSOPStateMemorySessionIDNotNullMigration(db))
	// v3.78.0：内容安全闸 guard_* 五张表——离线词库（deny/allow）+ 按主体类型
	// （user/merchant）的策略 + 异步审查任务队列 + 审查结论留痕 + 冻结/黑名单处置。
	// 表都是租户表，唯一索引经 tenancyidx 复合化为 (tenant_id, ...)
	register(NewContentGuardMigration(db))
	// v3.79.0：11 张手写 DDL 建表的租户表补 tenant_id 前导索引——
	// 判据是真库索引族里有没有 tenant_id 打头的列，不 grep 模型 tag：
	// tag 只对 AutoMigrate 建的表生效，对手写 CREATE TABLE 的表形同虚设（这 11 张就是这么漏的）
	register(NewTenantLeadingIndexMigration(db))
	// v3.80.0：存量"明文租户段"文件 URL 就地重签——/files 在 on 模式下
	// 只认签名凭证，把租户归属与过期时间搬进 ?s= token、URL 路径里不再出现 tenant_{id}。
	// 不改库形状、不删东西；幂等（签名后的值不再匹配 /files/tenant_<数字>/ 形态）
	register(NewFileURLSignMigration(db))
	// v3.81.0（引用漂移）：geo_citation_snapshots 唯一键 (tenant_id, snapshot_date)
	// ——快照 UPSERT 的 ON CONFLICT 依赖它；模型有意不用 uniqueIndex 标签（同 v3.71.0 教训）
	register(NewGeoCitationSnapshotUniqueKeyMigration(db))
	// v3.82.0（D-9 存储半项）：message_hub 加 platform_msg_id 列 +
	// (platform, account_id, platform_msg_id) 索引；存量行按行键格式回填，
	// 不能无歧义解析的留空（读路径有 platformMessageID 解析兜底）
	register(NewMessageHubPlatformMsgIDMigration(db))
	// v3.83.0（T3 观测报表）：llm_usage_daily 日聚合表（复合唯一索引 = upsert 冲突目标）+
	// llm_routing_logs / platform_llm_usage_records 补 channel、error_class 列 +
	// 存量失败行 error_class 回填 unknown
	register(NewLLMUsageDailyMigration(db))
	// v3.83.1（T1 低置信度回流）：unknown_questions 三表（沉淀/聚类/审核流水，均带 tenant_id）
	// + pg_trgm 扩展与 question GIN trigram 索引（相似归簇）
	register(NewUnknownQuestionRefluxMigration(db))
	// v3.84.0（退款行"原因"格）：payments 加可空 reason 列——
	// 退款行四格（金额/原因/渠道引用/状态）的第四格补齐，refunded 行填渠道退款原因
	register(NewPaymentReasonMigration(db))
	// v3.85.0（P0-2/P0-3/D-8 方案 A）：kb_item_bindings 成员表——知识库与内容
	// （FAQ/SOP/文档）的多对多绑定，UNIQUE(kb_id,item_type,item_id) 租户内唯一。
	// 无回填：存量内容与 KB 的 owner agent 交集≈0，绑定由管理端点显式登记。
	register(NewKBItemBindingMigration(db))
	// v3.86.0（B2 撤回状态格）：message_hub 加 recalled_at / recall_error——
	// 平台撤回通道早已接通，缺的是本地回执，导致重复撤回会重复出网。
	// 无回填：历史行的撤回状态保持"未知"，不猜成"没撤过"。
	register(NewMessageHubRecallMigration(db))
}
