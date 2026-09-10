package router

import (
	"context"
	"os"
	"time"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	opsctrl "hivemtk-user/internal/ops/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"
	"hivemtk-user/internal/websocket"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupCustomerServiceRoutes(auth *gin.RouterGroup, aiAgentSvc *service.AIAgentService, langResolver *translation.LangConfigResolver) {
	customerSessionCtrl := controller.NewCustomerSessionController()
	auth.GET("/customer-sessions", customerSessionCtrl.GetSessions)
	auth.GET("/customer-sessions/pending", customerSessionCtrl.GetPendingSessions)
	auth.POST("/customer-sessions", customerSessionCtrl.CreateSession)
	auth.POST("/customer-sessions/assign", customerSessionCtrl.AssignSession)
	auth.GET("/customer-sessions/:id/messages", customerSessionCtrl.GetMessages)
	auth.POST("/customer-sessions/:id/messages", customerSessionCtrl.SendMessage)
	auth.POST("/customer-sessions/:id/auto-assign", customerSessionCtrl.AutoAssignSession)
	auth.POST("/customer-sessions/:id/close", customerSessionCtrl.CloseSession)
	auth.POST("/customer-sessions/:id/rate", customerSessionCtrl.RateSession)
	auth.POST("/customer-sessions/:id/transfer", customerSessionCtrl.TransferSession)
	auth.POST("/customer-sessions/:id/tags", customerSessionCtrl.TagSession)
	auth.POST("/customer-sessions/:id/takeover", customerSessionCtrl.Takeover)
	auth.POST("/customer-sessions/:id/release", customerSessionCtrl.Release)
	auth.POST("/customer-sessions/:id/switch-handler", customerSessionCtrl.SwitchHandler)
	auth.GET("/customer-sessions/blacklist", customerSessionCtrl.ListActiveBlacklist)
	auth.GET("/customer-sessions/blacklist/check", customerSessionCtrl.IsUserBlacklisted)
	auth.POST("/customer-sessions/blacklist/remove", customerSessionCtrl.Unblacklist)
	auth.POST("/customer-sessions/:id/blacklist", customerSessionCtrl.Blacklist)
	auth.PUT("/customer-sessions/:id/status", customerSessionCtrl.UpdateSessionStatus)
	auth.GET("/customer-sessions/:id", customerSessionCtrl.GetSessionByID)

	agentStatusCtrl := controller.NewAgentStatusController(aiAgentSvc)
	auth.POST("/agents", agentStatusCtrl.CreateAgent)
	auth.GET("/agents/me", agentStatusCtrl.GetMyAgent)
	auth.GET("/agents/all", agentStatusCtrl.ListAllAgents)
	auth.GET("/agents/online", agentStatusCtrl.GetOnlineAgents)
	auth.GET("/agents/:id", agentStatusCtrl.GetAgentStatus)
	auth.PUT("/agents/:id/status", agentStatusCtrl.UpdateAgentStatus)
	auth.POST("/agents/:id/online", agentStatusCtrl.GoOnline)
	auth.POST("/agents/:id/offline", agentStatusCtrl.GoOffline)
	auth.GET("/agents/:id/sessions", agentStatusCtrl.GetAgentSessions)

	quickReplyCtrl := controller.NewQuickReplyController()
	auth.GET("/quick-replies", quickReplyCtrl.GetReplies)
	auth.GET("/quick-replies/categories", quickReplyCtrl.GetReplyCategories)
	quickAdmin := auth.Group("/quick-replies", middleware.AdminAuthMiddleware())
	quickAdmin.POST("", quickReplyCtrl.CreateReply)
	quickAdmin.PUT("/:id", quickReplyCtrl.UpdateReply)
	quickAdmin.DELETE("/:id", quickReplyCtrl.DeleteReply)

	sessionTagCtrl := controller.NewSessionTagController()
	auth.GET("/session-tags", sessionTagCtrl.GetTags)
	sessAdmin := auth.Group("/session-tags", middleware.AdminAuthMiddleware())
	sessAdmin.POST("", sessionTagCtrl.CreateTag)
	sessAdmin.PUT("/:id", sessionTagCtrl.UpdateTag)
	sessAdmin.DELETE("/:id", sessionTagCtrl.DeleteTag)

	aiSuggestionCtrl := controller.NewAISuggestionController()
	auth.GET("/ai-suggestions/:session_id", aiSuggestionCtrl.GetSuggestions)
	auth.POST("/ai-suggestions/:id/use", aiSuggestionCtrl.UseSuggestion)

	csQueueCtrl := controller.NewCustomerServiceController()
	auth.GET("/customer-service/queue", csQueueCtrl.GetQueue)
	auth.GET("/customer-service/capacity", csQueueCtrl.GetCapacity)
	auth.GET("/customer-service/agents", csQueueCtrl.GetAgents)

	wsHandler := websocket.NewWSHandler()
	wsHandler.SetLangResolver(langResolver)
	auth.GET("/ws/agent", wsHandler.HandleWebSocket)

	customer360Ctrl := controller.NewCustomer360Controller()
	auth.GET("/customer-360", customer360Ctrl.GetCustomer360)
	auth.GET("/customer-360/list", customer360Ctrl.GetCustomerList)
	auth.GET("/customer-360/basic", customer360Ctrl.GetCustomerBasicInfo)
	auth.GET("/customer-360/stats", customer360Ctrl.GetCustomerStats)
	auth.GET("/customer-360/sessions", customer360Ctrl.GetCustomerSessions)
	auth.GET("/customer-360/messages", customer360Ctrl.GetCustomerMessages)
	auth.GET("/customer-360/events", customer360Ctrl.GetCustomerEvents)
	auth.GET("/customer-360/orders", customer360Ctrl.GetCustomerOrders)
	auth.PUT("/customer-360/tags", customer360Ctrl.UpdateCustomerTags)
	auth.GET("/customer-360/tags", customer360Ctrl.GetCustomerTags)

	customerCtrl := controller.NewCustomerController()
	auth.POST("/customer", customerCtrl.CreateCustomer)
	auth.GET("/customer/list", customer360Ctrl.GetCustomerList)
	auth.GET("/customer/360/:id", customer360Ctrl.GetCustomer360ByID)
	auth.GET("/customer/:id", customer360Ctrl.GetCustomerDetail)
	auth.GET("/customer/:id/behaviors", customer360Ctrl.GetCustomerBehaviors)
	auth.GET("/customer/:id/communications", customer360Ctrl.GetCustomerCommunications)
	auth.PUT("/customer/:id", customer360Ctrl.UpdateCustomer)
	auth.POST("/customer/:id/tags", customer360Ctrl.AddCustomerTag)
	auth.DELETE("/customer/:id/tags/:tag", customer360Ctrl.RemoveCustomerTag)

	auth.GET("/customer-360/tag-rules", customer360Ctrl.ListTagRules)
	auth.POST("/customer-360/tag-rules", customer360Ctrl.SaveTagRule)
	auth.PUT("/customer-360/tag-rules/:id", customer360Ctrl.UpdateTagRule)
	auth.DELETE("/customer-360/tag-rules/:id", customer360Ctrl.DeleteTagRule)
	auth.GET("/customer-360/tag-stats", customer360Ctrl.GetTagStats)

	oneIDCtrl := controller.NewCustomerOneIDController()
	auth.GET("/customer/oneid/list", oneIDCtrl.ListOneID)
	auth.GET("/customer/oneid/stats", oneIDCtrl.OneIDStats)
	auth.GET("/customer/oneid/conflicts", oneIDCtrl.ListConflicts)
	auth.POST("/customer/oneid/merge", oneIDCtrl.MergeIdentity)
	auth.POST("/customer/oneid/resolve", oneIDCtrl.ResolveIdentity)
	auth.POST("/customer/oneid/conflicts/:id/resolve", oneIDCtrl.ResolveConflict)
	auth.GET("/customer-oneid/:customer_id/identities", oneIDCtrl.GetIdentityMappings)
	auth.POST("/customer-oneid/:customer_id/identities", oneIDCtrl.LinkIdentity)

	auth.GET("/customer/session/list", customerSessionCtrl.GetSessions)
	auth.POST("/customer/session", customerSessionCtrl.CreateSession)
	auth.POST("/customer/session/messages", customerSessionCtrl.SendMessage)
	auth.GET("/customer/session/:id/messages", customerSessionCtrl.GetMessages)
	auth.POST("/customer/session/:id/close", customerSessionCtrl.CloseSession)
	auth.POST("/customer/session/:id/transfer", customerSessionCtrl.TransferSession)

	appConfigCtrl := controller.NewAppConfigController()

	auth.GET("/app-config/health", appConfigCtrl.HealthCheck)

	appAdmin := auth.Group("", middleware.AdminAuthMiddleware())
	appAdmin.GET("/app-config", appConfigCtrl.GetAppConfig)
	appAdmin.PUT("/app-config", appConfigCtrl.UpdateAppConfig)
	appAdmin.POST("/app-config/sync", appConfigCtrl.SyncWithPlatform)
}

func setupMessageRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	unifiedMsgCtrl := controller.NewUnifiedMessageController()
	auth.GET("/messages", unifiedMsgCtrl.GetMessages)
	auth.GET("/messages/:id", unifiedMsgCtrl.GetMessageByID)

	messageHubCtrl := controller.NewMessageHubController(service.NewMessageHubService())
	auth.POST("/message-hub/push", messageHubCtrl.Push)
	auth.POST("/message-hub/push-batch", messageHubCtrl.PushBatch)
	auth.POST("/message-hub/push-from-channel", messageHubCtrl.PushFromChannel)
	auth.GET("/message-hub/list", messageHubCtrl.List)
	auth.GET("/message-hub/stats", messageHubCtrl.Stats)
	auth.GET("/message-hub/platforms", messageHubCtrl.Platforms)
	auth.GET("/message-hub/:id", messageHubCtrl.GetByID)
	auth.POST("/message-hub/:id/read", messageHubCtrl.MarkRead)

	inboxIngressCtrl := controller.NewInboxIngressController(service.NewInboxIngressService())
	auth.POST("/inbox/lock-human", inboxIngressCtrl.LockHuman)
	auth.POST("/inbox/unlock-human/:session_id", inboxIngressCtrl.UnlockHuman)

	// 统一收件箱：会话工作台（列表/详情/已读/置顶/星标/静音/标签/分配/统计/对账/消息线程）
	inboxCtrl := controller.NewInboxController(service.NewInboxService())
	auth.GET("/inbox/conversations", inboxCtrl.List)
	auth.GET("/inbox/stats", inboxCtrl.Stats)
	auth.GET("/inbox/assignments", inboxCtrl.ListAssignments)
	auth.GET("/inbox/staff/:staff/load", inboxCtrl.StaffLoad)
	auth.GET("/inbox/conversations/:id", inboxCtrl.GetByID)
	auth.GET("/inbox/conversations/:id/messages", inboxCtrl.GetMessages)
	auth.POST("/inbox/conversations/:id/read", inboxCtrl.MarkRead)
	auth.POST("/inbox/conversations/:id/pin", inboxCtrl.Pin)
	auth.POST("/inbox/conversations/:id/star", inboxCtrl.Star)
	auth.POST("/inbox/conversations/:id/mute", inboxCtrl.Mute)
	auth.POST("/inbox/conversations/:id/tags", inboxCtrl.AddTag)
	auth.DELETE("/inbox/conversations/:id/tags/:tag", inboxCtrl.RemoveTag)
	auth.DELETE("/inbox/conversations/:id/messages/:mid", inboxCtrl.DeleteMessage)
	auth.POST("/inbox/assign", inboxCtrl.Assign)
	auth.POST("/inbox/assign/auto", inboxCtrl.AutoAssign)
	auth.POST("/inbox/reconcile", inboxCtrl.Reconcile)
}

