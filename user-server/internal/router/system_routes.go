package router

import (
	knowledgectrl "hivemtk-user/internal/aiagent/knowledge/controller"
	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	rag_service "hivemtk-user/internal/aiagent/rag/service"
	"hivemtk-user/internal/aiagent/vector"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/migration/migrations"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupSystemRoutes 注册系统域路由。
//
// 两个参数按**数据域**分档（不是按"路由归属"分档）：
//   - tenantAdmin：操作**租户表**的端点。租户 admin 本就该读写自己租户的这些
//     数据（隔离 callback 已把范围限死在本租户），守卫是普通 admin。
//   - systemAdmin：操作**全局表与实例运行态**的端点（备份恢复 / 重启 / 实例统计），
//     平台运营语义，守卫是 PlatformOperatorMiddleware。
//
// 判据是"端点写读落到哪张表"：
//
//	system_config / system_config_kv / obs_config 都是**租户表**（有 tenant_id 列，
//	由隔离 callback 自动限域）⇒ 租户档；
//	backups / restore_records 是**全局表**，进程级操作没有租户概念 ⇒ 平台运营方档。
func setupSystemRoutes(tenantAdmin, systemAdmin *gin.RouterGroup, gormDB *gorm.DB) {
	systemConfigCtrl := controller.NewSystemConfigController()
	tenantAdmin.GET("/system/config", systemConfigCtrl.GetConfig)
	tenantAdmin.POST("/system/config", systemConfigCtrl.SaveConfig)

	toolIntegrationCtrl := controller.NewToolIntegrationConfigController()
	tenantAdmin.GET("/agent/tool-integrations", toolIntegrationCtrl.GetConfig)
	tenantAdmin.PUT("/agent/tool-integrations", toolIntegrationCtrl.SaveConfig)

	agentSettingsCtrl := controller.NewAgentSettingsController()
	tenantAdmin.GET("/agent/settings", agentSettingsCtrl.GetConfig)
	tenantAdmin.PUT("/agent/settings", agentSettingsCtrl.SaveConfig)

	// 实例运行态与备份恢复：进程与实例级，不属于任何租户 ⇒ 平台运营方档。
	systemOpsCtrl := controller.NewSystemOpsControllerWithServices(newBackupService(gormDB), newRestoreService(gormDB))
	systemAdmin.POST("/system/restart", systemOpsCtrl.RestartServer)
	systemAdmin.GET("/system/logs", systemOpsCtrl.GetSystemLogs)
	systemAdmin.GET("/system/stats", systemOpsCtrl.GetSystemStats)
	systemAdmin.GET("/stats/system", systemOpsCtrl.GetSystemStats)
	systemAdmin.GET("/system/backup", systemOpsCtrl.GetBackupList)
	systemAdmin.POST("/system/backup", systemOpsCtrl.CreateBackup)
	systemAdmin.POST("/system/restore", systemOpsCtrl.RestoreBackup)

	// 管理员登录策略：读的是**进程静态配置**（config.GetAdminConfig，不落库、
	// 与租户无关），不泄露任何租户数据 ⇒ 租户档即可，不必要求平台运营方。
	adminConfigCtrl := controller.NewAdminConfigController()
	tenantAdmin.GET("/admin/config", adminConfigCtrl.GetAdminConfig)

	// OBS 配置：obs_config 是**租户表**（有 tenant_id 列）⇒ 租户档。
	obsConfigCtrl := controller.NewObsConfigController()
	tenantAdmin.GET("/obs/config", obsConfigCtrl.GetConfigList)
	tenantAdmin.GET("/obs/config/:id", obsConfigCtrl.GetConfig)
	tenantAdmin.GET("/obs/config/default", obsConfigCtrl.GetDefaultConfig)

	obsAdmin := tenantAdmin.Group("", middleware.AdminAuthMiddleware())
	{
		obsAdmin.POST("/obs/config", obsConfigCtrl.CreateConfig)
		obsAdmin.PUT("/obs/config/:id", obsConfigCtrl.UpdateConfig)
		obsAdmin.DELETE("/obs/config/:id", obsConfigCtrl.DeleteConfig)
		obsAdmin.POST("/obs/config/:id/test", obsConfigCtrl.TestConnection)
		obsAdmin.POST("/obs/config/:id/default", obsConfigCtrl.SetDefault)
	}
}

func setupRagRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	ragRepo := knowledgerepo.NewRagConfigRepository(gormDB)
	llmService := llm.NewLLMService()
	rageEngine := rag_core.NewRAGEngine(nil)
	ragCoreService := rag_service.NewRAGService(llmService, rageEngine)
	docProcessor := etl.NewDocumentProcessor(nil)
	remoteVectorizer := vector.NewRemoteVectorizer(1024)
	vectorStore := vector.NewInMemoryVectorStore(1024)
	vecProcessor := vector.NewVectorProcessor(remoteVectorizer, vectorStore)
	ragService := knowledgesvc.NewRagConfigService(ragRepo, ragCoreService, docProcessor, vecProcessor)
	ragConfigCtrl := knowledgectrl.NewRagConfigController(ragService)
	ragConfigCtrl.RegisterRoutes(auth)

	ragMetricsSvc := service.NewRagMetricsService(repository.NewRagMetricsRepository(gormDB))
	ragHealthSvc := service.NewRagHealthService(repository.NewRagHealthRepository(gormDB), ragMetricsSvc)
	ragHealthCtrl := controller.NewRagHealthController(ragHealthSvc)
	ragHealthCtrl.RegisterRoutes(auth)

	ragRecallCtrl := controller.NewRagRecallMonitorController(service.NewRagRecallMonitorService(repository.NewRagRecallMonitorRepository(gormDB), 0, 0), ragMetricsSvc)
	ragRecallCtrl.RegisterRoutes(auth)
}

func setupKnowledgeBaseRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	knowledgeBaseCtrl := knowledgectrl.NewKnowledgeBaseController()
	// 上传口可选 QA 增强（P1 观察项②，对标 FastGPT）：与爬虫侧共用同一套拆分服务，
	// 默认关——只有 /rag/import 显式带 qa_split=true 才在文档入库后投递拆分。
	knowledgeBaseCtrl.SetQASplitService(knowledgesvc.NewQASplitService(knowledgesvc.NewGatewayLLMClient()))
	knowledgeBaseCtrl.RegisterRoutes(auth)

	// 知识库成员绑定（P0-2/P0-3/D-8 方案 A）：/knowledge-bases/:id/items 三端点。
	// 挂在同一个路由组，不新增 /api/knowledge 顶层域名（规划 §2.2 契约第 1 条）。
	kbItemCtrl := controller.NewKBItemBindingController(
		service.NewKBItemBindingService(
			service.NewKnowledgeBaseServiceWithRepos(
				repository.NewKnowledgeBaseRepository(gormDB),
				repository.NewAgentKBBindingRepository(gormDB),
				repository.NewKBItemBindingRepository(gormDB)),
			repository.NewKBItemBindingRepository(gormDB)))
	kbItemCtrl.RegisterRoutes(auth.Group("/knowledge-bases"))

	knowledgeWorkspaceCtrl := knowledgectrl.NewKnowledgeWorkspaceController(&openAPISourceAdapter{svc: service.NewOpenAPIService()})
	knowledgeWorkspaceCtrl.RegisterRoutes(auth)

	knowledgeWorkspaceCtrl.RegisterConnectors(auth, controller.NewKBConnectorController())

	knowledgeMerchantCtrl := knowledgectrl.NewKnowledgeMerchantController()
	knowledgeMerchantCtrl.RegisterRoutes(auth)
}

// setupBackupRoutes 注册备份/恢复端点。
//
// 入参是平台运营方组而非 auth：backups / restore_records 是全局表，
// 且备份动作读写的实例数据跨租户（执行体走 SystemScope），
// 租户 admin 既不该看到别的租户的备份清单，也不该触发全实例恢复。
// newBackupService / newRestoreService 统一在此构造（备份域两个装配点共用同一形状）。
func newBackupService(gormDB *gorm.DB) *service.BackupService {
	return service.NewBackupServiceWithRepos(
		repository.NewBackupRepositoryWithDB(gormDB),
		repository.NewBackupDataRepositoryWithDB(gormDB),
	)
}

func newRestoreService(gormDB *gorm.DB) *service.RestoreService {
	return service.NewRestoreServiceWithRepos(
		repository.NewRestoreRecordRepositoryWithDB(gormDB),
		repository.NewBackupRepositoryWithDB(gormDB),
		repository.NewBackupDataRepositoryWithDB(gormDB),
	)
}

