package router

import (
	contentctrl "hivemtk-user/internal/content/controller"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	opsctrl "hivemtk-user/internal/ops/controller"

	"github.com/gin-gonic/gin"
)

func setupBatchRoutes(auth *gin.RouterGroup) {
	batchImportCtrl := contentctrl.NewBatchImportController()
	batchExportCtrl := contentctrl.NewBatchExportController()
	batchOpCtrl := contentctrl.NewBatchOperationController()

	auth.GET("/batch/template", batchImportCtrl.DownloadTemplate)
	auth.POST("/batch/export", batchExportCtrl.ExportData)
	auth.GET("/batch/tools", batchOpCtrl.GetTools)
	auth.GET("/batch/histories", batchOpCtrl.GetHistories)
	auth.GET("/batch/histories/:id", batchOpCtrl.GetHistoryByID)
	auth.POST("/batch/preview", batchOpCtrl.Preview)

	admin := auth.Group("/batch", middleware.AdminAuthMiddleware())
	{
		admin.POST("/import", batchImportCtrl.ImportFile)
		admin.POST("/delete", batchOpCtrl.BatchDelete)
		admin.POST("/update", batchOpCtrl.BatchUpdate)
		admin.POST("/histories/:id/cancel", batchOpCtrl.CancelHistory)
	}
}

func setupAIContentRoutes(auth *gin.RouterGroup) {
	aiContentCtrl := contentctrl.NewAIContentController()
	auth.GET("/ai/history", aiContentCtrl.GetGenerationHistory)
	auth.GET("/ai/history/:id", aiContentCtrl.GetRecordByID)
	auth.POST("/ai/history", aiContentCtrl.CreateHistory)
	auth.POST("/ai/history/:id/favorite", aiContentCtrl.FavoriteRecord)
	auth.POST("/ai/history/:id/rate", aiContentCtrl.RateRecord)
	auth.GET("/ai/templates", aiContentCtrl.GetTemplates)
	auth.GET("/ai/templates/:id", aiContentCtrl.GetTemplateByID)
	auth.GET("/ai/template-types", aiContentCtrl.GetTemplateTypes)
	admin := auth.Group("/ai", middleware.AdminAuthMiddleware())
	{
		admin.POST("/generate", aiContentCtrl.GenerateContent)
		admin.POST("/history/:id/save", aiContentCtrl.SaveRecord)
		admin.DELETE("/history/:id", aiContentCtrl.DeleteRecord)
		admin.POST("/templates", aiContentCtrl.CreateTemplate)
		admin.PUT("/templates/:id", aiContentCtrl.UpdateTemplate)
		admin.DELETE("/templates/:id", aiContentCtrl.DeleteTemplate)
	}
}

func setupUserSegmentRoutes(auth *gin.RouterGroup) {
	userSegmentCtrl := controller.NewUserSegmentController()
	auth.GET("/user-segment/rfm/rule", userSegmentCtrl.GetRFMRule)
	auth.GET("/user-segment/rfm/rules", userSegmentCtrl.ListRFMRules)
	auth.GET("/user-segment/rfm/list", userSegmentCtrl.GetRFMList)
	auth.GET("/user-segment/rfm/user", userSegmentCtrl.GetUserRFM)
	auth.GET("/user-segment/rfm/stats", userSegmentCtrl.GetRFMStats)
	auth.GET("/user-segment/layers", userSegmentCtrl.GetLayerDescription)
	admin := auth.Group("/user-segment/rfm", middleware.AdminAuthMiddleware())
	{
		admin.POST("/rule", userSegmentCtrl.SaveRFMRule)
		admin.PUT("/rule/:id", userSegmentCtrl.UpdateRFMRule)
		admin.DELETE("/rule/:id", userSegmentCtrl.DeleteRFMRule)
		admin.POST("/calculate", userSegmentCtrl.CalculateRFM)
	}
}

