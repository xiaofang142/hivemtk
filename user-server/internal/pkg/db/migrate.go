package db

import (
	"fmt"
	ragcachemodel "hivemtk-user/internal/aiagent/rag/cache"
	browsermodel "hivemtk-user/internal/browser_automation/model"
	contentmodel "hivemtk-user/internal/content/model"
	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"strings"
	"sync"

	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"

	"gorm.io/gorm"
)

func allModels() []any {
	return []any{
		&model.Order{},
		&model.AfterSale{},
		&model.User{},
		&model.Account{},
		&model.Message{},
		&model.Smlist{},
		&model.Clue{},
		&model.EmailSmtp{},
		&model.EmailDraft{},
		&model.EmailList{},
		&model.EmailJobs{},
		&model.EmailSend{},
		&model.SystemConfig{},
		&model.DomainPool{},
		&model.DomainBlacklist{},
		&model.DomainHealthLog{},
		&model.ShortLink{},
		&model.ShortLinkAccess{},
		&model.DouyinCard{},
		&model.DouyinCardActivity{},
		&model.XiaohongshuCard{},
		&model.XiaohongshuCardActivity{},
		&model.KuaishouCard{},
		&model.KuaishouCardActivity{},
		&model.XianyuCard{},
		&model.XianyuCardActivity{},
		&model.SmsConfig{},
		&model.SmsAliyunConfig{},
		&model.SmsTencentConfig{},
		&model.SmsHuaweiConfig{},
		&model.SmsRecord{},
		&model.SmsDraft{},
		&model.SmsJob{},
		&model.SmsJobDetail{},
		&model.SystemUser{},
		&model.LiveCode{},
		&model.LiveCodeQR{},
		&model.LiveCodeQRStat{},
		&model.ObsConfig{},
		&contentmodel.Material{},
		&contentmodel.MaterialCategory{},

		&model.WhatsappAccount{},
		&model.WhatsappSession{},
		&model.WhatsappDraft{},
		&model.WhatsappJob{},
		&model.WhatsappJobDetail{},
		&knowledgemodel.RagProduct{},
		&model.PlatformAccountConfig{},
		&model.APILog{},
		&model.WebVitalRecord{},
		&model.CustomerSegment{},
		&model.Macro{},
		&model.SessionAISummary{},
		&model.AutomationRule{},
		&model.RulePendingExecution{},
		&model.HelpCenterTestRecord{},
		&model.WebhookSubscription{},
		&model.SavedView{},
		&model.ReportSubscription{},
		&model.RagEvalQuestion{},
		&model.RagEvalRun{},
		&model.VisitLog{},
		&model.DailyStats{},
		&knowledgemodel.KBDocument{},
		&model.TikTokCard{},
		&model.TikTokCardActivity{},
		&model.WhatsappGroupMessage{},
		&model.Backup{},
		&model.RestoreRecord{},
		&knowledgemodel.RagSession{},
		&knowledgemodel.RagMessage{},
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.QuickReply{},
		&model.QuickReplyFolder{},
		&model.CSATSurvey{},
		&model.SessionTag{},
		&model.WeComAccount{},
		&model.WeComCustomer{},
		&model.WeComGroup{},
		&model.WeComGroupMember{},
		&model.WeComMessage{},
		&model.WeComTag{},
		&model.TelegramAccount{},
		&model.QQAccount{},
		&model.TelegramGroupGate{},
		&model.TelegramGroupMember{},
		&model.SSOIdentity{},
		&model.FeishuAccount{},
		&model.FeishuCustomer{},
		&model.FeishuMessage{},
		&model.WhatsappMessageTemplate{},
		&model.WhatsAppMessageQueue{},
		&model.WhatsAppQueueStatus{},
		&model.SystemMetrics{},
		&model.Customer{},
		&model.CustomerTag{},
		&model.CustomerTagAssignment{},
		&model.CustomerDoNotContact{},
		&model.CustomerEvent{},
		&model.UserTag{},
		&model.OperationLog{},
		&opsmodel.ABExperiment{},
		&opsmodel.ABVariant{},
		&opsmodel.ABConversionEvent{},
		&opsmodel.ABExperimentResult{},
		&opsmodel.ChurnPrediction{},
		&opsmodel.ChurnWarning{},
		&opsmodel.ChurnModelConfig{},
		&opsmodel.ChurnStatistics{},
		&model.RFMRule{},
		&model.UserRFM{},
		&model.IntegrationAccount{},
		&model.SyncLog{},
		&model.ExternalCustomer{},
		&model.ExternalOrder{},
		&model.ExternalProduct{},
		&model.WebhookEvent{},
		&model.CommunityGroup{},
		&model.CommunityMember{},
		&model.CommunityMessage{},
		&contentmodel.MarketingFlow{},
		&contentmodel.FlowExecution{},
		&opsmodel.CustomReport{},
		&opsmodel.DashboardScreen{},
		&opsmodel.DashboardWidget{},
		&contentmodel.MarketTemplate{},
		&contentmodel.MarketTemplateDownload{},
		&contentmodel.ScriptTemplate{},
		&contentmodel.ScriptCategory{},
		&contentmodel.ScriptRecommend{},
		&contentmodel.AIGenerationRecord{},
		&contentmodel.PromptTemplate{},
		&model.UnifiedMessage{},
		&model.UnifiedReply{},
		&model.PlatformAccount{},
		&model.UpgradeTask{},
		&model.MigrationRecord{},
		&model.MigrationCheckpoint{},
		&contentmodel.BatchOperationHistory{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.InboxAssignment{},
		&model.MessageTrace{},
		&model.TraceEvalLog{},
		&model.LearningInsight{},
		&model.WeComAccountHealth{},
		&model.IntentRecord{},
		&model.DialogueMemory{},
		&model.SOPAgent{},
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.SOPTimer{},
		&model.SOPOutbox{},
		&model.SalesIntentScore{},
		&model.AISalesLog{},
		&model.MemoryItem{},
		&model.SOPStateMemory{},
		&model.BusinessMemory{},
		&model.CustomerLongTermMemory{},
		&model.LowQualitySample{},
		&model.SalesChampionProfileSnapshot{},
		&model.SOPNodeTransition{},
		&model.OptimizationSuggestion{},
		&model.ConfidenceSignal{},
		&model.ConfidenceCalibration{},
		&model.HandoffDecisionRecord{},
		&model.ThresholdPolicy{},
		&model.ABTest{},
		&model.ABTestMetric{},
		&model.HumanizeScore{},
		&model.HumanizeDimensionRecord{},
		&model.ChampionBaseline{},
		&model.ChampionPhrase{},
		&model.ABTestStat{},
		&model.FeedbackEvent{},
		&model.FeedbackSignal{},
		&model.ChampionDialogue{},
		&model.PromptCandidate{},
		&model.BanditArm{},
		&model.PromptABTest{},
		&model.ReachPipeline{},
		&model.ReachJob{},
		&model.ScriptLibrary{},
		&model.ScriptVersion{},
		&model.ScriptExposureLog{},
		&model.FeatureFlag{},
		&model.FeatureFlagAuditLog{},
		&model.FeatureFlagEvalLog{},
		&model.ObjectionTemplate{},
		&model.ConversionFunnel{},
		&model.SalesPersona{},
		&model.SalesEvent{},
		&opsmodel.PerformanceTestResult{},
		&knowledgemodel.KnowledgeDocument{},
		&knowledgemodel.KnowledgeChunk{},
		&knowledgemodel.KnowledgeImportLog{},
		&knowledgemodel.KnowledgeSearchLog{},
		&knowledgemodel.KnowledgeOpenAPISource{},
		&knowledgemodel.KnowledgeAPIToken{},
		&knowledgemodel.KnowledgeFeedback{},
		&knowledgemodel.ExternalImportJob{},
		&model.AIAgent{},
		&model.ChannelAgentBinding{},
		&model.CustomerServiceAgent{},
		&model.LLMProvider{},
		&model.LLMRoutingRule{},
		&model.FAQEntry{},
		&model.SOPTemplate{},
		&model.LayerDecisionLog{},
		&model.ChatChannel{},
		&model.Notification{},
		&model.AssetBundle{},
		&model.AssetBundleVersionLog{},
		&model.LocalAsset{},
		&model.LocalAssetData{},
		&model.LocalAssetSyncLog{},
		&model.DingTalkAppAccount{},
		&model.WhatsAppCloudAccount{},
		&model.AIToolConfig{},
		&model.AIToolAccountBinding{},
		&model.EmailUnsubscribe{},
		&model.SmsUnsubscribe{},
		&model.SmsDeliveryStatus{},
		&model.SmsNumberPortabilityRecord{},
		&model.RagQueryLog{},
		&model.RagRecallMonitorSnapshot{},
		&model.FeedbackRecordORM{},
		&model.LeadMiningConfig{},
		&model.SecurityAudit{},
		&model.SecurityAuditItem{},
		&model.UserBlacklist{},
		&model.BridgeAccount{},
		&model.PasswordResetToken{},

		&geomodel.GeoKeyword{},
		&geomodel.GeoKeywordGroup{},
		&geomodel.GeoArticle{},
		&geomodel.GeoOptimization{},
		&geomodel.GeoVerifyResult{},
		&geomodel.GeoAPICall{},
		&geomodel.GeoConfig{},
		&geomodel.GeoPlatformAccount{},
		&geomodel.GeoPublishRecord{},
		&geomodel.GeoKnowledgeDocument{},
		&geomodel.GeoWorkflow{},
		&geomodel.GeoWorkflowExecution{},
		&geomodel.GeoWorkflowTemplate{},

		&geomodel.GeoQueryChain{},
		&geomodel.GeoContentTask{},

		&geomodel.GeoProbeRun{},
		&geomodel.GeoDailyStat{},
		&geomodel.GeoSourceCatalog{},
		&geomodel.GeoEntity{},
		&geomodel.GeoEntityRelation{},
		&geomodel.GeoAlert{},
		&geomodel.GeoJobRun{},
		&geomodel.GeoCompetitor{},
		&geomodel.GeoPushRecord{},
		&geomodel.GeoSite{},
		&geomodel.GeoPusherConfig{},
		&geomodel.GeoIndexTracking{},
		&geomodel.GeoSchemaTemplate{},

		// GeoCrawlerVisit 此前是 29 个 geo 模型里**唯一没登记**的（2026-09-16 审计 DB-07）：
		// internal/router/router.go 的 AICrawlerMonitor 回调会 fire-and-forget 地写入它，
		// 而该写入的 error 又被 `_ =` 丢弃 —— 全新部署不建表 ⇒ 数据永久为 0 且毫无报错。
		// 现在补进清单，建表不再依赖"历史遗留库里恰好有这张表"。
		&geomodel.GeoCrawlerVisit{},

		// -------------------------------------------------------------------
		// 2026-09-16 审计 DB-07：补齐「有生产写入路径、却从未登记建表」的模型。
		//
		// 这些模型的表在开发库里确实存在，但那只是历史遗留（曾有测试把模型
		// AutoMigrate 进了 user_db，见 TEST-06）；它们既不在 allModels()、
		// 也不在 internal/migration/migrations、也不在 migrations/*.sql 里。
		// 因此**全新部署不会建这些表** → 写入失败 → 而多数写入点的 error
		// 又被 `_ =` 丢掉 → 表现为"功能静默失效"。
		//
		// 以下 29 个已逐个核对：均有 repository/存储层的 Create/Save 写入路径。
		// 另有两个因 import 成环无法写在此处，改用 RegisterExtraModels 登记：
		//   - KBDocumentChunkRow（internal/repository，见该文件 init）
		//   - TraceEvent        （internal/aiagent/llm，见该文件 init）
		// -------------------------------------------------------------------
		&model.AggregationWatermark{},
		&model.AlertHistory{},
		&model.AlertRule{},
		// ApprovalRequest（表 approval_requests）：T-P3-01 / N-4 审批检查点，
		// 写入路径是 repository.approvalRequestRepo.Insert。
		// 本卡交付的是底座、尚未装配（装配卡 T-P3-02/T-P3-07），但**建表登记必须在
		// 今天这一批**：登记晚一卡，中间那段部署里闸门第一次落库就会往不存在的表里写
		// （T-P1-08 在 tool_call_audits 上正是这个形状：只接 logger 不登记表 = 整批静默降级）。
		&model.ApprovalRequest{},
		&model.BanditRefluxLog{},
		&model.ChurnScore{},
		&model.ClueEngagementEvent{},
		&model.ClueScore{},
		&model.ConfigParamAuditLog{},
		&model.CustomerChannel{},
		// HumanTask（表 human_tasks）：T-P3-03 / N-9 统一人工待办，三类 kind 共用一张表。
		// 写入路径有两条且都已接线：repository.humanTaskRepo.Insert（转人工投递）与
		// ApplyAction（坐席认领/释放/完成/撤销、会话结束时的系统撤销）。
		// 与 ApprovalRequest 同一条理由：登记晚一卡，第一批流量就会往不存在的表里写。
		&model.HumanTask{},
		&model.IntegrationTemplate{},
		&model.IntentExample{},
		&model.LLMRoutingLog{},
		&model.LoginEvent{},
		// OrderDraft（表 order_drafts）：T-P2-01 前草稿只活在 service 的内存 map 里，
		// 全仓无 model 无 repo，所以这里是第一次建表登记（走版本化迁移在本仓不生效，
		// 见本文件头部关于 ExecuteUpgrade 固定空跑的说明）。
		&model.OrderDraft{},
		// Opportunity（表 opportunities）：T-P4-01 / N-1 商机域第一层。
		// 今天还没有生产写入方（构造与分配在 T-P4-05），但登记建表与登记写入路径是
		// 两件事：这张表的失败面是"表没建、代码全对"——建商机那一步只会在日志里留一句
		// 话，而商机的下游（漏斗、闭环率）全部读成 0 且无人报错（ApprovalRequest/HumanTask 同此）。
		&model.Opportunity{},
		&model.PasswordHistory{},
		&model.RagMetricsDaily{},
		// Quote / QuoteLineItem（表 quotes / quote_line_items）：T-P6-01 / N-5 报价域第一层。
		// 与 Opportunity 同一句理由登记：本卡只交付列与索引形状，生成方在 T-P6-02。
		// 这里少登记一张会是一种**不对称**的坏法：quotes 建了、明细没建 ⇒ 报价存得下、
		// 行项目写不进去，而"合计对不对"的用例连不上明细表时是 Skip 不是 Fail。
		&model.Quote{},
		&model.QuoteLineItem{},
		&model.RecoveryQueue{},
		&model.SecurityAlert{},
		&model.SystemConfigKV{},

		// ToolCallAudit（表 tool_call_audits）：T-P0-04 判定的"实现了但没接线"里最隐蔽的一条
		// —— 写入方 tooluse.DBAuditLogger 一直存在，但表**从未登记建表**（它自带的
		// AutoMigrateAuditTable 零调用），且 check_model_migration.py 因为"文件里含
		// .AutoMigrate("而把它误判成已登记。实测开发库里没有这张表。
		&model.ToolCallAudit{},

		&model.UserMFA{},
		&model.WorkflowExecution{},
		&model.WorkflowNodeExecution{},
		&model.WorkflowVersion{},

		&browsermodel.BrowserCommandLog{},
		&browsermodel.BrowserCronTrigger{},
		&browsermodel.BrowserLLMPlan{},
		&browsermodel.BrowserSession{},
		&browsermodel.BrowserStep{},
		&browsermodel.BrowserTask{},

		// 表 rag_answer_cache：store_pg.go 的文件头注释原本就写着
		// 「需在 internal/migration/migrations 注册」并附了 DDL，但一直没接。
		// 登记到 allModels() 效果等价（该包不 import pkg/db，无成环问题）。
		&ragcachemodel.RAGAnswerCache{},
	}
}

func ensureExtensions() {
	allowedExts := map[string]bool{
		"vector":    true,
		"uuid-ossp": true,
	}
	exts := []string{"vector", `"uuid-ossp"`}
	for _, ext := range exts {
		bare := strings.Trim(ext, `"`)
		if !allowedExts[bare] {
			logger.Warn(fmt.Sprintf("跳过未知 PG 扩展(白名单拒绝): %s", ext))
			continue
		}
		if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS " + ext).Error; err != nil {
			logger.Warn(fmt.Sprintf("启用 PG 扩展提示(可忽略若已在外部创建): %s, err=%v", ext, err))
		}
	}
}