func setupBackupRoutes(systemAdmin *gin.RouterGroup, gormDB *gorm.DB) {
	backupCtrl := controller.NewBackupController(newBackupService(gormDB))
	restoreCtrl := controller.NewRestoreController(newRestoreService(gormDB))
	systemAdmin.GET("/backups", backupCtrl.GetBackupList)
	systemAdmin.GET("/backups/:id", backupCtrl.GetBackupByID)
	systemAdmin.GET("/restore/list", restoreCtrl.GetRestoreList)
	systemAdmin.GET("/restore/last", restoreCtrl.GetLastRestore)
	systemAdmin.POST("/backups", backupCtrl.CreateBackup)
	systemAdmin.DELETE("/backups/:id", backupCtrl.DeleteBackup)
	systemAdmin.POST("/restore", restoreCtrl.RestoreBackup)
}

func setupMigrationRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	registry := migration.NewMigrationRegistry()
	migrationSvc := migration.NewMigrationService(registry, gormDB, migrations.RegisterMigrations)
	migrationCtrl := controller.NewMigrationController(migrationSvc)
	auth.GET("/migration/task/:id", migrationCtrl.GetUpgradeTask)
	auth.GET("/migration/history", migrationCtrl.GetUpgradeHistory)
	auth.GET("/migration/records", migrationCtrl.GetMigrationRecords)
	auth.GET("/migration/current-version", migrationCtrl.GetCurrentVersion)
	auth.GET("/migration/available", migrationCtrl.GetAvailableUpgrades)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/migration/task", migrationCtrl.CreateUpgradeTask)
		admin.POST("/migration/rollback", migrationCtrl.Rollback)
	}
}

func setupI18nRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	glossaryRepo := repository.NewGlossaryRepositoryWithDB(db)
	glossarySvc := translation.NewGlossaryService(glossaryRepo, nil)
	glossaryCtrl := controller.NewGlossaryController(glossarySvc)
	glossaryCtrl.RegisterRoutes(auth)

	statsRepo := repository.NewI18nStatsRepositoryWithDB(db)
	statsSvc := translation.NewI18nStatsService(statsRepo)
	statsCtrl := controller.NewI18nStatsController(statsSvc)
	statsCtrl.RegisterRoutes(auth)
}

func setupTuningRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	tuning := auth.Group("/admin/tuning")
	ctrl := controller.NewTuningController(service.NewTuningService(repository.NewConfidenceSignalRepositoryWithDB(gormDB), repository.NewConfidenceCalibrationRepositoryWithDB(gormDB), repository.NewThresholdPolicyRepositoryWithDB(gormDB)))

	tuning.GET("/confidence/signals", ctrl.ListConfidenceSignals)
	tuning.GET("/confidence/signals/:id", ctrl.GetConfidenceSignal)
	tuning.GET("/confidence/signals/stats", ctrl.StatsConfidenceSignals)

	tuning.GET("/confidence/calibrations", ctrl.ListCalibrations)

	tuning.GET("/confidence/policies", ctrl.ListThresholdPolicies)
	tuning.PUT("/confidence/policies", ctrl.UpsertThresholdPolicy)

	tuning.GET("/humanize/scores", ctrl.ListHumanizeScores)
	tuning.GET("/humanize/scores/stats", ctrl.StatsHumanizeScores)

	tuning.GET("/humanize/baselines", ctrl.ListChampionBaselines)

	tuning.GET("/feedback/events", ctrl.ListFeedbackEvents)
	tuning.GET("/feedback/events/stats", ctrl.StatsFeedbackEvents)

	tuning.GET("/feedback/dialogues", ctrl.ListChampionDialogues)

	tuning.GET("/prompt/candidates", ctrl.ListPromptCandidates)
	tuning.PUT("/prompt/candidates/:id/status", ctrl.UpdatePromptCandidateStatus)

	tuning.GET("/bandit/arms", ctrl.ListBanditArms)

	tuning.GET("/humanize/low-quality", ctrl.ListLowQualitySamples)
}

func setupAIToolConfigRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	toolRepo := repository.NewAIToolConfigRepository(db)
	bindingRepo := repository.NewAIToolAccountBindingRepository(db)
	svc := service.NewAIToolConfigService(toolRepo, bindingRepo)
	ctrl := controller.NewAIToolConfigController(svc)

	g := auth.Group("/ai-tools")
	{
		g.GET("", ctrl.ListTools)
		g.GET("/:name", ctrl.GetTool)
		g.GET("/:name/accounts", ctrl.GetToolAccounts)
	}

	admin := auth.Group("/ai-tools", middleware.AdminAuthMiddleware())
	{
		admin.PUT("/:name/status", ctrl.UpdateToolStatus)
		admin.POST("/batch-status", ctrl.BatchUpdateStatus)
		admin.POST("/:name/accounts", ctrl.BindAccount)
		admin.DELETE("/:name/accounts/:account_type/:account_id", ctrl.UnbindAccount)
	}
}