func setupMarketingFlowRoutes(auth *gin.RouterGroup) {
	marketingFlowCtrl := contentctrl.NewMarketingFlowController()

	extrasCtrl := controller.NewCustomerEventBatchController()
	auth.POST("/customer-events/batch", extrasCtrl.TrackBatch)

	auth.GET("/ai/sales-cockpit", controller.NewSalesCockpitController().GetCockpit)
	wvCtrl := controller.NewWebVitalsController()
	auth.POST("/monitor/web-vitals", wvCtrl.Report)
	mfSyncCtrl := controller.NewMarketingFlowSyncController()

	auth.GET("/marketing-flows", marketingFlowCtrl.GetFlowList)
	auth.GET("/marketing-flows/:id", marketingFlowCtrl.GetFlowByID)
	auth.GET("/marketing-flows/:id/executions", marketingFlowCtrl.GetExecutionList)
	auth.GET("/marketing-flows/:id/stats", marketingFlowCtrl.GetExecutionStats)
	admin := auth.Group("/marketing-flows", middleware.AdminAuthMiddleware())
	{
		admin.POST("", marketingFlowCtrl.CreateFlow)
		admin.PUT("/:id", marketingFlowCtrl.UpdateFlow)
		admin.DELETE("/:id", marketingFlowCtrl.DeleteFlow)
		admin.POST("/:id/activate", marketingFlowCtrl.ActivateFlow)
		admin.POST("/:id/pause", marketingFlowCtrl.PauseFlow)
		admin.POST("/:id/stop", marketingFlowCtrl.StopFlow)
		admin.POST("/:id/trigger", marketingFlowCtrl.Trigger)
		admin.POST("/:id/sync-ab-results", mfSyncCtrl.SyncABResults)
	}
}

func setupCustomReportRoutes(auth *gin.RouterGroup) {
	customReportCtrl := opsctrl.NewCustomReportController()
	auth.GET("/custom-reports", customReportCtrl.GetReportList)
	auth.GET("/custom-reports/:id", customReportCtrl.GetReport)
	auth.POST("/custom-reports", customReportCtrl.CreateReport)
	auth.PUT("/custom-reports/:id", customReportCtrl.UpdateReport)
	auth.DELETE("/custom-reports/:id", customReportCtrl.DeleteReport)
	auth.GET("/custom-reports/templates", customReportCtrl.GetPublicTemplates)
	auth.POST("/custom-reports/templates/:id/use", customReportCtrl.UseTemplate)
	auth.GET("/custom-reports/:id/data", customReportCtrl.QueryReportData)
	auth.GET("/custom-reports/:id/export", customReportCtrl.ExportCSV)
}

func setupDashboardRoutes(auth *gin.RouterGroup, public *gin.RouterGroup) {
	dashboardCtrl := opsctrl.NewDashboardScreenController()
	auth.GET("/dashboards", dashboardCtrl.GetScreenList)
	auth.GET("/dashboards/data", dashboardCtrl.GetDashboardData)
	auth.GET("/dashboards/activities", dashboardCtrl.GetRealtimeActivities)
	auth.GET("/dashboards/:id", dashboardCtrl.GetScreenByID)
	dashAdmin := auth.Group("/dashboards", middleware.AdminAuthMiddleware())
	dashAdmin.POST("", dashboardCtrl.CreateScreen)
	dashAdmin.PUT("/:id", dashboardCtrl.UpdateScreen)
	dashAdmin.DELETE("/:id", dashboardCtrl.DeleteScreen)

	public.GET("/dashboards/public/:code", dashboardCtrl.PublicViewScreen)
}

func setupTemplateRoutes(auth *gin.RouterGroup) {
	templateCtrl := contentctrl.NewTemplateMarketController()
	auth.GET("/templates", templateCtrl.GetTemplateList)
	auth.POST("/templates", templateCtrl.CreateTemplate)
	auth.GET("/templates/:id", templateCtrl.GetTemplateByID)
	auth.POST("/templates/:id/download", templateCtrl.DownloadTemplate)
	auth.POST("/templates/:id/rate", templateCtrl.RateTemplate)
	auth.GET("/templates/official", templateCtrl.GetOfficialTemplates)
	auth.GET("/templates/search", templateCtrl.SearchTemplates)
	auth.GET("/templates/my-downloads", templateCtrl.GetMyDownloads)
}