func setupPlatformAccountRoutes(auth *gin.RouterGroup) {
	platformAccountCtrl := controller.NewPlatformAccountController()
	auth.GET("/platform-accounts", platformAccountCtrl.GetAccounts)
	auth.GET("/platform-accounts/platforms", platformAccountCtrl.GetSupportedPlatforms)
	auth.GET("/platform-accounts/:id", platformAccountCtrl.GetAccountByID)
	auth.GET("/platform-accounts/:id/status", platformAccountCtrl.CheckLoginStatus)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/platform-accounts", platformAccountCtrl.CreateAccount)
	admin.PUT("/platform-accounts/:id", platformAccountCtrl.UpdateAccount)
	admin.DELETE("/platform-accounts/:id", platformAccountCtrl.DeleteAccount)
	admin.POST("/platform-accounts/:id/login", platformAccountCtrl.LoginAccount)
}

func setupWeComHealthRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	wcHCtrl := controller.NewWeComHealthController(service.NewWeComAccountHealthService(db), service.NewWeComIntegrationService(db))
	auth.GET("/wecom/health/accounts", wcHCtrl.ListAccountsWithHealth)
	auth.GET("/wecom/health/accounts/risks", wcHCtrl.GetRiskAccounts)
	auth.GET("/wecom/health/accounts/select", wcHCtrl.SelectHealthyAccount)
	auth.GET("/wecom/health/accounts/summary", wcHCtrl.GetHealthSummary)
	auth.GET("/wecom/health/accounts/:id", wcHCtrl.GetLatestHealth)
	auth.GET("/wecom/health/accounts/:id/history", wcHCtrl.ListHealthHistory)
	auth.POST("/wecom/health/accounts/:id", wcHCtrl.ReportHealth)
	auth.POST("/wecom/health/accounts/:id/status", wcHCtrl.UpdateAccountStatus)
	auth.POST("/wecom/health/accounts/:id/quota/consume", wcHCtrl.ConsumeQuota)
	auth.POST("/wecom/health/accounts/quota/reset", wcHCtrl.ResetDailyQuota)
	auth.POST("/wecom/messages/ingest", wcHCtrl.IngestMessage)
	auth.POST("/wecom/messages/send", wcHCtrl.SendMessage)
}

func setupIntentRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	rec := service.GetIntentRecognizer()
	if rec == nil {
		rec = service.NewIntentRecognizer(db, app.GetGlobalDispatcher(), nil)
	}
	intentCtrl := controller.NewIntentController(rec)
	auth.POST("/intent/recognize", intentCtrl.Recognize)
	auth.POST("/intent/recognize/batch", intentCtrl.BatchRecognize)
	auth.GET("/intent/stats", intentCtrl.Stats)
	auth.GET("/intent/recent", intentCtrl.RecentIntents)
	auth.GET("/intent/dict", intentCtrl.Intents)
	auth.POST("/intent/recognize/fine", intentCtrl.RecognizeFine)
	auth.GET("/intent/logs", intentCtrl.IntentLogs)
	auth.GET("/intent/stats/fine", intentCtrl.IntentStatsFine)
	auth.GET("/intent/config", intentCtrl.GetConfig)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.PUT("/intent/config", intentCtrl.UpdateConfig)
}

func setupLLMProviderRoutes(auth *gin.RouterGroup) {
	failoverSvc := service.NewLLMFailoverService(app.GetGlobalProviderFailover())
	llmProvCtrl := controller.NewLLMProviderController(failoverSvc)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/llm/providers/circuit/reset", llmProvCtrl.ResetCircuit)
	admin.POST("/llm/providers/circuit/reset/:provider", llmProvCtrl.ResetCircuit)
	admin.PUT("/llm/providers/policy", llmProvCtrl.UpdatePolicy)
	admin.POST("/llm-routings/providers/circuit/reset", llmProvCtrl.ResetCircuit)
	admin.PUT("/llm-routings/policy", llmProvCtrl.UpdatePolicy)

	auth.GET("/llm/providers/health", llmProvCtrl.GetHealth)
	auth.GET("/llm/providers/health/:provider", llmProvCtrl.GetProviderHealth)
	auth.GET("/llm/providers/policy", llmProvCtrl.GetPolicy)
	auth.GET("/llm-routings/providers/health", llmProvCtrl.GetHealth)
	auth.GET("/llm-routings/providers/:provider/health", llmProvCtrl.GetProviderHealth)
	auth.GET("/llm-routings/policy", llmProvCtrl.GetPolicy)

	auth.POST("/llm-routings/resolve", llmProvCtrl.ResolveRoute)
}