func AutoMigrate() *gorm.DB {
	ensureExtensions()

	tolerated := 0
	all := append(allModels(), ExtraModels()...)
	for _, m := range all {
		if err := DB.AutoMigrate(m); err != nil {
			if isTolerableMigrateError(err) {
				if !DB.Migrator().HasTable(m) {
					createTableFallback(m)
				}
				tolerated++
				continue
			}
			panic(err)
		}
	}

	if missing := missingTables(DB, all...); len(missing) > 0 {
		panic(fmt.Sprintf("AutoMigrate 终校验失败：以下模型对应的数据表缺失（可能被可容忍错误静默吞掉，需排查建表失败根因）：%s", strings.Join(missing, ", ")))
	}

	if tolerated > 0 {
		logger.Warn("AutoMigrate 完成，但存在可容忍的迁移漂移（历史约束命名不一致），建议排查并清理遗留历史表，避免长期静默漂移掩盖真实故障")
	} else {
		logger.Info("AutoMigrate 完成，无迁移漂移")
	}

	postMigrateMessageHubUniqueIndex()
	postMigrateOpportunityClueUniqueIndex(DB)
	postMigrateObsDefaultUniqueIndex(DB)

	return DB
}

// verifyUniqueIndex 在报"已就绪"之前读一次库里的**形状**：返回空串表示那枚索引确实唯一，
// 否则返回一句可以直接落日志的归因（含索引名，三条钩子同族、文案相近，不点名运维不知道该动谁）。
//
// 为什么 `CREATE UNIQUE INDEX IF NOT EXISTS` 的 err 不足以支撑"已就绪"这句话（N-36，实测）：
// PG 的 `IF NOT EXISTS` 只看**名字** —— 名字被一枚非唯一的历史索引占住时整句是静默 no-op
// （err=<nil>、indisunique=false、同键两行照落）。标签那一层也补不回来，GORM 同样按名字对账
// （`driver/postgres@v1.6.0/migrator.go:109` 的 HasIndex 只查 pg_indexes.indexname）。
// 三条钩子存在的全部理由就是"第二层没铺上要在启动日志里看得见"，
// 只看 err 恰好把这一格报成"铺上了" —— 日志说反话比没有日志更坏。
//
// 为什么形状不对也只报不修（不 DROP 那枚同名索引再重建）：非唯一索引可能是有人为读路径特意建的，
// 启动期替运维决定删它是拿一个可用性隐患换一致性隐患；且这三道守卫的既有口径就是"只报不改"。
//
// 索引名只按名字查、不带表条件：PG 里索引名在 schema 内唯一，同名对象不可能同时属于两张表。
func verifyUniqueIndex(db *gorm.DB, indexName string) string {
	var isUnique bool
	if err := db.Raw(`SELECT i.indisunique
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = ic.relnamespace
		WHERE ic.relname = ? AND n.nspname = current_schema()`, indexName).Row().Scan(&isUnique); err != nil {
		return fmt.Sprintf("%s 在库里不是唯一索引（DDL 报成功之后读不到它的形状: %v）", indexName, err)
	}
	if !isUnique {
		return fmt.Sprintf("%s 在库里不是唯一索引（名字被一枚非唯一的历史索引占住，CREATE UNIQUE INDEX IF NOT EXISTS 对它是静默 no-op）", indexName)
	}
	return ""
}