func setupScriptRoutes(auth *gin.RouterGroup) {
	scriptCtrl := contentctrl.NewScriptTemplateController()
	auth.GET("/scripts", scriptCtrl.GetTemplateList)
	auth.GET("/scripts/:id", scriptCtrl.GetTemplateByID)
	auth.POST("/scripts", scriptCtrl.CreateTemplate)
	auth.PUT("/scripts/:id", scriptCtrl.UpdateTemplate)
	auth.DELETE("/scripts/:id", scriptCtrl.DeleteTemplate)
	auth.GET("/scripts/categories", scriptCtrl.GetCategories)
	auth.GET("/scripts/search", scriptCtrl.SearchTemplates)
	auth.GET("/scripts/public", scriptCtrl.GetPublicTemplates)
	auth.POST("/scripts/recommend", scriptCtrl.RecommendScript)
	auth.POST("/scripts/sync-to-library", scriptCtrl.SyncToLibrary)

	scriptLibCtrl := controller.NewScriptLibraryController()
	auth.GET("/script-library/:id/versions", scriptLibCtrl.ListVersions)
	auth.POST("/script-library/:id/versions", scriptLibCtrl.CreateVersion)
	auth.PUT("/script-library/:id/versions/:vid/activate", scriptLibCtrl.ActivateVersion)
	auth.POST("/script-library/:id/expire", scriptLibCtrl.ExpireScript)
	auth.GET("/script-library/:id/ab-stats", scriptLibCtrl.GetABStats)
	auth.PUT("/script-library/:id/ab-config", scriptLibCtrl.UpdateABConfig)
	auth.POST("/script-ab/conversion", scriptLibCtrl.RecordConversion)

	flagCtrl := controller.NewFeatureFlagController()
	auth.GET("/feature-flags", flagCtrl.List)
	auth.GET("/feature-flags/stale", flagCtrl.Stale)
	auth.GET("/feature-flags/:id", flagCtrl.Get)
	auth.GET("/feature-flags/:id/audit", flagCtrl.Audit)
	auth.GET("/feature-flags/:id/eval-log", flagCtrl.EvalLogs)
	auth.GET("/feature-flags/:id/code-references", flagCtrl.CodeReferences)
	flagAdmin := auth.Group("/feature-flags", middleware.AdminAuthMiddleware())
	{
		flagAdmin.POST("", flagCtrl.Create)
		flagAdmin.PUT("/:id", flagCtrl.Update)
		flagAdmin.DELETE("/:id", flagCtrl.Delete)
		flagAdmin.POST("/:id/enable", flagCtrl.Enable)
		flagAdmin.POST("/:id/disable", flagCtrl.Disable)
		flagAdmin.POST("/:id/rollout", flagCtrl.Rollout)
		flagAdmin.POST("/evaluate", flagCtrl.Evaluate)
		flagAdmin.POST("/evaluate-batch", flagCtrl.EvaluateBatch)
		flagAdmin.POST("/:id/code-references", flagCtrl.RegisterCodeReference)
	}
}

func setupABTestRoutes(auth *gin.RouterGroup) {
	abCtrl := opsctrl.NewABExperimentController()
	auth.GET("/ab-experiments", abCtrl.GetExperimentList)
	auth.GET("/ab-experiments/:id", abCtrl.GetExperiment)
	auth.GET("/ab-experiments/:id/results", abCtrl.GetExperimentResults)
	auth.GET("/ab-experiments/:id/conversion-events", abCtrl.GetConversionEvents)

	auth.GET("/ab-experiments/:id/stats", abCtrl.GetAdvancedStats)
	auth.GET("/ab-experiments/:id/diagnostics", abCtrl.GetExperimentDiagnostics)
	auth.GET("/ab-experiments/:id/cuped", abCtrl.GetExperimentCUPED)
	auth.POST("/ab-experiments/:id/sequential-test", abCtrl.PostSequentialTest)
	auth.POST("/ab-experiments/:id/bayesian-test", abCtrl.PostBayesianTest)
	auth.GET("/ab-experiments/:id/results-with-reach", abCtrl.GetResultsWithReach)
	auth.POST("/ab-experiments/reach-metrics", abCtrl.PostReachMetrics)
	admin := auth.Group("/ab-experiments", middleware.AdminAuthMiddleware())
	{
		admin.POST("", abCtrl.CreateExperiment)
		admin.PUT("/:id", abCtrl.UpdateExperiment)
		admin.DELETE("/:id", abCtrl.DeleteExperiment)
		admin.POST("/:id/start", abCtrl.StartExperiment)
		admin.POST("/:id/pause", abCtrl.PauseExperiment)
		admin.POST("/:id/stop", abCtrl.StopExperiment)
	}
}