func setupTraceRoutes(auth *gin.RouterGroup) {
	traceCtrl := controller.NewTraceController()
	auth.GET("/traces/recent", traceCtrl.ListRecent)
	auth.GET("/trace/:traceId", traceCtrl.GetTrace)
}

func setupSSEDashboardRoutes(auth *gin.RouterGroup) {
	sseCtrl := controller.NewSSEDashboardController()
	auth.GET("/dashboard/sse", sseCtrl.Stream)
	auth.GET("/dashboard/clients", sseCtrl.ListClients)
	auth.GET("/dashboard/topics", sseCtrl.ListTopics)
	auth.GET("/dashboard/stats", sseCtrl.Stats)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/dashboard/broadcast", sseCtrl.Broadcast)
}

func setupDialogueMemoryRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	memCtrl := controller.NewDialogueMemoryController(service.NewDialogueMemoryService(db, nil))
	auth.POST("/memory/messages", memCtrl.AppendMessage)
	auth.GET("/memory/short", memCtrl.ShortTerm)
	auth.GET("/memory/long", memCtrl.LongTerm)
	auth.POST("/memory/facts", memCtrl.UpdateKeyFacts)
	auth.POST("/memory/objections", memCtrl.RecordObjection)
	auth.POST("/memory/purchase-intent", memCtrl.UpdatePurchaseIntent)
	auth.POST("/memory/intent-trail", memCtrl.RecordIntent)
	auth.POST("/memory/sop-history", memCtrl.RecordSOP)
	auth.GET("/memory/context", memCtrl.BuildContext)
	auth.GET("/memory/list", memCtrl.Stats)
}

func setupSOPRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	sopCtrl := controller.NewSOPController(service.NewSOPService(db, nil))

	sopAgentRepo := repository.NewSopAgentRepository(db)
	sopExecRepo := repository.NewSopExecutionRepository(db)
	heatmapCtrl := controller.NewSopHeatmapController(service.NewSopHeatmapService(sopAgentRepo, sopExecRepo))

	auth.GET("/sop", sopCtrl.List)
	auth.GET("/sop/stats", sopCtrl.Stats)
	auth.GET("/sop/match", sopCtrl.MatchByIntent)
	auth.GET("/sop/executions", sopCtrl.ListExecutions)
	auth.GET("/sop/executions/:id", sopCtrl.GetExecution)
	auth.GET("/sop/:id", sopCtrl.Get)
	auth.GET("/sop/:id/abtest/stats", sopCtrl.GetABTestStats)

	auth.GET("/sop/:id/heatmap", heatmapCtrl.GetHeatmap)

	admin := auth.Group("/sop", middleware.AdminAuthMiddleware())
	{
		admin.POST("", sopCtrl.Create)
		admin.PUT("/:id", sopCtrl.Update)
		admin.DELETE("/:id", sopCtrl.Delete)
		admin.POST("/:id/activate", sopCtrl.Activate)
		admin.POST("/:id/deactivate", sopCtrl.Deactivate)
		admin.POST("/executions/:id/pause", sopCtrl.Pause)
		admin.POST("/executions/:id/resume", sopCtrl.Resume)
		admin.POST("/executions/:id/cancel", sopCtrl.Cancel)
		admin.PUT("/:id/abtest/config", sopCtrl.UpdateABTestConfig)
		admin.POST("/execute", sopCtrl.Execute)
		admin.POST("/step", sopCtrl.Step)
	}

	faqCtrl := controller.NewFAQController()
	faqCtrl.RegisterRoutes(auth)

	sopTplCtrl := controller.NewSOPTemplateController()
	sopTplCtrl.RegisterRoutes(auth)

	kbCtrl := controller.NewKnowledgeBaseController()
	kbCtrl.RegisterRoutes(auth)

	bindCtrl := controller.NewAgentKBBindingController()
	bindCtrl.RegisterRoutes(auth)
}

func setupReachPipelineRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	reachSvc := service.NewReachPipelineService(db)
	if hook := service.NewHTTPAlertHook(os.Getenv("ALERT_WEBHOOK_URL")); hook != nil {
		reachSvc.SetAlertHook(hook)
	}
	if sender := app.NewPipelineReachSender(db); sender != nil {
		reachSvc.SetReachSender(sender)
	}

	app.RegisterAllReachServices(db)
	reachSvc.StartDispatcher(context.Background(), 15*time.Second)
	reachCtrl := controller.NewReachPipelineController(reachSvc)

	auth.GET("/reach/pipelines", reachCtrl.ListPipelines)
	auth.GET("/reach/stats", reachCtrl.Stats)
	auth.GET("/reach/jobs", reachCtrl.ListJobs)
	auth.GET("/reach/pipelines/:id", reachCtrl.GetPipeline)
	auth.GET("/reach/jobs/:id", reachCtrl.GetJob)
	auth.GET("/reach/jobs/with-experiment", reachCtrl.ListJobsWithExperiment)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/reach/pipelines", reachCtrl.CreatePipeline)
	admin.POST("/reach/jobs", reachCtrl.EnqueueJob)
	admin.POST("/reach/rate-limit/reset", reachCtrl.ResetRateLimit)
	admin.PUT("/reach/pipelines/:id", reachCtrl.UpdatePipeline)
	admin.DELETE("/reach/pipelines/:id", reachCtrl.DeletePipeline)
	admin.POST("/reach/pipelines/:id/pause", reachCtrl.PausePipeline)
	admin.POST("/reach/pipelines/:id/resume", reachCtrl.ResumePipeline)
	admin.POST("/reach/pipelines/:id/archive", reachCtrl.ArchivePipeline)
	admin.POST("/reach/jobs/:id/cancel", reachCtrl.CancelJob)
	admin.POST("/reach/jobs/:id/retry", reachCtrl.RetryJob)
	admin.POST("/reach/jobs/:id/execute", reachCtrl.ExecuteJob)
}

func setupProactiveReachRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	reachSvc := service.NewReachPipelineService(db)
	proactiveSvc := service.NewProactiveReachService(db, nil)
	service.BindProactiveReachSenders(proactiveSvc, db)
	proactiveCtrl := controller.NewProactiveReachController(proactiveSvc)

	auth.POST("/reach/proactive/send", proactiveCtrl.ProactiveSend)
	auth.POST("/reach/proactive/quick", proactiveCtrl.QuickSend)
	auth.POST("/reach/proactive/validate", proactiveCtrl.ValidateReach)

	auth.POST("/reach/proactive/customer/:customer_id", proactiveCtrl.ProactiveSendFromCustomer)
	auth.GET("/reach/proactive/customer/:customer_id/channels", proactiveCtrl.ListChannels)

	auth.POST("/reach/proactive/batch", proactiveCtrl.BatchProactiveSend)

	_ = reachSvc
}

