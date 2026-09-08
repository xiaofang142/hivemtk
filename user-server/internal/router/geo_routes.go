package router

import (
	"context"

	geoctrl "hivemtk-user/internal/geo/controller"
	georepo "hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	mainrepo "hivemtk-user/internal/repository"

	"hivemtk-user/internal/aiagent/llm"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupGeoRoutes GEO 生成式引擎优化功能路由。
//
// 权限分级：config 写入/优化（PUT /geo/config、POST /geo/config/optimize）
// 仅管理员可操作，防止 staff 误改品牌配置导致全链路 GEO 内容偏移。
func SetupGeoRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {

	keywordRepo := georepo.NewGeoKeywordRepositoryWithDB(gormDB)
	keywordGroupRepo := georepo.NewGeoKeywordGroupRepositoryWithDB(gormDB)
	articleRepo := georepo.NewGeoArticleRepositoryWithDB(gormDB)
	optimizationRepo := georepo.NewGeoOptimizationRepositoryWithDB(gormDB)
	verifyRepo := georepo.NewGeoVerifyResultRepositoryWithDB(gormDB)
	apiCallRepo := georepo.NewGeoAPICallRepositoryWithDB(gormDB)
	configRepo := georepo.NewGeoConfigRepositoryWithDB(gormDB)
	accountRepo := georepo.NewGeoPlatformAccountRepositoryWithDB(gormDB)
	publishRecordRepo := georepo.NewGeoPublishRecordRepositoryWithDB(gormDB)
	kbDocRepo := georepo.NewGeoKnowledgeDocumentRepositoryWithDB(gormDB)
	wfRepo := georepo.NewGeoWorkflowRepositoryWithDB(gormDB)
	execRepo := georepo.NewGeoWorkflowExecutionRepositoryWithDB(gormDB)
	tplRepo := georepo.NewGeoWorkflowTemplateRepositoryWithDB(gormDB)
	chainRepo := georepo.NewGeoQueryChainRepository(gormDB)
	taskRepo := georepo.NewGeoContentTaskRepository(gormDB)

	llmAdapter := geoservice.NewLLMAdapter()

	keywordSvc := geoservice.NewKeywordService(keywordGroupRepo, keywordRepo, apiCallRepo, llmAdapter)
	contentSvc := geoservice.NewContentService(articleRepo, optimizationRepo, apiCallRepo, kbDocRepo, llmAdapter)
	verifySvc := geoservice.NewVerificationService(verifyRepo, apiCallRepo, chainRepo, taskRepo, llmAdapter, geoservice.NewDefaultSearchProbe())
	reportSvc := geoservice.NewReportService(articleRepo, keywordRepo, optimizationRepo, verifyRepo, apiCallRepo)
	configSvc := geoservice.NewConfigService(configRepo, llmAdapter)
	platformSvc := geoservice.NewPlatformService(accountRepo, publishRecordRepo, articleRepo)
	kbSvc := geoservice.NewKBService(kbDocRepo, llmAdapter)
	wfSvc := geoservice.NewWorkflowService(wfRepo, execRepo, tplRepo, chainRepo, taskRepo, llmAdapter)

	mainClueRepo := mainrepo.NewClueRepositoryWithDB(gormDB)

	decisionCtrl := geoctrl.NewGeoDecisionController(
		georepo.NewGeoQueryChainRepository(gormDB),
		georepo.NewGeoContentTaskRepository(gormDB),
		newGeoLeadReporter(gormDB),
	)
	auth.GET("/geo/decision/report", decisionCtrl.GetDecisionReport)
	auth.GET("/geo/decision/tasks", decisionCtrl.GetTasks)
	auth.POST("/geo/decision/tasks/:id/done", decisionCtrl.MarkTaskDone)

	analyticsSvc := geoservice.NewGeoDecisionAnalyticsService(
		georepo.NewGeoVerifyResultRepositoryWithDB(gormDB),
		georepo.NewGeoProbeRunRepositoryWithDB(gormDB),
		georepo.NewGeoConfigRepositoryWithDB(gormDB),
		georepo.NewGeoContentTaskRepository(gormDB),
		georepo.NewGeoQueryChainRepository(gormDB),
		georepo.NewGeoCrawlerVisitRepository(gormDB),
		llmAdapter,
		georepo.NewGeoAPICallRepositoryWithDB(gormDB))

	geoChainRepo := georepo.NewGeoQueryChainRepository(gormDB)
	wfSvc.RegisterCaptureLeadExecutor(geoservice.CaptureLeadFunc(func(ctx context.Context, contact, contactType, chainID, intent string) (string, error) {
		clue := &model.Clue{
			Account:  contact,
			Name:     contact,
			SourceID: chainID,
			Type:     int64(9),
		}
		if err := mainClueRepo.Create(ctx, clue); err != nil {
			return "", err
		}

		if clue.OneID != "" && geoChainRepo != nil {
			utils.WarnErrKV("router.geo.bindOneIDToChain", gormDB.WithContext(ctx).
				Table("geo_query_chains").
				Where("chain_id = ? AND (one_id = '' OR one_id IS NULL)", chainID).
				Update("one_id", clue.OneID).Error, "chain_id", chainID, "one_id", clue.OneID, "clue_id", clue.ID)
		}
		return clue.ID, nil
	}))
	keSvc := geoservice.NewKeywordEnhanceService(keywordRepo, verifyRepo, llmAdapter)

	keywordCtrl := geoctrl.NewKeywordController(keywordSvc)
	contentCtrl := geoctrl.NewContentController(contentSvc)
	verifyCtrl := geoctrl.NewVerificationController(verifySvc)
	reportCtrl := geoctrl.NewReportController(reportSvc, analyticsSvc)
	configCtrl := geoctrl.NewConfigController(configSvc)
	platformCtrl := geoctrl.NewPlatformController(platformSvc)
	kbCtrl := geoctrl.NewKBController(kbSvc)
	wfCtrl := geoctrl.NewWorkflowController(wfSvc)
	tmCtrl := geoctrl.NewTechMetricsController()
	keCtrl := geoctrl.NewKeywordEnhanceController(keSvc)

	probeRepo := georepo.NewGeoProbeRunRepositoryWithDB(gormDB)
	probes := geoservice.NewEngineProbesFromDB(gormDB)
	probeSvc := geoservice.NewProbeService(probes, probeRepo)
	probeCtrl := geoctrl.NewProbeController(probeSvc)

	visibilitySvc := geoservice.NewVisibilityService(georepo.NewGeoDailyStatRepositoryWithDB(gormDB))
	fanoutSvc := geoservice.NewPromptFanoutService(llmAdapter, probeSvc)
	visibilityCtrl := geoctrl.NewVisibilityController(visibilitySvc, fanoutSvc)

	jobCtrl := geoctrl.NewJobController(geoservice.GetGeoJobManager())

	sourceRepo := georepo.NewGeoSourceCatalogRepositoryWithDB(gormDB)
	crawlerSvc := geoservice.NewCrawlerService(sourceRepo)
	sourceCtrl := geoctrl.NewSourceCatalogController(crawlerSvc)

	entityRepo := georepo.NewGeoEntityRepositoryWithDB(gormDB)
	extractSvc := geoservice.NewEntityExtractorService(
		entityRepo,
		kbDocRepo,
		llm.GetGlobalDispatcher(),
	)
	entityCtrl := geoctrl.NewEntityController(entityRepo, extractSvc)

	competitorRepo := georepo.NewGeoCompetitorRepository()
	competitorSvc := geoservice.NewGeoCompetitorService(competitorRepo)
	competitorCtrl := geoctrl.NewCompetitorController(competitorSvc)

	alertSvc := geoservice.NewAlertService(georepo.NewGeoAlertRepositoryWithDB(gormDB))
	alertCtrl := geoctrl.NewAlertController(alertSvc)

	geo := auth.Group("/geo")

	geo.POST("/keywords/mine", keywordCtrl.MineKeywords)
	geo.POST("/keywords/expand", keywordCtrl.SemanticExpand)
	geo.POST("/keywords/cluster", keywordCtrl.TopicCluster)
	geo.GET("/keywords/list", keywordCtrl.GetKeywordList)
	geo.DELETE("/keywords/:id", keywordCtrl.DeleteKeyword)

	geo.POST("/content/generate", contentCtrl.GenerateContent)
	geo.POST("/content/optimize", contentCtrl.OptimizeContent)
	geo.POST("/content/score", contentCtrl.ScoreContent)
	geo.POST("/content/eeat", contentCtrl.EnhanceEEAT)
	geo.POST("/content/schema", contentCtrl.GenerateSchema)
	geo.POST("/content/uniqueness", contentCtrl.CheckUniqueness)
	geo.GET("/content/list", contentCtrl.GetArticleList)
	geo.GET("/content/:id", contentCtrl.GetArticleByID)

	geo.POST("/verification/verify", verifyCtrl.VerifyArticle)
	geo.POST("/verification/negative", verifyCtrl.MonitorNegative)
	geo.GET("/verification/results/:article_id", verifyCtrl.GetVerifyResults)

	geo.GET("/reports/summary", reportCtrl.GetReport)
	geo.GET("/reports/roi", reportCtrl.GetROI)
	geo.GET("/reports/api-costs", reportCtrl.GetAPICosts)

	geo.GET("/sov", reportCtrl.ShareOfVoice)
	geo.GET("/sov/trend", reportCtrl.ShareOfVoiceTrend)
	geo.GET("/crawler-stats", reportCtrl.CrawlerStats)
	geo.POST("/crawler/run", reportCtrl.RunCrawler)
	geo.POST("/inaccurate-claims", reportCtrl.InaccurateClaims)

	geo.GET("/competitors", competitorCtrl.List)
	geo.GET("/competitors/:id", competitorCtrl.Get)
	geo.POST("/competitors", competitorCtrl.Create)
	geo.PUT("/competitors/:id", competitorCtrl.Update)
	geo.DELETE("/competitors/:id", competitorCtrl.Delete)

	geo.GET("/alerts", alertCtrl.List)
	geo.GET("/alerts/unread-count", alertCtrl.UnreadCount)
	geo.POST("/alerts/:id/ack", alertCtrl.MarkNotified)
	geo.DELETE("/alerts/:id", alertCtrl.Delete)

	geo.GET("/visibility/trend", visibilityCtrl.Trend)
	geo.GET("/visibility/engine-compare", visibilityCtrl.EngineCompare)
	geo.POST("/prompt/fanout", visibilityCtrl.Fanout)

	geo.GET("/config", configCtrl.GetConfig)

	geo.GET("/platform/platforms", platformCtrl.ListPlatforms)
	geo.GET("/platform/accounts", platformCtrl.ListAccounts)
	geo.GET("/platform/records", platformCtrl.ListPublishRecords)

	geo.GET("/kb/documents", kbCtrl.List)
	geo.GET("/kb/documents/:id", kbCtrl.Get)
	geo.GET("/kb/search", kbCtrl.Search)
	geo.POST("/kb/ask", kbCtrl.Ask)

	geo.GET("/workflow/workflows", wfCtrl.List)
	geo.GET("/workflow/workflows/:id", wfCtrl.Get)
	geo.GET("/workflow/workflows/:id/executions", wfCtrl.ListExecutions)
	geo.GET("/workflow/templates", wfCtrl.ListTemplates)

	geo.POST("/techconfig/robots", tmCtrl.GenerateRobots)
	geo.POST("/techconfig/sitemap", tmCtrl.GenerateSitemap)
	geo.POST("/techconfig/llms-txt", tmCtrl.GenerateLLMsTxt)

	geo.POST("/metrics/analyze", tmCtrl.AnalyzeMetrics)

	geo.GET("/keyword-enhance/analyze", keCtrl.Analyze)
	geo.POST("/keyword-enhance/enhance", keCtrl.Enhance)

	geo.GET("/probe/engines", probeCtrl.ListAvailableEngines)
	geo.POST("/probe/test", probeCtrl.TestSingle)
	geo.POST("/probe/all", probeCtrl.ProbeAll)
	geo.POST("/probe/run-negative", probeCtrl.RunNegativeMonitor)
	geo.POST("/probe/run-source-sync", probeCtrl.RunSourceSync)
	geo.GET("/probe/runs", probeCtrl.ListRuns)

	geo.GET("/source-catalog/levels", sourceCtrl.LookupLevels)

	geo.GET("/entity/list", entityCtrl.ListEntities)
	geo.GET("/entity/:id/graph", entityCtrl.GetGraph)
	geo.POST("/entities/extract", entityCtrl.Extract)

	geoAdmin := geo.Group("")
	geoAdmin.Use(middleware.AdminAuthMiddleware())
	geoAdmin.PUT("/config", configCtrl.UpdateConfig)
	geoAdmin.POST("/config/optimize", configCtrl.OptimizeConfig)

	geoAdmin.POST("/platform/accounts", platformCtrl.SaveAccount)
	geoAdmin.DELETE("/platform/accounts/:id", platformCtrl.DeleteAccount)
	geoAdmin.POST("/platform/publish", platformCtrl.Publish)

	geoAdmin.POST("/workflow/workflows", wfCtrl.Create)
	geoAdmin.PUT("/workflow/workflows/:id", wfCtrl.Update)
	geoAdmin.DELETE("/workflow/workflows/:id", wfCtrl.Delete)
	geoAdmin.POST("/workflow/workflows/:id/run", wfCtrl.Run)
	geoAdmin.POST("/workflow/templates", wfCtrl.CreateTemplate)

	geoAdmin.POST("/kb/documents", kbCtrl.Save)
	geoAdmin.DELETE("/kb/documents/:id", kbCtrl.Delete)

	geoAdmin.GET("/jobs", jobCtrl.List)
	geoAdmin.GET("/jobs/runs", jobCtrl.Runs)
	geoAdmin.POST("/jobs/:name/trigger", jobCtrl.Trigger)
	geoAdmin.PUT("/jobs/:name/schedule", jobCtrl.UpdateSchedule)
}

type geoLeadReporter struct{ db *gorm.DB }

func newGeoLeadReporter(db *gorm.DB) *geoLeadReporter { return &geoLeadReporter{db: db} }

func (r *geoLeadReporter) CountCapturedLeads(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Clue{}).
		Where("type = ?", int64(9)).
		Where("(source_id LIKE 'probe:%' OR source_id LIKE 'verify:%' OR source_id LIKE 'inbox:%')").
		Count(&n).Error
	return n, err
}