// postMigrateOpportunityClueUniqueIndex 给"一条线索最多只有一个商机"这条承诺配上库级守卫。
//
// 句柄作参数而不是读包级 DB（与上面那条 message_hub 钩子的唯一差别）：这条钩子有真库用例
// 要跑它，而用例若只能靠 SetTestDB 把全局句柄换掉才能生效，就等于把"全二进制共享一个全局"
// 那类顺序依赖引进来了。生产调用点仍然传 DB，形状没变。
//
// 为什么是裸 DDL 而不是 struct tag：GORM 的索引标签表达不了 **partial**（带 WHERE 谓词）。
// 而这里必须有谓词 —— opportunities.clue_id 的空串是合法常态（手工建的商机就没有来源线索），
// 不带 WHERE 的唯一索引会让第二条手工商机当场插不进去：一个把合法形状拦在门外的约束，
// 比没有约束更坏，因为它逼着人往里写假线索号。
//
// 为什么建不成只 Warn 不 panic：本表 T-P4-01 就登记进了 allModels()，已部署的库里
// 可能已经存在重复的 clue_id（早期由旁路写入的行），CREATE UNIQUE INDEX 撞上就会失败。
// 建不成的后果是"重复转化没有库级兜底"，而 panic 的后果是"整个服务起不来" ——
// 前者今天仍由 service 侧 GetByClueID 那道幂等判据兜着（它在，索引是第二层）。
// 报出来是为了让"第二层没铺上"这件事在启动日志里看得见，而不是等重复行出现才发现。
func postMigrateOpportunityClueUniqueIndex(db *gorm.DB) {
	if db == nil {
		return
	}
	const ddl = `CREATE UNIQUE INDEX IF NOT EXISTS idx_opportunities_clue_id
		ON opportunities (clue_id) WHERE clue_id <> ''`
	if err := db.Exec(ddl).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: CREATE idx_opportunities_clue_id 失败(存量里可能有重复 clue_id，需人工清重后重启): %v", err))
		return
	}
	if why := verifyUniqueIndex(db, "idx_opportunities_clue_id"); why != "" {
		logger.Warn("post-migrate: " + why + " ⇒ 同线索转化的库级兜底并未生效，需人工 DROP 那枚同名非唯一索引后重启")
		return
	}
	logger.Info("post-migrate: opportunities 非空 clue_id 唯一索引已就绪（手工商机的空 clue_id 不受约束）")
}

