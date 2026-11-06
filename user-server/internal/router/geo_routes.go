package router

import (
	"context"
	"strings"

	"hivemtk-user/internal/config"
	maindto "hivemtk-user/internal/dto"
	geoctrl "hivemtk-user/internal/geo/controller"
	georepo "hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	mainrepo "hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	cardsvc "hivemtk-user/internal/service/cards"

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
	contentSvc := geoservice.NewContentService(articleRepo, optimizationRepo, apiCallRepo, kbDocRepo, configRepo, llmAdapter)
	verifySvc := geoservice.NewVerificationService(verifyRepo, apiCallRepo, chainRepo, taskRepo, llmAdapter, geoservice.NewDefaultSearchProbe())
	reportSvc := geoservice.NewReportService(articleRepo, keywordRepo, optimizationRepo, verifyRepo, apiCallRepo)
	configSvc := geoservice.NewConfigService(configRepo, llmAdapter)
	platformSvc := geoservice.NewPlatformService(accountRepo, publishRecordRepo, articleRepo)
	kbSvc := geoservice.NewKBService(kbDocRepo, llmAdapter)
	wfSvc := geoservice.NewWorkflowService(wfRepo, execRepo, tplRepo, chainRepo, taskRepo, llmAdapter)

	mainClueRepo := mainrepo.NewClueRepositoryWithDB(gormDB)
	identitySvc := service.NewCustomerIdentityService()

	decisionCtrl := geoctrl.NewGeoDecisionController(
		geoservice.NewDecisionService(taskRepo, newGeoLeadReporter(mainClueRepo)),
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

	wfSvc.RegisterCaptureLeadExecutor(geoCaptureLeadPort(mainClueRepo, chainRepo, identitySvc))
	keSvc := geoservice.NewKeywordEnhanceService(keywordRepo, verifyRepo, llmAdapter)

	keywordCtrl := geoctrl.NewKeywordController(keywordSvc)
	contentCtrl := geoctrl.NewContentController(contentSvc)
	verifyCtrl := geoctrl.NewVerificationController(verifySvc)
	reportCtrl := geoctrl.NewReportController(reportSvc, analyticsSvc)
	configCtrl := geoctrl.NewConfigController(configSvc)
	platformCtrl := geoctrl.NewPlatformController(platformSvc)
	kbCtrl := geoctrl.NewKBController(kbSvc)
	wfCtrl := geoctrl.NewWorkflowController(wfSvc)
	tmCtrl := geoctrl.NewTechMetricsController(kbSvc)
	keCtrl := geoctrl.NewKeywordEnhanceController(keSvc)
	autopilotCtrl := geoctrl.NewAutopilotController(
		geoservice.NewAutopilotService(georepo.NewGeoAutopilotRunRepositoryWithDB(gormDB)))

	// ⬇️ GEO v2 新增 Service + Controller
	// 探针必须按请求租户解析：llm_providers/geo_config 都是租户表，装配期
	// 固化一份清单会把 A 的品牌提示词与 provider 凭据用在 B 的探针上。
	probeResolver := func(ctx context.Context) []geoservice.SearchProbe {
		if _, ok := tenant.FromContext(ctx); !ok && tenant.IsMultiTenant() {
			return nil
		}
		return geoservice.NewEngineProbesFromDB(ctx, gormDB)
	}
	kwMiningSvc := geoservice.NewKeywordMiningService(keywordRepo, llmAdapter)
	pushSvc := geoservice.NewPushService(
		georepo.NewGeoPusherConfigRepositoryWithDB(gormDB),
		georepo.NewGeoPushRecordRepositoryWithDB(gormDB),
		articleRepo,
	)
	siteRepo := georepo.NewGeoSiteRepositoryWithDB(gormDB)
	siteSvc := geoservice.NewSiteDeployService(siteRepo, articleRepo, pushSvc)
	indexTrackerSvc := geoservice.NewIndexTrackerService(
		georepo.NewGeoIndexTrackingRepositoryWithDB(gormDB),
		articleRepo,
		keywordRepo,
		georepo.NewGeoDailyStatRepositoryWithDB(gormDB),
	)
	indexTrackerSvc.SetProbeResolver(probeResolver)
	kwMiningCtrl := geoctrl.NewKeywordMiningController(kwMiningSvc)
	seedTermSvc := geoservice.NewSeedTermService(georepo.NewGeoSeedTermRepositoryWithDB(gormDB))
	seedTermCtrl := geoctrl.NewSeedTermController(seedTermSvc)
	dictSvc := geoservice.NewDictService(georepo.NewGeoDictRepositoryWithDB(gormDB))
	dictCtrl := geoctrl.NewDictController(dictSvc)
	pushCtrl := geoctrl.NewPushController(pushSvc)
	siteCtrl := geoctrl.NewSiteController(siteSvc, pushSvc, configRepo)
	indexCtrl := geoctrl.NewIndexTrackerController(indexTrackerSvc)

	// ⬇️ GEO v2 CRUD Controller（3 张核心配置表）
	siteCRUDCtrl := geoctrl.NewGeoSiteController(geoservice.NewSiteConfigService(siteRepo))
	pusherConfigSvc := geoservice.NewPusherConfigService(georepo.NewGeoPusherConfigRepositoryWithDB(gormDB))
	pusherCRUDCtrl := geoctrl.NewGeoPusherConfigController(pusherConfigSvc)
	schemaTemplateSvc := geoservice.NewSchemaTemplateService(georepo.NewGeoSchemaTemplateRepositoryWithDB(gormDB))
	schemaCRUDCtrl := geoctrl.NewGeoSchemaTemplateController(schemaTemplateSvc)

	probeRepo := georepo.NewGeoProbeRunRepositoryWithDB(gormDB)
	probeSvc := geoservice.NewProbeService(nil, probeRepo)
	probeSvc.SetProbeResolver(probeResolver)
	probeCtrl := geoctrl.NewProbeController(probeSvc)
	citeSvc := geoservice.NewCitationService(probeRepo, configRepo, georepo.NewGeoCompetitorRepositoryWithDB(gormDB))
	// A10（2026-10-24）：引用漂移读端注入（快照由 cron citation_drift 写）。
	citeCtrl := geoctrl.NewCitationControllerWithDrift(citeSvc,
		geoservice.NewCitationDriftService(probeRepo, georepo.NewGeoCitationSnapshotRepositoryWithDB(gormDB)))
	oppSvc := geoservice.NewOpportunityService(
		analyticsSvc, citeSvc,
		georepo.NewGeoAlertRepositoryWithDB(gormDB),
		articleRepo, configRepo)
	oppCtrl := geoctrl.NewOpportunityController(oppSvc)

	visibilitySvc := geoservice.NewVisibilityService(georepo.NewGeoDailyStatRepositoryWithDB(gormDB))
	fanoutSvc := geoservice.NewPromptFanoutService(llmAdapter, probeSvc)
	visibilityCtrl := geoctrl.NewVisibilityController(visibilitySvc, fanoutSvc)

	jobCtrl := geoctrl.NewJobController(geoservice.GetGeoJobManager())

	sourceRepo := georepo.NewGeoSourceCatalogRepositoryWithDB(gormDB)
	crawlerSvc := geoservice.NewCrawlerService(sourceRepo)
	sourceCtrl := geoctrl.NewSourceCatalogController(crawlerSvc)

	entityRepo := georepo.NewGeoEntityRepositoryWithDB(gormDB)
	entitySvc := geoservice.NewEntityService(entityRepo)
	extractSvc := geoservice.NewEntityExtractorService(
		entityRepo,
		kbDocRepo,
		llm.GetGlobalDispatcher(),
	)
	entityCtrl := geoctrl.NewEntityController(entitySvc, extractSvc)

	competitorRepo := georepo.NewGeoCompetitorRepositoryWithDB(gormDB)
	competitorSvc := geoservice.NewGeoCompetitorService(competitorRepo)
	competitorCtrl := geoctrl.NewCompetitorController(competitorSvc)

	alertSvc := geoservice.NewAlertService(georepo.NewGeoAlertRepositoryWithDB(gormDB))
	alertCtrl := geoctrl.NewAlertController(alertSvc)

	auditSvc := geoservice.NewAuditService(georepo.NewGeoAuditRepositoryWithDB(gormDB), alertSvc)
	auditCtrl := geoctrl.NewAuditController(auditSvc)

	searchConsoleSvc := geoservice.NewSearchConsoleService(
		georepo.NewGeoSearchQueryRepositoryWithDB(gormDB),
		georepo.NewGeoProbeRunRepositoryWithDB(gormDB),
		georepo.NewGeoPushRecordRepositoryWithDB(gormDB),
	)
	searchConsoleCtrl := geoctrl.NewSearchConsoleController(searchConsoleSvc)

	captureSvc := geoservice.NewCaptureService(
		georepo.NewGeoCaptureLinkRepositoryWithDB(gormDB),
		kbSvc,
		geoCaptureLeadPort(mainClueRepo, chainRepo, identitySvc),
		geoCaptureShortLinkAdapter{svc: cardsvc.NewShortLinkService(
			mainrepo.NewShortLinkRepository(gormDB),
			mainrepo.NewDomainPoolRepository(gormDB),
			mainrepo.NewShortLinkAccessRepository(gormDB),
		)},
		config.GetPublicBaseURL(),
	)
	captureCtrl := geoctrl.NewCaptureController(captureSvc)

	geo := auth.Group("/geo")

	geo.POST("/capture/links", captureCtrl.Create)
	geo.GET("/capture/links", captureCtrl.List)
	geo.GET("/capture/links/:id", captureCtrl.Get)
	geo.DELETE("/capture/links/:id", captureCtrl.Delete)

	geo.POST("/audit/site", auditCtrl.RunSite)
	geo.POST("/audit/content", auditCtrl.RunContent)
	geo.GET("/audit/runs", auditCtrl.List)
	geo.GET("/audit/runs/:id", auditCtrl.Get)
	geo.GET("/audit/runs/:id/report", auditCtrl.Report)
	geo.GET("/audit/compare", auditCtrl.Compare)

	geo.POST("/search-console/import", searchConsoleCtrl.Import)
	geo.GET("/reports/dual-engine", searchConsoleCtrl.DualEngine)

	geo.POST("/keywords/mine", keywordCtrl.MineKeywords)
	geo.POST("/keywords/expand", keywordCtrl.SemanticExpand)
	geo.POST("/keywords/cluster", keywordCtrl.TopicCluster)
	geo.GET("/keywords/list", keywordCtrl.GetKeywordList)
	geo.DELETE("/keywords/:id", keywordCtrl.DeleteKeyword)

	geo.POST("/content/generate", contentCtrl.GenerateContent)
	geo.POST("/content/batch-generate", contentCtrl.GenerateContentBatch) // B2 批量生成
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
	geo.GET("/reports/content-format", reportCtrl.ContentFormatAttribution) // A12 内容类型归因

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
	geo.POST("/techconfig/audit", tmCtrl.AuditContent)
	geo.POST("/techconfig/site-audit", tmCtrl.SiteAudit)

	// 一键自动驾驶（单关键词全链路，傻瓜式入口）
	geo.POST("/autopilot/runs", autopilotCtrl.Create)
	geo.GET("/autopilot/runs", autopilotCtrl.List)
	geo.GET("/autopilot/runs/:id", autopilotCtrl.Get)

	geo.POST("/metrics/analyze", tmCtrl.AnalyzeMetrics)

	// ⬇️ GEO v2 全链路路由
	// 关键词蒸馏 v2
	geo.POST("/keyword-mining/crawl-suggest", kwMiningCtrl.CrawlSuggest)
	geo.POST("/keyword-mining/longtail", kwMiningCtrl.CombineLongtail)
	geo.GET("/keyword-mining/funnel", kwMiningCtrl.BuildFunnel)
	geo.POST("/keyword-mining/pipeline", kwMiningCtrl.TriggerPipeline)
	geo.GET("/seed-terms", seedTermCtrl.List)
	geo.GET("/dicts", dictCtrl.List)

	// 蜘蛛推送 v2
	geo.POST("/push/urls", pushCtrl.PushURLs)
	geo.GET("/push/quota", pushCtrl.QuotaStatus)
	geo.GET("/push/sitemap", pushCtrl.GenerateSitemap)

	// 静态站部署 v2
	geo.POST("/site/export", siteCtrl.ExportToHugo)
	geo.POST("/site/deploy", siteCtrl.TriggerDeploy)
	geo.POST("/site/full-pipeline", siteCtrl.FullPipeline)
	geo.GET("/site/llms-txt-preview", siteCtrl.LlmsTxtPreview)
	geo.GET("/site/robots-preview", siteCtrl.RobotsTxtPreview)

	// 收录追踪 v2
	geo.POST("/index-tracking/verify/:article_id", indexCtrl.VerifyFull)
	geo.GET("/index-tracking/funnel", indexCtrl.FunnelStats)
	geo.POST("/index-tracking/verify-all", indexCtrl.ManualVerifyAll)

	// ⬇️ GEO v2 三张核心配置表 CRUD
	geo.GET("/sites", siteCRUDCtrl.List)
	geo.POST("/sites", siteCRUDCtrl.Create)
	geo.GET("/sites/:id", siteCRUDCtrl.Get)
	geo.PUT("/sites/:id", siteCRUDCtrl.Update)
	geo.DELETE("/sites/:id", siteCRUDCtrl.Delete)

	geo.GET("/schema-templates", schemaCRUDCtrl.List)
	geo.POST("/schema-templates", schemaCRUDCtrl.Create)
	geo.GET("/schema-templates/:id", schemaCRUDCtrl.Get)
	geo.PUT("/schema-templates/:id", schemaCRUDCtrl.Update)
	geo.DELETE("/schema-templates/:id", schemaCRUDCtrl.Delete)

	geo.GET("/keyword-enhance/analyze", keCtrl.Analyze)
	geo.POST("/keyword-enhance/enhance", keCtrl.Enhance)

	geo.GET("/probe/engines", probeCtrl.ListAvailableEngines)
	geo.GET("/probe/engines/meta", probeCtrl.ListEngineMeta)
	geo.POST("/probe/import", probeCtrl.Import)
	geo.POST("/probe/test", probeCtrl.TestSingle)
	geo.POST("/probe/all", probeCtrl.ProbeAll)
	geo.POST("/probe/run-negative", probeCtrl.RunNegativeMonitor)
	geo.POST("/probe/run-source-sync", probeCtrl.RunSourceSync)
	geo.POST("/probe/run-sov", probeCtrl.RunSOVRefresh)
	geo.GET("/probe/runs", probeCtrl.ListRuns)
	geo.GET("/citations/domains", citeCtrl.Domains)
	geo.GET("/citations/matrix", citeCtrl.Matrix)
	geo.GET("/citations/drift", citeCtrl.Drift) // A10 引用漂移 diff
	geo.GET("/opportunities", oppCtrl.List)

	geo.GET("/source-catalog/levels", sourceCtrl.LookupLevels)

	geo.GET("/entity/list", entityCtrl.ListEntities)
	geo.GET("/entity/:id/graph", entityCtrl.GetGraph)
	geo.POST("/entities/extract", entityCtrl.Extract)

	geoAdmin := geo.Group("")
	geoAdmin.Use(middleware.AdminAuthMiddleware())
	// 推送凭据含百度 token / Google 私钥 / IndexNow key：列表回显明文，必须管理员专属
	geo.GET("/pushers", pusherCRUDCtrl.List)
	geo.POST("/pushers", pusherCRUDCtrl.Create)
	geo.GET("/pushers/:id", pusherCRUDCtrl.Get)
	geo.PUT("/pushers/:id", pusherCRUDCtrl.Update)
	geo.DELETE("/pushers/:id", pusherCRUDCtrl.Delete)

	geoAdmin.PUT("/config", configCtrl.UpdateConfig)
	geoAdmin.POST("/config/optimize", configCtrl.OptimizeConfig)

	geoAdmin.POST("/platform/accounts", platformCtrl.SaveAccount)
	geoAdmin.DELETE("/platform/accounts/:id", platformCtrl.DeleteAccount)
	geoAdmin.POST("/platform/publish", platformCtrl.Publish)

	geo.POST("/workflow/workflows", wfCtrl.Create)
	geo.PUT("/workflow/workflows/:id", wfCtrl.Update)
	geo.DELETE("/workflow/workflows/:id", wfCtrl.Delete)
	geo.POST("/workflow/workflows/:id/run", wfCtrl.Run)
	geo.POST("/workflow/templates", wfCtrl.CreateTemplate)

	geo.POST("/kb/documents", kbCtrl.Save)
	geo.DELETE("/kb/documents/:id", kbCtrl.Delete)

	geoAdmin.GET("/jobs", jobCtrl.List)
	geoAdmin.GET("/jobs/runs", jobCtrl.Runs)
	geoAdmin.POST("/jobs/:name/trigger", jobCtrl.Trigger)
	geoAdmin.PUT("/jobs/:name/schedule", jobCtrl.UpdateSchedule)

	// 字典写操作归管理员：改定价/prompt/UA 等影响全站行为（读接口保持普通鉴权）
	geoAdmin.POST("/seed-terms/reseed", seedTermCtrl.Reseed)
	geoAdmin.PUT("/dicts", dictCtrl.Set)
	geoAdmin.DELETE("/dicts/:category/:key", dictCtrl.Delete)
}

// geoLeadReporter GEO 捕获线索计数。
//
// v3.56.0 起 GEO 捕获有独立类型号 18（存量 9 已改判），所以按类型统计即可；
// 若写死 type=9 且靠 source_id 前缀排除抖音线索——改判后恒为 0。
type geoLeadReporter struct{ clues mainrepo.ClueRepository }

func newGeoLeadReporter(clues mainrepo.ClueRepository) *geoLeadReporter {
	return &geoLeadReporter{clues: clues}
}

func (r *geoLeadReporter) CountCapturedLeads(ctx context.Context) (int64, error) {
	clueType := service.ClueTypeGeoCapture
	return r.clues.CountWithQuery(ctx, mainrepo.ClueQuery{Type: &clueType})
}

// geoCaptureLeadPort 把 GEO 承接线索写进主域 clues（类型 18），并在有 OneID 时
// 回填归因链。装配层共用这一份实现，避免工作流与公开落地页两套口径。
func geoCaptureLeadPort(clues mainrepo.ClueRepository, chains georepo.GeoQueryChainRepository, identitySvc *service.CustomerIdentityService) geoservice.LeadCapturePort {
	return geoservice.CaptureLeadFunc(func(ctx context.Context, contact, contactType, sourceID, intent string) (string, error) {
		oneID := ""
		if identitySvc != nil {
			ids := identity.Identifiers{}
			switch contactType {
			case "phone":
				ids.Phone = contact
			case "email":
				ids.Email = contact
			}
			if ids.Phone != "" || ids.Email != "" {
				if customer, err := identitySvc.IdentifyOrCreate(ctx, ids); err != nil {
					utils.WarnErrKV("router.geo.identifyLead", err, "contact_type", contactType)
				} else if customer != nil {
					oneID = customer.UnifiedID
				}
			}
		}
		clue := &model.Clue{
			Account:  contact,
			Name:     contact,
			SourceID: sourceID,
			Desc:     intent,
			OneID:    oneID,
			Type:     service.ClueTypeGeoCapture,
		}
		if err := clues.Create(ctx, clue); err != nil {
			return "", err
		}
		if clue.OneID != "" && sourceID != "" {
			_, err := chains.BindOneIDToChain(ctx, sourceID, clue.OneID)
			utils.WarnErrKV("router.geo.bindOneIDToChain", err, "source_id", sourceID, "one_id", clue.OneID, "clue_id", clue.ID)
		}
		return clue.ID, nil
	})
}

// geoCaptureShortLinkAdapter 把 cards.ShortLinkService 适配成 geo 的端口，
// 防止 geo service 反向依赖业务域。
type geoCaptureShortLinkAdapter struct{ svc cardsvc.ShortLinkService }

func (a geoCaptureShortLinkAdapter) CreateCaptureShortLink(ctx context.Context, code, targetURL, title string) (*geoservice.ShortLinkRef, error) {
	resp, err := a.svc.Create(ctx, &maindto.CreateShortLinkRequest{
		ShortCode:   code,
		OriginalURL: targetURL,
		Title:       title,
	})
	if err != nil {
		return nil, err
	}
	ref := &geoservice.ShortLinkRef{ID: resp.ID, Code: resp.ShortCode}
	if base := strings.TrimRight(config.GetPublicBaseURLFor(ctx), "/"); base != "" {
		ref.URL = base + "/api/s/" + resp.ShortCode
	}
	return ref, nil
}

func (a geoCaptureShortLinkAdapter) DeleteShortLink(ctx context.Context, id uint) error {
	return a.svc.Delete(ctx, id)
}

// SetupGeoCapturePublicRoutes 公开落地页三端点。
//
// 租户由 code 自证（PublicCodeTenant 先按 code 反查唯一 tenant_id，再绑定 ctx）。
// 全局限流在 router 装配期已挂；FAQ/留资另按链接做每日配额，防单页把 LLM 成本打穿。
func SetupGeoCapturePublicRoutes(public *gin.RouterGroup, gormDB *gorm.DB) {
	captureSvc := geoservice.NewCaptureService(
		georepo.NewGeoCaptureLinkRepositoryWithDB(gormDB),
		geoservice.NewKBService(georepo.NewGeoKnowledgeDocumentRepositoryWithDB(gormDB), geoservice.NewLLMAdapter()),
		geoCaptureLeadPort(mainrepo.NewClueRepositoryWithDB(gormDB), georepo.NewGeoQueryChainRepository(gormDB), service.NewCustomerIdentityService()),
		nil,
		"",
	)
	ctrl := geoctrl.NewPublicController(captureSvc)
	tenantGuard := middleware.PublicCodeTenant(gormDB, "geo_capture_links", "code")
	public.GET("/public/geo/l/:code", tenantGuard, ctrl.Landing)
	public.POST("/public/geo/l/:code/ask", tenantGuard, ctrl.Ask)
	public.POST("/public/geo/l/:code/lead", tenantGuard, ctrl.Lead)
}