func setupLLMRoutingRoutes(auth *gin.RouterGroup) {
	dispatcher := app.GetGlobalDispatcher()
	routingService := service.NewLLMRoutingService(dispatcher)
	llmCtrl := controller.NewLLMRoutingController(routingService)
	if f := app.GetGlobalProviderFailover(); f != nil {
		llmCtrl.SetFailover(service.NewLLMFailoverService(f))
	}

	admin := auth.Group("/llm", middleware.AdminAuthMiddleware())
	{
		admin.POST("/models", llmCtrl.CreateModel)
		admin.PUT("/models/:name", llmCtrl.UpdateModel)
		admin.DELETE("/models/:name", llmCtrl.DeleteModel)
		admin.POST("/models/:name/test", llmCtrl.TestModel)
		admin.PUT("/strategies", llmCtrl.UpdateStrategies)
		admin.POST("/strategies", llmCtrl.UpdateStrategies)
		admin.PUT("/scene-routing", llmCtrl.UpdateSceneRouting)
		admin.POST("/scene-routing", llmCtrl.UpdateSceneRouting)
		admin.PUT("/fallback", llmCtrl.UpdateSceneRouting)
		admin.POST("/fallback", llmCtrl.UpdateSceneRouting)
	}

	auth.GET("/llm/models", llmCtrl.ListModels)
	auth.GET("/llm/models/:name", llmCtrl.GetModel)
	auth.GET("/llm/strategies", llmCtrl.ListStrategies)
	auth.GET("/llm/audit", llmCtrl.ListAuditHistory)
	auth.GET("/llm/stats", llmCtrl.Stats)
	auth.GET("/llm/usage", llmCtrl.Usage)
	auth.GET("/llm/cost-stats", llmCtrl.CostStats)
	auth.GET("/llm/fallback", llmCtrl.FallbackConfig)
	auth.GET("/llm/scene-routing", llmCtrl.ListStrategies)
	auth.GET("/llm/scenarios", llmCtrl.ListScenarios)
	auth.GET("/llm/health", llmCtrl.Health)
	auth.GET("/llm/scenario-stats", llmCtrl.ScenarioStats)
	auth.GET("/llm/model-type-stats", llmCtrl.ModelTypeStats)
	auth.GET("/llm/egress-alerts", llmCtrl.EgressAlerts)
	auth.GET("/llm/egress-audit", llmCtrl.EgressAudit)
}

func setupAnalyticsRoutes(auth *gin.RouterGroup) {
	funnelCtrl := opsctrl.NewConversionFunnelController()
	auth.GET("/analytics/funnel", funnelCtrl.GetFunnel)
	auth.GET("/analytics/funnel/stage", funnelCtrl.GetStageDetails)

	aiProdCtrl := opsctrl.NewAIProductivityController()
	auth.GET("/analytics/ai-productivity", aiProdCtrl.GetReport)
	auth.GET("/analytics/ai-productivity/trend", aiProdCtrl.GetDailyTrend)

	personaCtrl := controller.NewSalesPersonaController()
	auth.GET("/analytics/persona/staffs", personaCtrl.ListStaffs)
	auth.GET("/analytics/persona/staffs/:id", personaCtrl.GetReport)
}

func setupObjectionHandlerRoutes(auth *gin.RouterGroup) {
	objCtrl := controller.NewObjectionHandlerController()
	auth.POST("/objection/handle", objCtrl.Handle)
	auth.POST("/objection/classify", objCtrl.Classify)
	auth.GET("/objection/categories", objCtrl.ListCategories)
	auth.POST("/objection/usage", objCtrl.RecordUsage)
}

func setupCustomerJourneyRoutes(auth *gin.RouterGroup) {
	journeyCtrl := controller.NewCustomerJourneyController()
	auth.GET("/customer-journey/overview", journeyCtrl.GetOverview)
	auth.GET("/customer-journey/stages", journeyCtrl.ListStages)
	auth.GET("/customer-journey/by-stage", journeyCtrl.ListByStage)
	auth.POST("/customer-journey/transition", journeyCtrl.TransitionStage)
	auth.POST("/customer-journey/touch", journeyCtrl.TouchCustomer)
}

func setupQualityRoutes(auth *gin.RouterGroup) {
	perfCtrl := opsctrl.NewPerformanceTestController()

	auth.GET("/perf/list", perfCtrl.ListResults)
	auth.GET("/perf/:id", perfCtrl.GetResult)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/perf/run", perfCtrl.RunTest)
}

func setupSecurityAuditRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	ctrl := controller.NewSecurityAuditController(service.NewSecurityAuditService(gormDB))
	auth.GET("/security/audit/list", ctrl.ListSecurityAudits)
	auth.GET("/security/audit/:id", ctrl.GetSecurityAudit)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/security/audit", ctrl.RunSecurityAudit)
}