// postMigrateObsDefaultUniqueIndex 给"全站最多一条默认存储"配库级守卫（批M / N-26）。
//
// 为什么必须有这一道：obs_config 的 is_default 是**选取判据**而不是普通布尔列 ——
// 一旦同时有两条为真，GetDefault 的 First 就退化成按 uuid 主键序随便挑一条，
// 而主键是随机串，等于每次进程重启都可能换一台存储：媒体转存、素材上传会分别
// 落到两台桶上，两边都能"成功"，只有取回时才露馅。应用层的两条 UPDATE 挡不住
// 旁路写入（管理页并发、SQL 运维、别家 service 整行 Save 的陈旧快照）。
//
// 为什么是裸 DDL 而不是 struct tag：GORM 的索引标签表达不了 **partial**（带 WHERE 谓词）。
// 这里必须有谓词 —— is_default=false 的行是绝大多数，不带 WHERE 的唯一索引会把
// 第二台"非默认"存储直接拦在创建门外，那是比双默认更坏的后果。
//
// 为什么先清重再建索引（与 clue_id 那条不同）：那条的重复只可能来自历史旁路写入，
// 清它需要人判断留哪一条；这里的重复**语义上必然有一行是错的**，而"哪一行是对的"
// 有唯一不自相矛盾的答案 —— 保留 GetDefault 本来就会选中的那一行（最早 created_at，
// 同值按 id 升序），因为那是存量文件已经实际落在的那台。
// 降级动作只在真的出现重复时才发生，并原样报出条数。
func postMigrateObsDefaultUniqueIndex(db *gorm.DB) {
	if db == nil {
		return
	}
	var dups int64
	if err := db.Table("obs_config").Where("is_default = ?", true).Count(&dups).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: 统计 obs_config 默认行失败(跳过唯一索引): %v", err))
		return
	}
	if dups > 1 {
		res := db.Exec(`UPDATE obs_config SET is_default = false
			WHERE is_default AND id <> (
				SELECT id FROM obs_config WHERE is_default ORDER BY created_at ASC, id ASC LIMIT 1)`)
		if res.Error != nil {
			logger.Warn(fmt.Sprintf("post-migrate: obs_config 双默认清重失败(跳过唯一索引): %v", res.Error))
			return
		}
		logger.Warn(fmt.Sprintf("post-migrate: obs_config 有 %d 条默认存储，已保留 GetDefault 会选中的最早一条、降级 %d 条", dups, res.RowsAffected))
	}
	const ddl = `CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_config_single_default
		ON obs_config (is_default) WHERE is_default`
	if err := db.Exec(ddl).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: CREATE idx_obs_config_single_default 失败: %v", err))
		return
	}
	if why := verifyUniqueIndex(db, "idx_obs_config_single_default"); why != "" {
		logger.Warn("post-migrate: " + why + " ⇒ 双默认的库级兜底并未生效，需人工 DROP 那枚同名非唯一索引后重启")
		return
	}
	logger.Info("post-migrate: obs_config 单默认偏唯一索引已就绪（is_default=false 的行不受约束）")
}