func setupChurnRoutes(auth *gin.RouterGroup) {
	churnCtrl := opsctrl.NewChurnPredictionController()
	auth.GET("/churn/prediction", churnCtrl.GetChurnPrediction)
	auth.GET("/churn/predictions", churnCtrl.GetChurnPredictions)
	auth.GET("/churn/high-risk-users", churnCtrl.GetHighRiskUsers)
	auth.GET("/churn/warnings", churnCtrl.GetChurnWarnings)
	auth.GET("/churn/unhandled-warnings", churnCtrl.GetUnhandledWarnings)
	auth.POST("/churn/warnings/:id/handle", churnCtrl.MarkWarningHandled)
	auth.POST("/churn/warnings/intervene", churnCtrl.InterveneUser)
	auth.GET("/churn/model-config", churnCtrl.GetModelConfig)
	auth.GET("/churn/statistics", churnCtrl.GetChurnStatistics)
	auth.GET("/churn/risk-distribution", churnCtrl.GetRiskDistribution)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/churn/model-config", churnCtrl.SaveModelConfig)
}

func setupIntegrationRoutes(auth *gin.RouterGroup) {
	integrationCtrl := controller.NewIntegrationController()
	auth.GET("/integrations", integrationCtrl.GetAccountList)
	auth.GET("/integrations/:id", integrationCtrl.GetAccountByID)

	auth.GET("/integrations/templates", integrationCtrl.GetTemplates)
	auth.GET("/integrations/categories", integrationCtrl.GetCategories)
	auth.GET("/integration/sync-logs", integrationCtrl.GetSyncLogs)
	auth.GET("/integration/external-customers", integrationCtrl.GetExternalCustomers)
	auth.GET("/integration/external-products", integrationCtrl.GetExternalProducts)
	auth.GET("/integration/external-orders", integrationCtrl.GetExternalOrders)
	auth.GET("/integration/external-orders-by-customer", integrationCtrl.GetExternalOrdersByCustomer)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/integrations", integrationCtrl.CreateAccount)
		admin.PUT("/integrations/:id", integrationCtrl.UpdateAccount)
		admin.DELETE("/integrations/:id", integrationCtrl.DeleteAccount)
		admin.POST("/integrations/:id/test", integrationCtrl.TestIntegration)
		admin.POST("/integrations/:id/sync-customers", integrationCtrl.SyncCustomers)
		admin.POST("/integrations/:id/sync-products", integrationCtrl.SyncProducts)
	}
	// 旧契约（T-P2-02 起标记 deprecation，保留一个大版本）：挂在 auth 组里，
	// 语义是"调用方是我们的登录用户"。外部电商平台不该持有会话票，所以新对接走
	// setupOrderWebhookRoutes 那条公开 + HMAC 的路径。这里不删、也不改成验签 ——
	// 现网可能真有内部服务在用它，直接换语义会把能跑的调用打挂；改为每次命中
	// 回 deprecation 头 + 限速告警，日志即下线前的名单。
	auth.POST(orderWebhookLegacyPath,
		middleware.LegacyDeprecationNotice(orderWebhookSuccessorPrefix),
		integrationCtrl.ReceiveOrderWebhook)
}

const (
	// orderWebhookLegacyPath 旧路径（挂在 auth 组内，组前缀见 router.go 的 /api 组）。
	orderWebhookLegacyPath = "/integration/order-webhook/:" + middleware.OrderWebhookPlatformParam
	// orderWebhookPublicPath 新路径：公开 + HMAC 验签。与旧路径同域不同段，
	// 对接方只需改一段 URL 即可切换，两条路可同时在线（一个用会话票、一个用签名）。
	orderWebhookPublicPath = "/integration/webhook/order/:" + middleware.OrderWebhookPlatformParam
	// orderWebhookSuccessorPrefix 给旧路径调用方指路用（Link: rel=successor-version），
	// 因此必须是客户端能直接照抄的绝对路径 —— 它和上面那条注册路径的对应关系由
	// TestOrderWebhook_BothPathsRegistered 钉住（用例真的照着这个头去发一次签名回调）；
	// 指错路不会编译报错，只会让对接方对着 404 猜。
	orderWebhookSuccessorPrefix = middleware.OrderWebhookVerifiedPathPrefix
)

