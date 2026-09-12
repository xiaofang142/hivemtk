package db

import (
	"fmt"
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
		&model.IntentLog{},
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

	return DB
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
	stmt.Parse(m)
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