func postMigrateMessageHubUniqueIndex() {
	if DB == nil {
		return
	}
	if err := DB.Exec(`ALTER TABLE message_hub DROP CONSTRAINT IF EXISTS uni_message_hub_msg_id`).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: DROP 旧 uni_message_hub_msg_id 失败(可忽略若已不存在): %v", err))
	}

	if err := DB.Exec(`DROP INDEX IF EXISTS uni_message_hub_msg_id_conv`).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: DROP 旧 uni_message_hub_msg_id_conv 失败: %v", err))
	}
	if err := DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uni_message_hub_platform_msg_conv ON message_hub (platform, msg_id, conversation_id)`).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: CREATE uni_message_hub_platform_msg_conv 失败: %v", err))
	} else if why := verifyUniqueIndex(DB, "uni_message_hub_platform_msg_conv"); why != "" {
		logger.Warn("post-migrate: " + why + " ⇒ 三元组去重的库级兜底并未生效，需人工 DROP 那枚同名非唯一索引后重启")
	} else {
		logger.Info("post-migrate: message_hub (platform, msg_id, conversation_id) 三元组唯一索引已就绪")
	}
}

func isTolerableMigrateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "does not exist") && strings.Contains(msg, "constraint") {
		logger.Warn("AutoMigrate 命中历史约束命名漂移(可容忍，建议清理历史表): " + msg)
		return true
	}
	if strings.Contains(msg, "already exists") {
		logger.Warn("AutoMigrate 命中幂等重跑提示(可容忍): " + msg)
		return true
	}
	return false
}

func missingTables(db *gorm.DB, models ...any) []string {
	var missing []string
	for _, m := range models {
		if db == nil {
			missing = append(missing, fmt.Sprintf("%T", m))
			continue
		}
		if !db.Migrator().HasTable(m) {
			missing = append(missing, fmt.Sprintf("%T", m))
		}
	}
	return missing
}

func tableNameOf(db *gorm.DB, m any) string {
	stmt := &gorm.Statement{DB: db, Dest: m}
	if err := stmt.Parse(m); err != nil {
		logger.Warn(fmt.Sprintf("解析模型表名失败 %T: %v", m, err))
		return ""
	}
	return stmt.Table
}

func createTableFallback(m any) {
	if tn := tableNameOf(DB, m); tn != "" {
		DB.Exec(fmt.Sprintf("DROP TYPE IF EXISTS %s CASCADE", tn))
	}
	if cerr := DB.Migrator().CreateTable(m); cerr != nil && !isTolerableMigrateError(cerr) {
		panic(fmt.Sprintf("AutoMigrate 兜底 CreateTable(%T) 失败: %v", m, cerr))
	}
	if DB.Migrator().HasTable(m) {
		logger.Warn(fmt.Sprintf("AutoMigrate 兜底 CreateTable 已重建缺失表: %s", tableNameOf(DB, m)))
	} else {
		panic(fmt.Sprintf("AutoMigrate 兜底 CreateTable(%T) 后表仍缺失，需排查建表失败根因", m))
	}
}

// extraModels 允许其他包（如 service）注册自有模型，统一走启动期 AutoMigrate，
// 替代散落在业务路径上的 AutoMigrate 调用（迁移漂移治理：单轨收敛）。
var extraModelsMu sync.Mutex

var extraModels []any

// RegisterExtraModels 由各包在 init() 中注册模型，需在 AutoMigrate() 之前完成。
func RegisterExtraModels(models ...any) {
	extraModelsMu.Lock()
	defer extraModelsMu.Unlock()
	extraModels = append(extraModels, models...)
}

// ExtraModels 返回已注册的外部模型列表。
func ExtraModels() []any {
	extraModelsMu.Lock()
	defer extraModelsMu.Unlock()
	out := make([]any, len(extraModels))
	copy(out, extraModels)
	return out
}
