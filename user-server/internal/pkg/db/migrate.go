package db

import (
	"context"
	"fmt"
	browsermodel "hivemtk-user/internal/browser_automation/model"
	contentmodel "hivemtk-user/internal/content/model"
	guardmodel "hivemtk-user/internal/contentguard/model"
	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"strings"
	"sync"

	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"

	"gorm.io/gorm"
)

func allModels() []any {
	return []any{
		&model.Tenant{},
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
		// EmailTrackingEvent（表 email_tracking_events）：邮件追踪事件
		// （open / click / unsubscribe / bounce）。生产写路径三条：
		// service/email_tracking.go:200、service/email_open_tracker.go:200 与 :268
		// （后两条在**公开的追踪像素**回调路径上——每封邮件的打开/点击都走这里）。
		// 此前未登记建表 ⇒ 全新部署没有这两张表 ⇒ 追踪事件写不进去
		// ⇒ 打开率/点击率恒为 0，而调用处只把 error 往上抛给一个没人看的 webhook。
		// event_id 的 uniqueIndex 经 tenancyidx.Normalize 复合化为
		// (tenant_id, event_id)——幂等键是租户内的。
		&model.EmailTrackingEvent{},
		// EmailJobMetric（表 email_job_metrics）：每个邮件任务一行的指标汇总
		// （发送/打开/点击/退信/退订计数 + 打开率/点击率），生产写路径是
		// service/email_tracking.go:322 的 UpsertJobMetric。同一组缺失建表登记：
		// 没有这张表，任务列表里的打开率/点击率两列永远读成 0（不是「没打开过」，
		// 是「表不存在」）。job_id 的 uniqueIndex 同样复合化为 (tenant_id, job_id)。
		&model.EmailJobMetric{},
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
		&model.RoleMenuPermission{},
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
		&model.CustomTool{},
		&model.MCPServer{},
		&model.DialogueEvalRun{},
		&model.DialogueEvalSet{},
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
		// WechatAccount（表 wechat_accounts）/ WechatMessage（表 wechat_messages）：
		// 微信公众号接入（公众号账号管理 + 消息收发）。
		// 生产链路完整：router/platform_routes.go:84 与 router/router.go:738
		// （/api/webhook/wechat/:account_id）→ controller/wechat.go（12 个端点）
		// → service/wechat.go → repository/wechat_account_repo.go。
		// 此前未登记建表，且没有任何迁移创建这两张表（迁移目录里搜不到
		// wechat）⇒ 全新部署上整个微信公众号接入全部 500：
		// relation "wechat_accounts" does not exist。
		// 登记后由 AutoMigrate 建表；app_id / msg_id 的 uniqueIndex 经
		// tenancyidx.Normalize 复合化为 (tenant_id, app_id) / (tenant_id, msg_id)。
		&model.WechatAccount{},
		&model.WechatMessage{},
		&model.TelegramAccount{},
		&model.QQAccount{},
		&model.TelegramGroupGate{},
		&model.TelegramGroupMember{},
		&model.CronJobLease{},
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
		// CustomerRFM（表 customer_rfm）：客户 RFM 分层记录，生产写路径是
		// service/customer_rfm.go 的两处 rfmRepo.Upsert（207/295 行），另有
		// service/customer_rfm_cron.go 的 RFM 周期重算。
		// 此前未登记建表 ⇒ 全新部署没有这张表 ⇒ RFM 分层整体不落库
		// （RFM 规则命中、流失候选名单都读成空），而 Upsert 的错误在 cron 里
		// 只记一行日志 ⇒ 客户分层看起来「没数据」，实际是「表不存在」。
		// 登记后 AutoMigrate 建表；customer_id 的单列 uniqueIndex 经
		// tenancyidx.Normalize 复合化为 (tenant_id, customer_id)——一个客户在每个
		// 租户内一条 RFM 行。表名是单数 customer_rfm（模型里显式 TableName），
		// module_tables.go 里同时登记的 "customer_rfms" 是复数写法（遗留）。
		&model.CustomerRFM{},
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
		&contentmodel.AIGenerationRecord{},
		&contentmodel.PromptTemplate{},
		&model.UnifiedMessage{},
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
		&model.HumanizeScore{},
		&model.HumanizeDimensionRecord{},
		&model.ChampionBaseline{},
		&model.ChampionPhrase{},
		&model.FeedbackEvent{},
		&model.FeedbackSignal{},
		&model.ChampionDialogue{},
		&model.PromptCandidate{},
		&model.BanditArm{},
		&model.PromptABTest{},
		&model.ReachPipeline{},
		&model.ReachJob{},
		// R-8 合规审计：落库器在 main 装配（InitComplianceAuditLogger），表必须随全量
		// AutoMigrate 建出来，否则首次刷盘整批报"relation does not exist"后回队重试。
		&model.ReachComplianceLog{},
		&model.ScriptLibrary{},
		&model.ScriptVersion{},
		&model.ScriptExposureLog{},
		&model.FeatureFlag{},
		&model.FeatureFlagAuditLog{},
		&model.FeatureFlagEvalLog{},
		// R39-K7/K8 三面板收编（业务闭环 C-6）：模板两只。漏登记时全新部署
		// 建不出这两张表，r39_panel_routes 的端点全数 500（relation does not exist）。
		&model.KbTemplate{},
		&model.DashboardTemplate{},
		// 功能模块化（specs .specify/002-modular-subscription）：
		// 租户模块本地镜像 + 同步日志 + 订单镜像。
		// 漏登记时全新部署建不出这三张表，而模块闸是**启动即读**的：
		// 读不到就退化成"闸未启用"，表现为已付费模块全量放行 —— 收费模式静默失效。
		&model.TenantModule{},
		&model.ModuleSyncLog{},
		&model.ModuleOrder{},
		// 订阅镜像（003 T022）：显式迁移 v3.64.0 是生产口径，
		// 这里登记供 AutoMigrate 兜底（全新部署先于迁移跑时建表）。
		&model.TenantSubscriptionMirror{},
		&model.LLMWallet{},
		&model.LLMWalletTxn{},
		&model.PlatformLLMUsageRecord{},
		&model.ObjectionTemplate{},
		&model.ConversionFunnel{},
		&model.SalesPersona{},
		&model.SalesEvent{},
		&opsmodel.PerformanceTestResult{},
		&knowledgemodel.KnowledgeDocument{},
		&knowledgemodel.KnowledgeChunk{},
		&knowledgemodel.KnowledgeChunkParent{},
		&knowledgemodel.KnowledgeImportLog{},
		&knowledgemodel.KnowledgeSearchLog{},
		&knowledgemodel.KnowledgeOpenAPISource{},
		&knowledgemodel.KnowledgeAPIToken{},
		&knowledgemodel.KnowledgeFeedback{},
		&knowledgemodel.ExternalImportJob{},
		// KnowledgeBase（表 knowledge_bases）：知识库**主表**，有完整五层链路
		// （controller/knowledge_base.go 12 个端点 → service/knowledge_base.go
		// → repository/knowledge_base.go → 本 model），还有一条**跨域生产读路径**：
		// RAG 答案缓存的 pgKBMetaReader.GetKBUpdatedAt（internal/aiagent/rag/cache/
		// store_pg.go:46）按表名查 knowledge_bases.updated_at，命中前用它判缓存新鲜度。
		// 此前**从未登记建表**：开发库里的 knowledge_bases 是 v3.15 知识库统一迁移
		// 留下的残留，而本仓生产建表只跑 AutoMigrate ⇒ 全新部署没有这张表 ⇒
		// 答案缓存每次命中前都报 relation does not exist（service.go:108 直接返回 err），
		// 缓存整层不可用；而知识库管理端的增删改查全部撞同一张不存在的表。
		// 登记后 AutoMigrate 建表；UNIQUE(kb_code) 经 tenancyidx.Normalize 复合化为
		// (tenant_id, kb_code)——kb_code 是租户内唯一，不是全实例唯一。
		&model.KnowledgeBase{},
		// RAGAnswerCache（表 rag_answer_cache）：语义答案缓存行，读写方只在
		// internal/aiagent/rag/cache/store_pg.go；唯一命名空间是 (kb_id, prompt_version)。
		&model.RAGAnswerCache{},
		&model.AIAgent{},
		&model.ChannelAgentBinding{},
		// AgentKBBinding（表 agent_kb_bindings）：智能体 ↔ 知识库 多对多绑定，
		// 有两条生产写入路径——service.AgentKBBindingService.Bind / BatchBind，
		// 以及 repository.KnowledgeBaseRepository.CreateWithBinding（建私有知识库时
		// 在同一事务里写 kb 行 + 绑定行）。此前**从未登记建表**：开发库里的
		// agent_kb_bindings 是知识库统一迁移留下的残留，而本仓生产
		// 建表只跑 AutoMigrate（启动期 ExecuteUpgrade 固定空跑）⇒ 全新部署根本没有
		// 这张表 ⇒ Bind 报 relation does not exist，私有知识库创建整个事务回滚。
		// 登记后 AutoMigrate 建表，tenancyidx.Normalize 把 UNIQUE(agent_id, kb_id)
		// 复合化为 (tenant_id, agent_id, kb_id)——业务唯一性是租户内的。
		&model.AgentKBBinding{},
		// KBItemBinding（表 kb_item_bindings）：知识库成员表（P0-2/P0-3/D-8 方案 A）。
		// 此前知识库与内容（FAQ/SOP/文档）之间没有成员关系：内容表无 kb_id、按 owner
		// agent 反推交集为空，前端抽屉的 kb_id 参数被 List 静默忽略、rag 分支请求的
		// /api/knowledge 恒 404。本表把"归属"建成独立的多对多关系（UNIQUE(kb_id,
		// item_type, item_id) 经 tenancyidx.Normalize 复合化为租户内唯一），
		// 支持一条内容属多个 KB（shared 语义），内容大表不动，允许"未分配"长期存在。
		&model.KBItemBinding{},
		&model.CustomerServiceAgent{},
		&model.LLMProvider{},
		&model.LLMRoutingRule{},
		&model.FAQEntry{},
		&model.SOPTemplate{},
		&model.LayerDecisionLog{},
		&model.ChatChannel{},
		&model.Notification{},
		&model.NotificationRead{},
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
		// Glossary（表 glossaries）：多语言术语表（保护 SKU/价格/品牌名不被 LLM 翻译）。
		// 生产链路完整：router/system_routes.go 装配 → controller/glossary.go
		// （6 个端点）→ service/translation/glossary.go → repository/glossary_repo.go。
		// 另有跨域消费方：app/sales_engine_factory.go 把它注入 SalesEngine 的
		// GlossaryRenderer + OutputCalibrator（LLM 输出后置校准）。
		// 此前未登记建表 ⇒ 全新部署没有这张表 ⇒ 术语管理端全挂，且 Calibrate
		// 每次都拿到 list active failed。
		// 登记后 AutoMigrate 建表；本轮同时给这张表补了 tenant_id（术语是租户
		// 业务数据），TermID 的 uniqueIndex 经 tenancyidx.Normalize 复合化为
		// (tenant_id, term_id)——同一个 term_id 在不同租户各有一条。
		&model.Glossary{},
		&model.FeedbackRecordORM{},
		&model.LeadMiningConfig{},
		&model.SecurityAudit{},
		&model.SecurityAuditItem{},
		&model.UserBlacklist{},
		&model.BridgeAccount{},
		// B1-a：桥接选择器健康度计数（扩展上报「试了几次/命中几次」，服务端按阈值判
		// degraded/broken）。不登记建表 ⇒ 全新部署上上报口每次都 500，而扩展侧只会在
		// 自己的日志里记一行——运营面永远看不到平台改版导致的静默失效。
		&model.BridgeSelectorHealth{},
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
		&geomodel.GeoCitationSnapshot{}, // A10 引用漂移快照
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
		&geomodel.GeoSeedTerm{},
		&geomodel.GeoDict{},

		// GeoCrawlerVisit 此前是 29 个 geo 模型里**唯一没登记**的：
		// internal/router/router.go 的 AICrawlerMonitor 回调会 fire-and-forget 地写入它，
		// 而该写入的 error 又被 `_ =` 丢弃 —— 全新部署不建表 ⇒ 数据永久为 0 且毫无报错。
		// 现在补进清单，建表不再依赖"遗留库里恰好有这张表"。
		&geomodel.GeoCrawlerVisit{},

		// -------------------------------------------------------------------
		// 补齐「有生产写入路径、却从未登记建表」的模型。
		//
		// 这些模型的表在开发库里确实存在，但那只是遗留（曾有测试把模型
		// AutoMigrate 进了 user_db，见 TEST-06）；它们既不在 allModels()、
		// 也不在 internal/migration/migrations、也不在 migrations/*.sql 里。
		// 因此**全新部署不会建这些表** → 写入失败 → 而多数写入点的 error
		// 又被 `_ =` 丢掉 → 表现为"功能静默失效"。
		//
		// 以下 29 个已逐个核对：均有 repository/存储层的 Create/Save 写入路径。
		// 另有一个因 import 成环无法写在此处，改用 RegisterExtraModels 登记：
		//   - KBDocumentChunkRow（internal/repository，见该文件 init）
		// -------------------------------------------------------------------
		// trace_events 由 DBTraceSink 异步批量落库，中间件在每请求发布 span 事件。
		&model.TraceEvent{},
		&model.AggregationWatermark{},
		// email_accounts：邮件账号由 EmailService 读取、仓储写配额计数；模型原在 service 包，
		// 靠 init() 的 RegisterExtraModels 绕环登记，现落到 internal/model 正常进清单。
		&model.EmailAccount{},
		// msg_hourly_summary 由 message_hub 聚合 cron 写入（repository 原子 upsert）。
		// 此前只出现在测试夹具里，全新部署缺表 ⇒ 聚合永远写不进去。
		&model.MsgHourlySummary{},
		&model.AlertHistory{},
		&model.AlertRule{},
		// ApprovalRequest（表 approval_requests）：T-P3-01 / N-4 审批检查点，
		// 写入路径是 repository.approvalRequestRepo.Insert。
		// 本处交付的是底座、尚未装配（装配卡 T-P3-02/T-P3-07），但**建表登记必须在
		// 今天这一批**：登记晚一卡，中间那段部署里闸门第一次落库就会往不存在的表里写
		// （T-P1-08 在 tool_call_audits 上正是这个形状：只接 logger 不登记表 = 整批静默降级）。
		&model.ApprovalRequest{},
		&model.BanditRefluxLog{},
		&model.ChurnScore{},
		&model.ClueEngagementEvent{},
		&model.ClueScore{},
		// ConfigParam（表 config_params）：参数中心主表。SeedConfigParams 启动即
		// First() 探测它——漏登记时全新部署建不出表，seed 直接失败，**参数中心
		// 全量失效**（所有 Get* 走代码兜底值，意图锚点预计算、线索阈值等整批降级）。
		// 审计表 ConfigParamAuditLog 在下面有登记，主表只靠开发库遗留存在。
		// MCPCredential（表 mcp_credentials）：MCP 工具入口的 ClientID+APIKey 凭证对。
		&model.MCPCredential{},
		// ExternalKBConnector / ExternalKBSyncItem：第三方知识库连接器与同步幂等明细。
		&model.ExternalKBConnector{},
		&model.ExternalKBSyncItem{},
		&model.ConfigParam{},
		&model.ConfigParamAuditLog{},
		&model.ConfigParamGroup{},
		// 租户参数覆盖与审计（SaaS 多租户）：定义仍在上面的实例级 config_params，
		// 本表按 tenant_id 分域。漏登记时 AutoMigrate 建不出表，租户管理端保存参数
		// 会一直报“存储不可用”，而读取侧会静默回落到平台默认值。
		&model.TenantConfigParam{},
		&model.TenantConfigParamAuditLog{},

		// 业务字典（阶段四）：与 config_params 同级登记，AutoMigrate 建表。
		&model.Dictionary{},
		&model.DictionaryTransition{},
		&model.CustomerChannel{},
		// HumanTask（表 human_tasks）：T-P3-03 / N-9 统一人工待办，三类 kind 共用一张表。
		// 写入路径有两条且都已接线：repository.humanTaskRepo.Insert（转人工投递）与
		// ApplyAction（坐席认领/释放/完成/撤销、会话结束时的系统撤销）。
		// 与 ApprovalRequest 同一条理由：登记晚一卡，第一批流量就会往不存在的表里写。
		&model.HumanTask{},
		&model.IntegrationTemplate{},
		&model.IntentExample{},
		// T-P9-02 知识库发布制三张表。kb_releases 是**读路径的依赖**（召回侧的
		// NOT EXISTS 子查询按它判定），没建出来时闸门形同虚设且不报错，
		// 所以它与另两张表都必须在这里登记，见 migrate_test.go 里那条理由。
		&model.KBChangeAuditLog{},
		&model.KBChangeRequest{},
		&model.KBRelease{},
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
		// Payment（表 payments）：T-P7-02 / N-6 回款域第二层，bills 那张"应收"的**唯一操作数**。
		// 登记理由与 Bill 同一条而更静：入账是渠道回调驱动的，表没建时对面没有人在看着报错，
		// 只有重试三轮之后的一句 Warn —— 而后果不是"少一行记录"，是**这张应收永远收不齐**
		// （settled 恒 0、状态永远停在 open），逾期扫描与赢单跃迁（T-P7-03/04）读的正是那个差。
		&model.Payment{},
		&model.RagMetricsDaily{},
		// Quote / QuoteLineItem（表 quotes / quote_line_items）：T-P6-01 / N-5 报价域第一层。
		// 与 Opportunity 同一句理由登记：本处只交付列与索引形状，生成方在 T-P6-02。
		// 这里少登记一张会是一种**不对称**的坏法：quotes 建了、明细没建 ⇒ 报价存得下、
		// 行项目写不进去，而"合计对不对"的用例连不上明细表时是 Skip 不是 Fail。
		&model.Quote{},
		&model.QuoteLineItem{},
		// BadCase（表 bad_cases）：T-P8-03 / G-2。卡面写的 `v3_51_0_bad_case_migration.go`
		// 因此不产出 —— 建表只走这一处登记（与 Bill 同一口径）。
		// 登记理由与待办那一条同形且更硬：坏例是**整条闭环的唯一事实源**，表没建出来时
		// 对话照样回答、日志照样打点，只有"这一轮低质被记下了"这件事静默消失，
		// 而它下游是评测集 —— 少记的那天基线分数不会红，只会悄悄变成另一件事的分数。
		&model.BadCase{},
		// Bill（表 bills）：T-P7-01 / N-6 回款域第一层。
		// 卡面写的 `v3_50_0_bill_migration.go` 因此不产出 —— 本仓生产建表只跑 AutoMigrate，
		// 启动期 ExecuteUpgrade 固定空跑（与本文件头部那八次同源）。
		// 登记理由与 Quote 同一条：这张表的失败面是"表没建、代码全对"，
		// 而应收是**给钱的那张纸**：漏建表的那天销售照样点"客户已接受"，
		// 报价状态真的变成 accepted，只有账单这一侧在日志里留一句话。
		&model.Bill{},
		&model.RecoveryQueue{},
		&model.SecurityAlert{},
		// SystemConfigKV（表 system_config_kv）：租户业务 KV，主键 (tenant_id, key)。
		// PlatformConfigKV（表 platform_config_kv）：平台级 KV（桥接/MCP/回调验签/
		// 宿主令牌、口令策略、安全规则、备份策略），实例一份，不参与租户分域。
		// 两张表都必须登记：漏建 system_config_kv 时租户配置写不进去（管理台报
		// 存储不可用）；漏建 platform_config_kv 时桥接/MCP/回调验签全部静默拒绝。
		&model.SystemConfigKV{},
		&model.PlatformConfigKV{},

		// ToolCallAudit（表 tool_call_audits）：T-P0-04 判定的"实现了但没接线"里最隐蔽的一条
		// —— 写入方 tooluse.DBAuditLogger 一直存在，但表**从未登记建表**（它自带的
		// AutoMigrateAuditTable 零调用），且 check_model_migration.py 因为"文件里含
		// .AutoMigrate("而把它误判成已登记。开发库里没有这张表。
		&model.ToolCallAudit{},

		&model.UserMFA{},
		&model.WorkflowExecution{},
		&model.WorkflowNodeExecution{},
		&model.WorkflowVersion{},

		// BrowserAuditDigest / BrowserAuditPruneRun（批22 / A6）：command_log 裁剪前的内容摘要
		// 与每次扫描的留痕。这两张表是「删掉的那段」唯一的凭据，本身永不裁剪；
		// 没建出来的失效方向是 fail-close（PruneBefore 写不进摘要就整批回滚，一行都不删），
		// 表现为治理任务天天告警，而不是无声消失。
		&browsermodel.BrowserAuditDigest{},
		&browsermodel.BrowserAuditPruneRun{},
		// BrowserOutreachDedupe（表 browser_outreach_dedupe）：跨任务用户级触达去重行
		// （同平台同目标同动作同文案只触达一次），生产写路径是
		// browser_automation/service/outreach_dedupe.go:113 的 recordOutreachDedupeSend。
		// 此前**从未登记建表**：开发库里那张表是测试夹具（testutil.NewTestDB 直接
		// AutoMigrate 这个 model）留下的，而本仓生产建表只跑 AutoMigrate ⇒ 全新部署
		// 没有这张表 ⇒ 落去重行时 relation does not exist，而那一处**只 Warn 不判红**
		// （outreach_dedupe.go:121「内容已发布，仅记日志」）⇒ 跨任务去重整层静默失效，
		// 用户被重复触达，且日志里只有一行 warn。读侧 FindOutreachHit 同样查不到表。
		// 登记后 AutoMigrate 建表；uk_outreach_dedupe 经 tenancyidx.Normalize 复合化为
		// (tenant_id, platform, target_url, action, copy_hash)——去重是租户内的。
		&browsermodel.BrowserOutreachDedupe{},
		// BrowserOutreachReceipt（表 browser_outreach_receipts）：触达回执
		// （截图 + 帖子链接 + 文案快照），生产写路径是
		// browser_automation/service/outreach_receipt.go:159 的 RecordOutreachReceipt。
		// 与去重表同一条理由：此前未登记建表 ⇒ 全新部署没有这张表 ⇒ 回执行写不进去，
		// 而那一处同样只 Warn 不判红（outreach_receipt.go:160）⇒ 「到底发出去了没有」
		// 这件事在全新部署上永远查不到行，而日志里只有一行 warn。
		// 它同时是去重行的对账面（模型文件头第 19-20 行）：「有去重行无回执」=
		// 收口截图或回执写入失败，两边都可见——而这张表不存在时这个对账面整个不存在。
		&browsermodel.BrowserOutreachReceipt{},
		// BrowserProfileHealth（表 browser_profile_health）：单主 Profile 的平台级
		// 熔断行（blocked=true 即同平台新任务拒绝/排队，人工恢复清标记）。生产写路径
		// 两条：service/profile_fuse.go:25 的 MarkBlocked（熔断）与
		// service/task.go:517 的 MarkRecovered（人工恢复）。
		// 此前未登记建表 ⇒ 全新部署没有这张表 ⇒ 熔断状态落不下去
		// （熔断是风控闸，落不下去等于闸门永远不关）；而熔断状态**必须**落库，
		// 模型文件头第 8 行明说「Executor 进程级单例禁内存计数」。
		// 登记后 AutoMigrate 建表；platform 的单列 uniqueIndex 经 tenancyidx.Normalize
		// 复合化为 (tenant_id, platform)——「每平台一行」是**每租户每平台**一行。
		&browsermodel.BrowserProfileHealth{},
		&browsermodel.BrowserCommandLog{},
		&browsermodel.BrowserCronTrigger{},
		&browsermodel.BrowserLLMPlan{},
		&browsermodel.BrowserSession{},
		&browsermodel.BrowserStep{},
		&browsermodel.BrowserTask{},
		// BrowserWriteClaim（批20f / A12）：双发闸的存储层独占声明。这张表没建出来
		// 不是「少一张审计表」，是写步在 ClaimWriteSlot 处第一条 INSERT 就报错——
		// 而报错方向恰好是 fail-close 的那一侧（所有写步判红），所以漏登记会在
		// 「功能全废」而不是「闸门静默失效」上暴露。建表走 allModels 而不是迁移文件
		// （本泳道口径：**新表**由标签直建、**存量表加列**走版本化迁移，后者见 v3.43.0 的
		// browser_steps 三列——两者不是同一条路，别拿这句去省存量表的 DDL 文件）。
		&browsermodel.BrowserWriteClaim{},
		// ContentGuard 内容安全闸五张表（moderation 域，见 contentguard/model）：
		// guard_words 词库、guard_policies 主体策略、guard_tasks 异步审查队列、
		// guard_records 审查结论、guard_sanctions 冻结/黑名单。
		// 为什么必须登记：这套表的生产写入路径在 worker 里（service.Pipeline.processOne），
		// 表没建出来时消息照常收发、日志照常打，只有"审了没有、罚了没有"静默消失——
		// 而那正是平台承担法律风险的那一格。
		&guardmodel.GuardWord{},
		&guardmodel.GuardPolicy{},
		&guardmodel.GuardTask{},
		&guardmodel.GuardRecord{},
		&guardmodel.GuardSanction{},

		// T3 观测报表（LLMUsageDaily）：llm_usage_daily 日聚合表，写路径是
		// service/report_service.go 的聚合任务 upsert；漏登记 ⇒ 全新部署聚合写不进、
		// 报表 API 全部读空。表形状以 v3.83.0 迁移为准，此处登记供 AutoMigrate 兜底。
		&model.LLMUsageDaily{},
		// UnifiedReply（unified_replies）：统一回复行，与 UnifiedMessage 同域；写路径
		// 在统一消息回复链路，漏登记 ⇒ 全新部署回复落不了库。
		&model.UnifiedReply{},
		// T1 未知问题回流三表（unknown_questions / unknown_question_clusters /
		// unknown_question_review_logs）：低置信/未命中问题沉淀→聚类→审核→写回 FAQ 的
		// 全链路事实源，写路径在 service/reflux；漏登记 ⇒ 沉淀与聚类静默失效。
		&model.UnknownQuestion{},
		&model.UnknownQuestionCluster{},
		&model.UnknownQuestionReviewLog{},

		// ego-lite 腿四表（设计文档 §一）：物理新表由标签直建（本泳道口径：新表
		// allModels、存量表加列走版本化迁移）。漏登记的后果与 BrowserWriteClaim 同族——
		// 执行/事件/流程/写台账的 INSERT 全部报错（fail-visible，不是静默失效）。
		&model.EgoliteExecution{},
		&model.EgoEventLog{},
		&model.EgoliteFlow{},
		&model.EgoliteWriteClaim{},
		// 资产目录表（平台端发布 → 用户端使用 → 下发执行的中间事实源）：
		// user-server 自包含，不经 platform-server / binary_asset 通道。
		&model.EgoliteAsset{},
		// runner 令牌表（每用户多条，只存 sha256）：漏登记 ⇒ 签发/校验全挂，
		// 表现是"所有 runner 都连不上"，fail-visible。
		&model.EgoliteRunnerToken{},
		// 流程定时触发器（specs/016 S10）：漏登记 ⇒ cron 挂载/触发记账全挂，fail-visible。
		&model.EgoliteFlowCron{},
		// 通用执行能力（2026-10-28）：会话 + 轮次账本。漏登记 ⇒ 通用任务
		// Open/Act 全挂，fail-visible。
		&model.EgoliteSession{},
		&model.EgoliteRound{},
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

	// 清理"形状已漂移"的模块索引（必须在 AutoMigrate **之前**）。
	//
	// 为什么需要它：GORM AutoMigrate **只按名字判索引是否存在，不比对列集**。
	// 一个列集变过的索引（同名）会让 AutoMigrate 认为"已经有了"，
	// 于是永远停留在旧形状上。而 ON CONFLICT 依赖索引的**精确列集** ——
	// 匹配不上就是 42P10 → 模块下发全量失败 → tenant_modules 恒空 →
	// 模块闸落在"首启全放行"态 → **付费模块对所有人开放**。
	//
	// 为什么不能只靠 tenancyidx.Normalize：它的幂等判据是
	// `indexdef NOT LIKE '%tenant_id%'` —— 索引一旦含 tenant_id 就被**跳过**。
	// 所以 (tenant_id, tenant_slot, module_key) 这种"含 tenant_id 但列集不对"的
	// 形状，它永远不会修。
	//
	// 顺序：先删残留 → AutoMigrate 按当前模型重建 → tenancyidx 复合化加 tenant_id。
	// 三步之后形状才是 (tenant_id, 业务列) 且与 ON CONFLICT 一致。
	dropDriftedModuleIndexes(DB)

	// tenant_id 列类型归一化（同样必须在 AutoMigrate 之前）。
	// 详见 tenant_id_normalize.go 的文件头：varchar 的 tenant_id 会让
	// `WHERE tenant_id = 1` 直接报 "operator does not exist"，而 AutoMigrate
	// 因 DEFAULT 无法转换而中断整表迁移。
	normalizeTenantIDColumns(DB)

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

	// 租户唯一索引规范化（specs/001-saas-multi-tenant T008 框架层实现）：
	// AutoMigrate 按模型标签建的是单列唯一索引，行级隔离要求业务唯一性升级为
	// (tenant_id, 原列)。此处与迁移 v3.59.0 共用同一实现，幂等可重入；
	// 失败 fail-closed 拒绝启动——带病唯一约束等于重新打开跨租户冲突面。
	if n, err := tenancyidx.Normalize(DB); err != nil {
		panic(fmt.Sprintf("租户唯一索引规范化失败: %v", err))
	} else if n > 0 {
		logger.Info(fmt.Sprintf("租户唯一索引规范化完成：处理 %d 个索引", n))
	}

	// 租户登记表 + 隔离 callback（specs/001-saas-multi-tenant T005/T006）：
	// 登记表以数据库内省（表上有无 tenant_id 列）为唯一判据；callback 在 off
	// 模式直通、on 模式注入/填充/fail-closed。注册幂等（同名替换）。
	// 内省失败必须阻止启动：nil 登记表会让 on 模式把所有表判成非租户表 = 全量放行。
	if err := tenancyidx.InitTenantRegistry(DB); err != nil {
		panic(fmt.Sprintf("租户登记表装配失败（拒绝以 fail-open 状态启动）: %v", err))
	}
	tenancyidx.RegisterTenantCallbacks(DB)

	// 后台任务扇出的活跃租户枚举器（specs/001-saas-multi-tenant T020）：
	// 仅枚举正常状态的租户；停用租户的后台任务整体跳过（数据保留）。
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) {
		var ids []uint
		if err := DB.WithContext(ctx).
			Table("tenants").
			Where("status = ?", 1).
			Order("id ASC").
			Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		return ids, nil
	})

	postMigrateMessageHubUniqueIndex()
	postMigrateOpportunityClueUniqueIndex(DB)
	postMigrateObsDefaultUniqueIndex(DB)
	postMigrateDropLegacyPlaintextResetTokens(DB)
	// 排在终校验之后：它动的是**存量形状**（AutoMigrate 只加不删的那一半），
	// 表都没建出来时这一句只会得到一句被吞掉的告警，而那句话会被读成"旧索引删掉了"。
	postMigrateDropLegacyExternalOrderKey(DB)
	// customer_tags.id 历史上是 serial integer，模型自多租户改造起是 varchar(36)+UUID。
	// 形状不收敛时**任何**建标签/自动打标签都会在 INSERT 报
	// `invalid input syntax for type integer: "<uuid>"`（2026-10-27 逐路由实测）。
	postMigrateCustomerTagIDType(DB)

	return DB
}