// setupOrderWebhookRoutes 注册对外电商平台的订单回调入口。
//
// 挂在**公开组**上：外部平台没有、也不应该有本系统的会话凭证。代价是这个端点在互联网
// 上裸奔，所以可信性全部押在 guard 的 HMAC 验签上 —— 无密钥 ⇒ 503（fail-closed），
// 这条路径**没有** "先公开、后验签" 的中间态，见中间件文件头的三条口径。
func setupOrderWebhookRoutes(public *gin.RouterGroup) {
	integrationCtrl := controller.NewIntegrationController()
	guard := middleware.NewOrderWebhookGuard(nil)
	public.POST(orderWebhookPublicPath, guard.Middleware(), integrationCtrl.ReceiveOrderWebhook)
}

func setupCommunityRoutes(auth *gin.RouterGroup) {
	communityCtrl := controller.NewCommunityController()
	auth.GET("/community/groups", communityCtrl.GetGroups)
	auth.GET("/community/groups/:id", communityCtrl.GetGroupByID)
	auth.GET("/community/members", communityCtrl.GetMembers)
	auth.GET("/community/members/:id", communityCtrl.GetMemberByID)
	auth.GET("/community/messages", communityCtrl.GetMessages)
	auth.GET("/community/stats", communityCtrl.GetStatistics)

	admin := auth.Group("/community", middleware.AdminAuthMiddleware())
	{
		admin.POST("/groups", communityCtrl.CreateGroup)
		admin.PUT("/groups/:id", communityCtrl.UpdateGroup)
		admin.DELETE("/groups/:id", communityCtrl.DeleteGroup)
		admin.POST("/members", communityCtrl.AddMember)
		admin.DELETE("/members/:id", communityCtrl.RemoveMember)
		admin.PUT("/members/:id", communityCtrl.UpdateMember)
		admin.POST("/import", communityCtrl.ImportData)
		admin.POST("/export", communityCtrl.ExportData)
	}
}

func setupEventRoutes(auth *gin.RouterGroup) {
	eventCtrl := controller.NewCustomerEventController()
	auth.POST("/events/track", eventCtrl.TrackEvent)
	auth.GET("/events/customer/:id", eventCtrl.GetEventHistory)

	auth.GET("/customer-events/list", eventCtrl.ListGlobal)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.DELETE("/events/customer/:id", eventCtrl.DeleteEvent)
	auth.GET("/events/stats", eventCtrl.GetEventStats)
	auth.POST("/events/pageview", eventCtrl.TrackPageView)
	auth.POST("/events/click", eventCtrl.TrackClick)
	auth.POST("/events/purchase", eventCtrl.TrackPurchase)
	auth.POST("/events/signup", eventCtrl.TrackSignup)
	auth.POST("/events/login", eventCtrl.TrackLogin)
	auth.POST("/events/add-to-cart", eventCtrl.TrackAddToCart)
}

func setupRateQuotaRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewRateQuotaController()
	auth.GET("/system/rate-quota", ctrl.GetRateQuota)
}

func setupPromptRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewPromptController()

	auth.GET("/prompts", ctrl.List)
	auth.GET("/prompts/:id", ctrl.GetByID)
	auth.GET("/prompts/ab-experiments", ctrl.GetABExperiments)
	auth.GET("/prompts/:id/versions", ctrl.GetVersions)

	admin := auth.Group("/prompts", middleware.AdminAuthMiddleware())
	admin.POST("", ctrl.Create)
	admin.PUT("/:id", ctrl.Update)
	admin.DELETE("/:id", ctrl.Delete)
	admin.POST("/:id/publish", ctrl.Publish)
}

func setupTypingPredictRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewTypingPredictController()

	auth.POST("/chat/typing-predict/predict", ctrl.Predict)

	auth.GET("/chat/typing-predict/sse", ctrl.SSEStream)
}