// postMigrateCustomerTagIDType 把 customer_tags.id 收敛到模型声明的 varchar(36)。
//
// 为什么必须显式改：GORM AutoMigrate 不会把已有 integer 列自动改成 varchar
// （类型不同且列上有默认序列时它选择跳过），于是模型侧 BeforeCreate 生成的 UUID
// 写进 integer 列必然报错——「标签规则」与「自动打标签」两条链路在旧库上 100% 失败，
// 而错误原文是 PG 的类型转换错误，读起来像参数问题。
//
// 幂等：只在该列仍是整数族时动；已有数据用 `id::varchar` 无损转换（数字文本），
// 新数据走 UUID。序列默认值先摘掉，否则后续 INSERT 仍尝试 nextval 塞进 varchar。
func postMigrateCustomerTagIDType(db *gorm.DB) {
	if db == nil || !db.Migrator().HasTable("customer_tags") {
		return
	}
	var dataType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'customer_tags' AND column_name = 'id'`).
		Scan(&dataType).Error; err != nil || dataType == "" {
		return
	}
	if dataType != "integer" && dataType != "bigint" && dataType != "smallint" {
		return
	}
	// 走平台级豁免通道（与 password_reset_tokens 的 DDL 同类）：租户 SQL 守卫会把
	// 裸 Exec 当成"业务查询绕开 GORM"直接拒绝，迁移期 DDL 必须显式声明系统作用域。
	sysDB := db.WithContext(tenant.WithSystemScope(context.Background()))
	if err := sysDB.Exec(`ALTER TABLE customer_tags ALTER COLUMN id DROP DEFAULT`).Error; err != nil {
		logger.Warn("customer_tags.id 摘除序列默认值失败（继续尝试改类型）: " + err.Error())
	}
	if err := sysDB.Exec(`ALTER TABLE customer_tags ALTER COLUMN id TYPE varchar(36) USING id::varchar`).Error; err != nil {
		panic("customer_tags.id 类型迁移失败（integer → varchar(36)）: " + err.Error())
	}
	_ = sysDB.Exec(`DROP SEQUENCE IF EXISTS customer_tags_id_seq`).Error
	logger.Info("customer_tags.id 已从整数序列收敛为 varchar(36)（模型侧 UUID 主键）")
}

// verifyUniqueIndex 在报"已就绪"之前读一次库里的**形状**：返回空串表示那枚索引确实唯一，
// 否则返回一句可以直接落日志的归因（含索引名，三条钩子同族、文案相近，不点名运维不知道该动谁）。
//
// 为什么 `CREATE UNIQUE INDEX IF NOT EXISTS` 的 err 不足以支撑"已就绪"这句话：
// PG 的 `IF NOT EXISTS` 只看**名字** —— 名字被一枚非唯一的历史索引占住时整句是静默 no-op
// （err=<nil>、indisunique=false、同键两行照落）。标签那一层也补不回来，GORM 同样按名字对账
// （`driver/postgres@v1.6.0/migrator.go:109` 的 HasIndex 只查 pg_indexes.indexname）。
// 三条钩子存在的全部理由就是"第二层没铺上要在启动日志里看得见"，
// 只看 err 恰好把这一步报成"铺上了" —— 日志说反话比没有日志更坏。
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
	// 平台级启动期操作：obs_config 是租户表，TENANT_MODE=on 时 post-migrate 的裸
	// Count（query）与下面那段清重 UPDATE（预构建 SQL）会被隔离 callback fail-closed
	// （生产日志实证："post-migrate: 统计 obs_config 默认行失败(跳过唯一索引): [tenant] 表
	// obs_config 需要租户上下文但 ctx 未绑定（query）"）。这里没有 HTTP 请求 ctx 可绑，
	// 走 tenant.WithSystemScope 这条平台级豁免通道（与 migration/备份/健康检查同类）。
	db = db.WithContext(tenant.WithSystemScope(context.Background()))
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

// postMigrateDropLegacyPlaintextResetTokens 作废"改哈希之前"落库的那批明文密码重置令牌。
//
// 为什么必须清而不是留着：令牌校验现在先哈希再查，旧那批 72 字符明文行**永远匹配不上**，
// 功能上是死行；但它们是**可直接重放的改密凭证**（24h 窗口内），而且这张表在备份里
// （service/backup.go 会把 password_reset_tokens 整表导出）⇒ 留着等于把泄露面一起打包。
//
// 为什么是硬删不是软删：模型带 DeletedAt，走 GORM 的 Delete 只打标记，原文仍在表里，
// 备份与只读副本照抄得到 ⇒ 软删等于没修。
//
// 判据取列宽而非内容形状：哈希恒 64 字符十六进制，是 uuid+uuid＝72 字符。
// 版本化迁移链在当前启动口径下永不执行（建表真值是 AutoMigrate），所以作废挂在 post-migrate。
func postMigrateDropLegacyPlaintextResetTokens(db *gorm.DB) {
	if db == nil {
		return
	}
	// 平台级启动期操作：password_reset_tokens 是租户表，TENANT_MODE=on 时这里的裸
	// Raw/Exec 会被 check-gorm-only 运行时闸 fail-closed（生产日志实证：
	// "[tenant] 预构建 SQL 引用租户表 password_reset_tokens 被拒绝：数据访问必须走 GORM API"
	// + 紧随的 "post-migrate: 统计明文密码重置令牌失败(跳过作废)"）。
	// 改写为 GORM API 查询 + tenant.WithSystemScope 平台级豁免；统计与作废语义保持不变：
	// Unscoped 绕过软删过滤（原裸 SQL 不看 deleted_at），Delete 也走 Unscoped 物理硬删
	// （模型带 DeletedAt，普通 GORM Delete 只打软删标记，原文仍在表里等于没修）。
	sysDB := db.WithContext(tenant.WithSystemScope(context.Background()))
	const legacyWhere = "char_length(token) <> ?"
	var legacy int64
	if err := sysDB.Unscoped().Model(&model.PasswordResetToken{}).Where(legacyWhere, 64).Count(&legacy).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: 统计明文密码重置令牌失败(跳过作废): %v", err))
		return
	}
	if legacy == 0 {
		return
	}
	res := sysDB.Unscoped().Where(legacyWhere, 64).Delete(&model.PasswordResetToken{})
	if res.Error != nil {
		logger.Warn(fmt.Sprintf("post-migrate: 作废旧版明文密码重置令牌失败: %v", res.Error))
		return
	}
	logger.Warn(fmt.Sprintf("post-migrate: 已硬删 %d 条旧版明文密码重置令牌（哈希化后这些链接本就永不匹配，用户需重新走一次忘记密码）", res.RowsAffected))
}

// postMigrateDropLegacyExternalOrderKey 删掉"改键之前"那把 external_orders.order_id 单列唯一索引。
//
// 为什么必须有这一步（G15 第①条的收尾，也是本钩子存在的全部理由）：幂等键改成
// (platform, order_id) 只写在模型标签上，而 **AutoMigrate 只加不删** —— 已部署的库里
// 那把 `uni_external_orders_order_id` 还在、还在生效。于是"改键"在存量库上是一次
// **看起来完成了**的动作：新索引建出来了，标签也改了，而两家平台用同一个订单号时
// 第二条回调仍然撞 23505 —— 一条签名完全合法的推送继续被静默丢掉，只是这次连"为什么"都没人知道。
//
// 顺序是判据的一部分：先 DROP 旧索引，再确保复合唯一在位，最后按**形状**（indisunique）验一次。
// 反过来的话，CREATE 那一步在旧约束还在的库里照样成功（它只按名字对账），
// 于是日志会报"已就绪"而旧键仍在拦人 —— 与 N-36 那条"名字被非唯一历史索引占住"是同一族谎报。
//
// 索引名一律取自 model 的两枚常量，不在这里再写字面量：常量改名而这里没改，
// DROP 会打在一个不存在的名字上并**静默成功**（IF EXISTS 对拼错的名字也是"执行成功"）。
//
// 建不成只 Warn 不 panic：这里失败的后果是"跨平台同号仍被拦"（即改键之前的既有行为），
// 而 panic 的后果是整个服务起不来 —— 前者是存量缺陷未修，后者是把一个域的问题升级成全站。
func postMigrateDropLegacyExternalOrderKey(db *gorm.DB) {
	if db == nil {
		return
	}
	if err := db.Exec("DROP INDEX IF EXISTS " + model.ExternalOrderLegacyOrderIDIndex).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: DROP 旧 %s 失败(跨平台订单号仍会被这把旧键拦住): %v",
			model.ExternalOrderLegacyOrderIDIndex, err))
		return
	}
	const ddl = "CREATE UNIQUE INDEX IF NOT EXISTS " + model.ExternalOrderScopedUniqueIndex +
		" ON external_orders (platform, order_id)"
	if err := db.Exec(ddl).Error; err != nil {
		logger.Warn(fmt.Sprintf("post-migrate: CREATE %s 失败(存量里可能有同平台重复订单号，需人工清重后重启): %v",
			model.ExternalOrderScopedUniqueIndex, err))
		return
	}
	if why := verifyUniqueIndex(db, model.ExternalOrderScopedUniqueIndex); why != "" {
		logger.Warn("post-migrate: " + why + " ⇒ 订单幂等键的库级兜底并未生效，需人工 DROP 那枚同名非唯一索引后重启")
		return
	}
	logger.Info(fmt.Sprintf("post-migrate: external_orders 订单幂等键已是 (platform, order_id)，旧单列唯一索引 %s 已删除",
		model.ExternalOrderLegacyOrderIDIndex))
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
	// D-2（2026-10-23）：索引形状改为 (platform, msg_id, conversation_id, direction)。
	// 迁移路径：库里还是旧三列形状（历史部署的真实状态）⇒ DROP 后按新形状重建——
	// 这是 D-2 的升级路径，不是"只报不改"要守的对象；但若同名索引已是**新四列形状**
	// 而非唯一（有人为读路径特意建的同形索引），仍走 verifyUniqueIndex 的"只报不改"
	// 旧口径（形状读数见 indexShapeMatches）。AutoMigrate 按 model tag 建的索引成功时
	// 此处幂等重入无害。
	switch indexShapeMatches(DB, "message_hub", "uni_message_hub_platform_msg_conv",
		[]string{"platform", "msg_id", "conversation_id", "direction"}) {
	case "exact": // 新形状已就绪，不动（幂等）
		logger.Info("post-migrate: message_hub (tenant_id, platform, msg_id, conversation_id, direction) 唯一索引已就绪（D-2 方案③：跨方向同文本可共存）")
	case "old_shape": // 旧三列形状 ⇒ D-2 升级：换形状
		if err := DB.Exec(`DROP INDEX IF EXISTS uni_message_hub_platform_msg_conv`).Error; err != nil {
			logger.Warn(fmt.Sprintf("post-migrate: DROP 旧三列版 uni_message_hub_platform_msg_conv 失败: %v", err))
		}
		if err := DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uni_message_hub_platform_msg_conv ON message_hub (tenant_id, platform, msg_id, conversation_id, direction)`).Error; err != nil {
			logger.Warn(fmt.Sprintf("post-migrate: CREATE uni_message_hub_platform_msg_conv 失败: %v", err))
		} else if why := verifyUniqueIndex(DB, "uni_message_hub_platform_msg_conv"); why != "" {
			logger.Warn("post-migrate: " + why)
		} else {
			logger.Info("post-migrate: message_hub 唯一索引已按 D-2 升级为 (tenant_id, platform, msg_id, conversation_id, direction)")
		}
	default: // 名字被别的形状占住（既非旧三列也非新四列）⇒ 只报不改（同族守卫既有口径）
		if err := DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uni_message_hub_platform_msg_conv ON message_hub (tenant_id, platform, msg_id, conversation_id, direction)`).Error; err != nil {
			logger.Warn(fmt.Sprintf("post-migrate: CREATE uni_message_hub_platform_msg_conv 失败: %v", err))
		}
		if why := verifyUniqueIndex(DB, "uni_message_hub_platform_msg_conv"); why != "" {
			logger.Warn("post-migrate: " + why + " ⇒ 三元组去重的库级兜底并未生效，需人工 DROP 那枚同名非唯一索引后重启")
		}
	}
}

// indexShapeMatches 读库里同名索引的列清单：exact=与 want 完全一致、old_shape=与
// legacy（D-2 前的三列形状）一致、其他=名字被别的形状占用或不存在。
//
// 比较前先剥掉租户前缀：SaaS 归一化（`tenancyidx.Normalize`）会把租户表上的唯一索引
// 原位复合为 `(tenant_id, …)`，所以库里的列清单永远比 want 多一列。不剥的话
// `exact` 永不命中、`old_shape` 也永不命中 ⇒ 升级分支变成空转，而钩子只在"其他"
// 分支里 CREATE IF NOT EXISTS（名字已在场 ⇒ 空转）——"库里还是旧形状"这件事就没人管了。
func indexShapeMatches(db *gorm.DB, table, indexName string, want []string) string {
	type col struct {
		Attname string
		N       int
	}
	var cols []col
	if err := db.Raw(`
		SELECT a.attname, k.n
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = ic.relnamespace
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, n)
		JOIN pg_attribute a ON a.attrelid = tc.oid AND a.attnum = k.attnum
		WHERE ic.relname = ? AND tc.relname = ? AND n.nspname = current_schema()
		ORDER BY k.n`, indexName, table).Scan(&cols).Error; err != nil {
		return "unknown"
	}
	if len(cols) == 0 {
		return "missing"
	}
	got := make([]string, 0, len(cols))
	for _, c := range cols {
		got = append(got, c.Attname)
	}
	if len(got) > 1 && got[0] == "tenant_id" {
		got = got[1:]
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if eq(got, want) {
		return "exact"
	}
	if eq(got, []string{"platform", "msg_id", "conversation_id"}) {
		return "old_shape"
	}
	return "other"
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
	// 列类型的**遗留漂移**：库里的列类型与模型声明不一致，且 default 无法自动转换。
	//
	// 实测形态：GEO 模块的模型已把 tenant_id 改成 uint(bigint)，
	// 而开发库里遗留的列仍是 varchar（default ''::character varying），
	// AutoMigrate 改类型时撞 `42804 default for column "tenant_id" cannot be
	// cast automatically to type bigint` —— 而这个错误**不在可容忍清单**，
	// 于是 migrate.go 直接 panic，user-server 在该库上根本起不来。
	//
	// 为什么要容忍而不是放着 panic：一个环境的 schema 漂移，不该让**整个进程**
	// 起不来。panic 的伤害远大于漂移本身 —— 漂移只影响那一列，而起不来意味着
	// 连本地验证、连看日志都做不到。
	// 走 createTableFallback 兜底时，缺表会被重建（数据丢失，日志里会明写），
	// 有表则只跳过 —— 两种情况都不静默。
	if strings.Contains(msg, "cannot be cast automatically to type") ||
		strings.Contains(msg, "default for column") {
		logger.Warn("AutoMigrate 命中列类型遗留漂移(可容忍，建议按迁移修正列类型): " + msg)
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

// driftedIndexes 需要清理形状漂移的索引/列。
//
// 每条都对应一次真实的模型演进，不是猜的：
//   - tenant_modules.idx_local_tenant_module 曾建在 (tenant_slot, module_key) 上，
//     被租户策略复合化成 (tenant_id, tenant_slot, module_key)。而 tenant_slot
//     是多余的列（多租户维度由 tenant_id 承担）。
//   - tenant_slot 列同步删除：不再有任何模型声明它，留着只会让下一个读模型的人困惑。
var driftedIndexes = []struct {
	table   string
	index   string
	columns []string // 允许的形状（业务列 或 tenant_id+业务列）
	dropCol string   // 需要一并删除的废弃列（空表示无）
}{
	{
		table:   "tenant_modules",
		index:   "idx_local_tenant_module",
		columns: []string{"tenant_id, module_key", "module_key"},
		dropCol: "tenant_slot",
	},
}

// dropDriftedModuleIndexes 删除列集与当前模型不符的索引，以及废弃列。
// 幂等：形状正确时什么都不做。
func dropDriftedModuleIndexes(db *gorm.DB) {
	for _, d := range driftedIndexes {
		if !db.Migrator().HasTable(d.table) {
			continue
		}
		var defs []string
		if err := db.Raw(`SELECT pg_get_indexdef(indexrelid) FROM pg_index
			WHERE indrelid = to_regclass(?::text) AND indexrelid::regclass::text = ?`,
			d.table, d.index).Scan(&defs).Error; err != nil {
			logger.Warnf("[migrate] 读取索引 %s.%s 失败，跳过清理: %v", d.table, d.index, err)
			continue
		}
		if len(defs) == 0 {
			continue // 不存在：交给 AutoMigrate 建
		}
		actual := indexColumns(defs[0])
		if allowedShape(actual, d.columns) {
			// 形状已正确；但仍可能留着废弃列（索引不引用它，策略也不会碰）
			if d.dropCol != "" {
				dropColumnIfExists(db, d.table, d.dropCol)
			}
			continue
		}
		logger.Warnf("[migrate] 索引 %s.%s 列集漂移（实际 %v，期望 %v），删除后由 AutoMigrate 重建",
			d.table, d.index, actual, d.columns)
		if err := db.Exec("DROP INDEX IF EXISTS " + d.index).Error; err != nil {
			logger.Warnf("[migrate] 删除漂移索引 %s 失败: %v", d.index, err)
			continue
		}
		if d.dropCol != "" {
			dropColumnIfExists(db, d.table, d.dropCol)
		}
	}
}

// indexColumns 从 pg_get_indexdef 提取列清单（取列区括号内、逗号分隔、去空白）
func indexColumns(def string) string {
	open := strings.Index(def, "(")
	close := strings.LastIndex(def, ")")
	if open < 0 || close <= open {
		return ""
	}
	parts := strings.Split(def[open+1:close], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, ", ")
}

func allowedShape(actual string, allowed []string) bool {
	for _, a := range allowed {
		if actual == a {
			return true
		}
	}
	return false
}

func dropColumnIfExists(db *gorm.DB, table, col string) {
	var n int
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
		table, col).Scan(&n).Error; err != nil || n == 0 {
		return
	}
	if err := db.Exec(fmt.Sprintf(`ALTER TABLE %s DROP COLUMN IF EXISTS %s`, table, col)).Error; err != nil {
		logger.Warnf("[migrate] 删除废弃列 %s.%s 失败: %v", table, col, err)
		return
	}
	logger.Warnf("[migrate] 已删除废弃列 %s.%s（不再被任何模型声明）", table, col)
}
