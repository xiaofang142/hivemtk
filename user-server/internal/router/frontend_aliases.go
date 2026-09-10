package router

import (
	"fmt"

	"hivemtk-user/internal/app"
	contentctrl "hivemtk-user/internal/content/controller"
	contentService "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	opsctrl "hivemtk-user/internal/ops/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupFrontendAliases(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
	aiAgentSvc := service.NewAIAgentServiceWithDB(gormDB)
	doReg := func(method, path string, handlers ...gin.HandlerFunc) {
		defer func() {
			if r := recover(); r != nil {
				_ = r
			}
		}()
		switch method {
		case "GET":
			auth.GET(path, handlers...)
		case "POST":
			auth.POST(path, handlers...)
		case "PUT":
			auth.PUT(path, handlers...)
		case "DELETE":
			auth.DELETE(path, handlers...)
		case "PATCH":
			auth.PATCH(path, handlers...)
		}
	}

	doRegAdmin := func(method, path string, handlers ...gin.HandlerFunc) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[WARN] doRegAdmin panic: %s %s -> %v\n", method, path, r)
			}
		}()
		adminHandlers := append([]gin.HandlerFunc{middleware.AdminAuthMiddleware()}, handlers...)
		switch method {
		case "GET":
			auth.GET(path, adminHandlers...)
		case "POST":
			auth.POST(path, adminHandlers...)
		case "PUT":
			auth.PUT(path, adminHandlers...)
		case "DELETE":
			auth.DELETE(path, adminHandlers...)
		case "PATCH":
			auth.PATCH(path, adminHandlers...)
		}
		fmt.Printf("[OK] doRegAdmin registered: %s %s (handlers=%d)\n", method, path, len(adminHandlers))
	}

	notifCtrl := controller.NewNotificationController(service.NewNotificationService(gormDB))
	doReg("GET", "/notifications/list", notifCtrl.List)

	clueCtrl := controller.NewClueController()
	doReg("GET", "/clues", clueCtrl.GetClueList)
	doReg("GET", "/clues/list", clueCtrl.GetClueList)
	doReg("GET", "/clues/statistics", clueCtrl.GetClueStatistics)
	doReg("GET", "/clues/type", clueCtrl.GetClueTypes)
	doReg("GET", "/clues/import", clueCtrl.GetClueTypes)
	doRegAdmin("POST", "/clues/import", clueCtrl.ImportClues)
	doReg("GET", "/clue-statistics/overview", clueCtrl.GetClueStatistics)
	doRegAdmin("DELETE", "/clues/:id", clueCtrl.DeleteClue)

	customer360Ctrl := controller.NewCustomer360Controller()
	doReg("GET", "/customer-360/list", customer360Ctrl.GetCustomerList)
	doReg("GET", "/customer-360/stats", customer360Ctrl.GetCustomerStats)
	doReg("GET", "/customer-360/tags", customer360Ctrl.GetCustomerTags)
	doReg("GET", "/customer-360", customer360Ctrl.GetCustomer360)
	doReg("PUT", "/customer-360/tags", customer360Ctrl.UpdateCustomerTags)
	doReg("GET", "/customer/list", customer360Ctrl.GetCustomerList)
	doReg("GET", "/customer/360/:id", customer360Ctrl.GetCustomer360ByID)
	doReg("GET", "/customer/:id", customer360Ctrl.GetCustomerDetail)
	doReg("PUT", "/customer/:id", customer360Ctrl.UpdateCustomer)

	doReg("GET", "/customer-tags", customer360Ctrl.GetCustomerTags)
	doReg("PUT", "/customer-tags", customer360Ctrl.UpdateCustomerTags)
	doReg("GET", "/tag-segments", customer360Ctrl.GetTagStats)
	doReg("GET", "/tag-segmentation/list", customer360Ctrl.GetTagStats)

	customerEventCtrl := controller.NewCustomerEventController()
	doReg("GET", "/customer-events", customerEventCtrl.GetEventStats)
	doReg("GET", "/customer-events/list", customerEventCtrl.GetEventStats)
	doReg("GET", "/customer-events/stats", customerEventCtrl.GetEventStats)
	doReg("GET", "/customer-events/customer/:customer_id", customerEventCtrl.GetEventHistory)
	doReg("POST", "/customer-events/track", customerEventCtrl.TrackEvent)
	doReg("POST", "/customer-events/pageview", customerEventCtrl.TrackPageView)
	doReg("POST", "/customer-events/click", customerEventCtrl.TrackClick)
	doReg("POST", "/customer-events/purchase", customerEventCtrl.TrackPurchase)
	doReg("POST", "/customer-events/signup", customerEventCtrl.TrackSignup)
	doReg("POST", "/customer-events/login", customerEventCtrl.TrackLogin)
	doReg("POST", "/customer-events/add-to-cart", customerEventCtrl.TrackAddToCart)
	doRegAdmin("DELETE", "/customer-events/customer/:customer_id", customerEventCtrl.DeleteEvent)

	userSegmentCtrl := controller.NewUserSegmentController()
	doReg("GET", "/user-segments/list", userSegmentCtrl.ListRFMRules)
	doReg("GET", "/user-segments/rfm/list", userSegmentCtrl.GetRFMList)
	doReg("GET", "/user-segments/layers", userSegmentCtrl.GetLayerDescription)

	unifiedMsgCtrl := controller.NewUnifiedMessageController()
	doReg("GET", "/unified-messages", unifiedMsgCtrl.GetMessages)
	doReg("GET", "/unified-messages/list", unifiedMsgCtrl.GetMessages)
	doReg("GET", "/unified-messages/:id", unifiedMsgCtrl.GetMessageByID)

	oneIDCtrl := controller.NewCustomerOneIDController()
	doReg("GET", "/oneid/identities", oneIDCtrl.ListOneID)
	doReg("GET", "/oneid/conflicts", oneIDCtrl.ListConflicts)
	doRegAdmin("POST", "/oneid/merge", oneIDCtrl.MergeIdentity)
	doRegAdmin("POST", "/oneid/resolve", oneIDCtrl.ResolveIdentity)
	doRegAdmin("POST", "/oneid/conflicts/:id/resolve", oneIDCtrl.ResolveConflict)

	doRegAdmin("GET", "/oneid/merge-rules", oneIDCtrl.GetMergeRules)
	doRegAdmin("POST", "/oneid/merge-rules", oneIDCtrl.SaveMergeRules)
	doRegAdmin("DELETE", "/oneid/merge-rules/:id", oneIDCtrl.DeleteMergeRule)

	doRegAdmin("POST", "/oneid/merge-rules/preview", oneIDCtrl.PreviewMergeRules)

	customerSessionCtrl := controller.NewCustomerSessionController()
	doReg("GET", "/customer-sessions", customerSessionCtrl.GetSessions)
	doReg("GET", "/customer-sessions/list", customerSessionCtrl.GetSessions)
	doReg("GET", "/customer-sessions/pending", customerSessionCtrl.GetPendingSessions)
	doReg("GET", "/customer-sessions/:id", customerSessionCtrl.GetSessionByID)
	doReg("GET", "/customer-sessions/:id/messages", customerSessionCtrl.GetMessages)
	doReg("POST", "/customer-sessions/:id/messages", customerSessionCtrl.SendMessage)
	doReg("POST", "/customer-sessions/:id/transfer", customerSessionCtrl.TransferSession)
	doReg("POST", "/customer-sessions/:id/tags", customerSessionCtrl.TagSession)
	doReg("POST", "/customer-sessions/:id/rate", customerSessionCtrl.RateSession)
	doReg("PUT", "/customer-sessions/:id/status", customerSessionCtrl.UpdateSessionStatus)

	intentRec := service.GetIntentRecognizer()
	if intentRec == nil {
		intentRec = service.NewIntentRecognizer(gormDB, app.GetGlobalDispatcher(), nil)
	}
	intentCtrl := controller.NewIntentController(intentRec)
	doReg("GET", "/intent-records", intentCtrl.RecentIntents)
	doReg("GET", "/intent-records/list", intentCtrl.RecentIntents)
	doReg("GET", "/intent-records/stats", intentCtrl.Stats)
	doReg("GET", "/intent-records/dict", intentCtrl.Intents)
	doReg("POST", "/intent-records/recognize", intentCtrl.Recognize)
	doReg("POST", "/intent-records/recognize/batch", intentCtrl.BatchRecognize)
	doReg("GET", "/intent-records/config", intentCtrl.GetConfig)
	doRegAdmin("PUT", "/intent-records/config", intentCtrl.UpdateConfig)
	doReg("GET", "/intent-records/keywords-override", intentCtrl.GetKeywordOverride)
	doRegAdmin("PUT", "/intent-records/keywords-override", intentCtrl.UpdateKeywordOverride)

	memCtrl := controller.NewDialogueMemoryController(service.NewDialogueMemoryService(gormDB, nil))
	doReg("GET", "/dialogue-memories", memCtrl.Stats)
	doReg("GET", "/dialogue-memories/list", memCtrl.Stats)
	doReg("GET", "/dialogue-memories/stats", memCtrl.Stats)
	doReg("GET", "/dialogue-memories/:customer_id", memCtrl.ShortTerm)
	doReg("POST", "/dialogue-memories/messages", memCtrl.AppendMessage)
	doReg("GET", "/dialogue-memories/short", memCtrl.ShortTerm)
	doReg("GET", "/dialogue-memories/long", memCtrl.LongTerm)
	doReg("POST", "/dialogue-memories/facts", memCtrl.UpdateKeyFacts)
	doReg("POST", "/dialogue-memories/objections", memCtrl.RecordObjection)
	doReg("POST", "/dialogue-memories/purchase-intent", memCtrl.UpdatePurchaseIntent)
	doReg("POST", "/dialogue-memories/intent-trail", memCtrl.RecordIntent)
	doReg("POST", "/dialogue-memories/sop-history", memCtrl.RecordSOP)
	doReg("GET", "/dialogue-memories/context", memCtrl.BuildContext)

	routingSvc := service.NewLLMRoutingService(app.GetGlobalDispatcher())
	llmCtrl := controller.NewLLMRoutingController(routingSvc)
	doReg("GET", "/llm-routing/rules", llmCtrl.ListStrategies)
	doReg("GET", "/llm-routing/models", llmCtrl.ListModels)
	doRegAdmin("PUT", "/llm-routing/strategies", llmCtrl.UpdateStrategies)

	doReg("GET", "/llm/models", llmCtrl.ListModels)
	doReg("GET", "/llm/models/:id", llmCtrl.ListModels)
	doRegAdmin("POST", "/llm/models", llmCtrl.CreateModel)
	doRegAdmin("PUT", "/llm/models/:id", llmCtrl.UpdateModel)
	doRegAdmin("DELETE", "/llm/models/:id", llmCtrl.DeleteModel)
	doRegAdmin("PUT", "/llm/models/:id/status", llmCtrl.UpdateModel)
	doRegAdmin("POST", "/llm/models/:id/test", llmCtrl.TestModel)
	doReg("GET", "/llm/scene-routing", llmCtrl.ListStrategies)
	doRegAdmin("PUT", "/llm/scene-routing", llmCtrl.UpdateStrategies)
	doReg("GET", "/llm/fallback", llmCtrl.Stats)
	doRegAdmin("PUT", "/llm/fallback", llmCtrl.UpdateStrategies)
	doReg("GET", "/llm/cost-stats", llmCtrl.Usage)

	reachCtrl := controller.NewReachPipelineController(service.NewReachPipelineService(gormDB))
	doReg("GET", "/reach-pipelines", reachCtrl.ListPipelines)
	doReg("GET", "/reach-pipelines/list", reachCtrl.ListPipelines)
	doReg("GET", "/reach-pipelines/:id", reachCtrl.GetPipeline)

	doRegAdmin("POST", "/reach-pipelines", reachCtrl.CreatePipeline)
	doRegAdmin("PUT", "/reach-pipelines/:id", reachCtrl.UpdatePipeline)
	doRegAdmin("DELETE", "/reach-pipelines/:id", reachCtrl.DeletePipeline)

	marketingFlowCtrl := contentctrl.NewMarketingFlowController()
	doReg("GET", "/marketing-flows", marketingFlowCtrl.GetFlowList)
	doReg("GET", "/marketing-flows/list", marketingFlowCtrl.GetFlowList)
	doReg("GET", "/marketing-flows/:id", marketingFlowCtrl.GetFlowByID)

	doReg("GET", "/marketing-flows/executions", marketingFlowCtrl.GetExecutionList)
	doReg("GET", "/marketing-flows/executions/stats", marketingFlowCtrl.GetExecutionStats)

	batchOpCtrl := contentctrl.NewBatchOperationController()
	doReg("GET", "/batch-operations", batchOpCtrl.GetTools)
	doReg("GET", "/batch-operations/list", batchOpCtrl.GetHistories)

	sopCtrl := controller.NewSOPController(service.NewSOPService(gormDB, nil))
	doReg("GET", "/sop-agents", sopCtrl.List)
	doReg("GET", "/sop-agents/list", sopCtrl.List)
	doReg("GET", "/sop-agents/stats", sopCtrl.Stats)
	doReg("GET", "/sop-agents/:id", sopCtrl.Get)
	doRegAdmin("POST", "/sop-agents", sopCtrl.Create)
	doRegAdmin("PUT", "/sop-agents/:id", sopCtrl.Update)
	doRegAdmin("DELETE", "/sop-agents/:id", sopCtrl.Delete)
	doRegAdmin("POST", "/sop-agents/:id/activate", sopCtrl.Activate)
	doRegAdmin("POST", "/sop-agents/:id/deactivate", sopCtrl.Deactivate)
	doRegAdmin("POST", "/sop-agents/:id/execute", sopCtrl.Execute)
	doRegAdmin("POST", "/sop-agents/:id/step", sopCtrl.Step)
	doRegAdmin("POST", "/sop-agents/:id/pause", sopCtrl.Pause)

	scriptCtrl := contentctrl.NewScriptTemplateController()
	doReg("GET", "/script-templates", scriptCtrl.GetTemplateList)
	doReg("GET", "/script-templates/list", scriptCtrl.GetTemplateList)
	doReg("GET", "/script-templates/categories", scriptCtrl.GetCategories)
	doReg("GET", "/script-templates/:id", scriptCtrl.GetTemplateByID)
	doRegAdmin("POST", "/script-templates", scriptCtrl.CreateTemplate)
	doRegAdmin("PUT", "/script-templates/:id", scriptCtrl.UpdateTemplate)
	doRegAdmin("DELETE", "/script-templates/:id", scriptCtrl.DeleteTemplate)
	doReg("GET", "/script-templates/search", scriptCtrl.SearchTemplates)
	doReg("GET", "/script-templates/public", scriptCtrl.GetPublicTemplates)
	doReg("POST", "/script-templates/recommend", scriptCtrl.RecommendScript)

	agentStatusCtrl := controller.NewAgentStatusController(aiAgentSvc)
	doReg("GET", "/agent-statuses", agentStatusCtrl.GetOnlineAgents)
	doReg("GET", "/agent-statuses/online", agentStatusCtrl.GetOnlineAgents)
	doReg("GET", "/agent-statuses/list", agentStatusCtrl.ListAllAgents)
	doReg("GET", "/agent-statuses/:id", agentStatusCtrl.GetAgentStatus)
	doReg("PUT", "/agent-statuses/:id/status", agentStatusCtrl.UpdateAgentStatus)
	doReg("POST", "/agent-statuses/:id/online", agentStatusCtrl.GoOnline)
	doReg("POST", "/agent-statuses/:id/offline", agentStatusCtrl.GoOffline)
	doReg("GET", "/agent-statuses/available", agentStatusCtrl.GetOnlineAgents)
	doReg("GET", "/agent-statuses/:id/sessions", agentStatusCtrl.GetAgentSessions)

	quickReplyCtrl := controller.NewQuickReplyController()
	doReg("GET", "/quick-replies", quickReplyCtrl.GetReplies)
	doReg("GET", "/quick-replies/list", quickReplyCtrl.GetReplies)
	doReg("GET", "/quick-replies/categories", quickReplyCtrl.GetReplyCategories)

	aiSuggestionCtrl := controller.NewAISuggestionController()
	doReg("GET", "/ai-suggestions", aiSuggestionCtrl.GetSuggestions)

	objCtrl := controller.NewObjectionHandlerController()
	doReg("GET", "/objection-templates", objCtrl.ListCategories)
	doReg("GET", "/objection-templates/list", objCtrl.ListCategories)
	doReg("POST", "/objection-templates/handle", objCtrl.Handle)
	doReg("POST", "/objection-templates/classify", objCtrl.Classify)
	doReg("POST", "/objection-templates/usage", objCtrl.RecordUsage)

	personaCtrl := controller.NewSalesPersonaController()
	doReg("GET", "/sales-champions", personaCtrl.ListStaffs)
	doReg("GET", "/sales-champions/list", personaCtrl.ListStaffs)
	doReg("GET", "/sales-champions/:id", personaCtrl.GetReport)

	emailListCtrl := controller.NewEmailListController()
	doReg("GET", "/email/lists", emailListCtrl.GetEmailListList)
	doReg("GET", "/email/lists/list", emailListCtrl.GetEmailListList)
	doReg("GET", "/email/lists/:id", emailListCtrl.GetEmailListDetail)
	doRegAdmin("POST", "/email/lists", emailListCtrl.CreateEmailList)
	doRegAdmin("PUT", "/email/lists/:id", emailListCtrl.UpdateEmailList)
	doRegAdmin("DELETE", "/email/lists/:id", emailListCtrl.DeleteEmailList)
	doRegAdmin("GET", "/email/lists/:id/trace", emailListCtrl.TraceEmail)
	doReg("GET", "/email/lists/:id/tracking", emailListCtrl.GetTracking)

	emailSmtpCtrl := controller.NewEmailSmtpController()
	doReg("GET", "/email/smtps", emailSmtpCtrl.GetEmailSmtpList)
	doReg("GET", "/email/smtps/list", emailSmtpCtrl.GetEmailSmtpList)
	doReg("GET", "/email/smtps/:id", emailSmtpCtrl.GetEmailSmtp)

	doRegAdmin("POST", "/email/smtps", emailSmtpCtrl.CreateEmailSmtp)
	doRegAdmin("PUT", "/email/smtps/:id", emailSmtpCtrl.UpdateEmailSmtp)
	doRegAdmin("DELETE", "/email/smtps/:id", emailSmtpCtrl.DeleteEmailSmtp)

	smsCtrl := controller.NewSmsController(service.NewSmsService(repository.NewSmsRepository()))
	doReg("GET", "/sms/records", smsCtrl.GetSmsList)
	doReg("GET", "/sms/records/list", smsCtrl.GetSmsList)

	doRegAdmin("POST", "/sms/records", smsCtrl.SendSms)

	doReg("GET", "/sms/drafts", smsCtrl.GetDraftList)
	doReg("GET", "/sms/drafts/list", smsCtrl.GetDraftList)
	doRegAdmin("POST", "/sms/drafts", smsCtrl.CreateDraft)
	doRegAdmin("PUT", "/sms/drafts/:id", smsCtrl.UpdateDraft)
	doRegAdmin("DELETE", "/sms/drafts/:id", smsCtrl.DeleteDraft)

	doReg("GET", "/sms/jobs", smsCtrl.GetJobList)
	doReg("GET", "/sms/jobs/list", smsCtrl.GetJobList)
	doRegAdmin("POST", "/sms/jobs", smsCtrl.CreateJob)
	doRegAdmin("POST", "/sms/jobs/:id/pause", smsCtrl.PauseJob)
	doRegAdmin("POST", "/sms/jobs/:id/resume", smsCtrl.ResumeJob)
	doRegAdmin("DELETE", "/sms/jobs/:id", smsCtrl.DeleteJob)

	doReg("GET", "/sms/configs", smsCtrl.GetConfig)
	doReg("GET", "/sms/configs/list", smsCtrl.GetConfig)
	doRegAdmin("POST", "/sms/configs", smsCtrl.SaveConfig)

	{
		douyinCtrl := controller.NewDouyinCardController(service.NewDouyinCardService(gormDB))
		doReg("GET", "/douyin-cards", douyinCtrl.GetList)
		doReg("GET", "/douyin-cards/list", douyinCtrl.GetList)
		doReg("GET", "/douyin-cards/:id", douyinCtrl.GetByID)
		doRegAdmin("POST", "/douyin-cards", douyinCtrl.Create)
		doRegAdmin("PUT", "/douyin-cards/:id", douyinCtrl.Update)
		doRegAdmin("DELETE", "/douyin-cards/:id", douyinCtrl.Delete)
	}
	{
		kuaishouCtrl := controller.NewKuaishouCardController(service.NewKuaishouCardService(gormDB))
		doReg("GET", "/kuaishou-cards", kuaishouCtrl.GetList)
		doReg("GET", "/kuaishou-cards/list", kuaishouCtrl.GetList)
		doReg("GET", "/kuaishou-cards/:id", kuaishouCtrl.GetByID)
		doRegAdmin("POST", "/kuaishou-cards", kuaishouCtrl.Create)
		doRegAdmin("PUT", "/kuaishou-cards/:id", kuaishouCtrl.Update)
		doRegAdmin("DELETE", "/kuaishou-cards/:id", kuaishouCtrl.Delete)
	}
	{
		xhsCtrl := controller.NewXiaohongshuCardController(service.NewXiaohongshuCardService(gormDB))
		doReg("GET", "/xiaohongshu-cards", xhsCtrl.GetList)
		doReg("GET", "/xiaohongshu-cards/list", xhsCtrl.GetList)
		doReg("GET", "/xiaohongshu-cards/:id", xhsCtrl.GetByID)
		doRegAdmin("POST", "/xiaohongshu-cards", xhsCtrl.Create)
		doRegAdmin("PUT", "/xiaohongshu-cards/:id", xhsCtrl.Update)
		doRegAdmin("DELETE", "/xiaohongshu-cards/:id", xhsCtrl.Delete)
	}
	{
		xianyuCtrl := controller.NewXianyuCardController(service.NewXianyuCardService(gormDB), service.NewXianyuCardStatsService(gormDB))
		doReg("GET", "/xianyu-cards", xianyuCtrl.GetList)
		doReg("GET", "/xianyu-cards/list", xianyuCtrl.GetList)
		doReg("GET", "/xianyu-cards/:id", xianyuCtrl.GetByID)
		doRegAdmin("POST", "/xianyu-cards", xianyuCtrl.Create)
		doRegAdmin("PUT", "/xianyu-cards/:id", xianyuCtrl.Update)
		doRegAdmin("DELETE", "/xianyu-cards/:id", xianyuCtrl.Delete)
	}

	tiktokCtrl := controller.NewTikTokCardController(
		service.NewTikTokCardServiceWithDB(gormDB),
	)
	doReg("GET", "/tiktok-cards", tiktokCtrl.List)
	doReg("GET", "/tiktok-cards/list", tiktokCtrl.List)
	doReg("GET", "/tiktok-cards/:id", tiktokCtrl.Get)
	doRegAdmin("POST", "/tiktok-cards", tiktokCtrl.Create)
	doRegAdmin("PUT", "/tiktok-cards/:id", tiktokCtrl.Update)
	doRegAdmin("DELETE", "/tiktok-cards/:id", tiktokCtrl.Delete)

	feishuCtrl := controller.NewFeishuAccountController(service.NewFeishuService(gormDB), service.NewFeishuIntegrationService(gormDB))
	doReg("GET", "/feishu/accounts", feishuCtrl.List)
	doReg("GET", "/feishu/accounts/list", feishuCtrl.List)
	doReg("GET", "/feishu/accounts/:id", feishuCtrl.Get)

	tgCtrl := controller.NewTelegramAccountController(service.NewTelegramService(gormDB))
	doReg("GET", "/telegram/accounts", tgCtrl.List)
	doReg("GET", "/telegram/accounts/list", tgCtrl.List)
	doReg("GET", "/telegram/accounts/:id", tgCtrl.Get)

	shortLinkCtrl := controller.NewShortLinkController(service.NewShortLinkService(gormDB))
	doReg("GET", "/short-links", shortLinkCtrl.GetList)
	doReg("GET", "/short-links/list", shortLinkCtrl.GetList)
	doReg("GET", "/short-links/:id", shortLinkCtrl.GetByID)

	doRegAdmin("POST", "/short-links", shortLinkCtrl.Create)
	doRegAdmin("PUT", "/short-links/:id", shortLinkCtrl.Update)
	doRegAdmin("DELETE", "/short-links/:id", shortLinkCtrl.Delete)

	liveCodeCtrl := controller.NewLiveCodeController(service.NewLiveCodeService(gormDB))
	doReg("GET", "/live-codes", liveCodeCtrl.GetList)
	doReg("GET", "/live-codes/list", liveCodeCtrl.GetList)
	doReg("GET", "/live-codes/:id", liveCodeCtrl.GetByID)
	doRegAdmin("POST", "/live-codes", liveCodeCtrl.Create)
	doRegAdmin("PUT", "/live-codes/:id", liveCodeCtrl.Update)
	doRegAdmin("DELETE", "/live-codes/:id", liveCodeCtrl.Delete)

	ragProductCtrl := controller.NewRagProductController()
	doReg("GET", "/rag-product-configs", ragProductCtrl.List)
	doReg("GET", "/rag-product-configs/list", ragProductCtrl.List)
	doReg("GET", "/rag-product-configs/stats", ragProductCtrl.Stats)
	doReg("GET", "/rag-product-configs/:id", ragProductCtrl.Get)
	doRegAdmin("POST", "/rag-product-configs", ragProductCtrl.Create)
	doRegAdmin("PUT", "/rag-product-configs/:id", ragProductCtrl.Update)
	doRegAdmin("DELETE", "/rag-product-configs/:id", ragProductCtrl.Delete)

	aiContentCtrl := contentctrl.NewAIContentController()
	doReg("GET", "/ai-content/list", aiContentCtrl.GetGenerationHistory)
	doReg("GET", "/ai-content/history", aiContentCtrl.GetGenerationHistory)
	doReg("GET", "/ai-content/templates", aiContentCtrl.GetTemplates)
	doReg("GET", "/ai-content/templates/:id", aiContentCtrl.GetTemplateByID)
	doRegAdmin("POST", "/ai-content/generate", aiContentCtrl.GenerateContent)
	doRegAdmin("POST", "/ai-content", aiContentCtrl.CreateHistory)
	doReg("GET", "/ai-content/:id", aiContentCtrl.GetRecordByID)
	doRegAdmin("POST", "/ai-content/:id/save", aiContentCtrl.SaveRecord)
	doReg("POST", "/ai-content/:id/favorite", aiContentCtrl.FavoriteRecord)
	doReg("POST", "/ai-content/:id/rate", aiContentCtrl.RateRecord)
	doRegAdmin("DELETE", "/ai-content/:id", aiContentCtrl.DeleteRecord)

	templateCtrl := contentctrl.NewTemplateMarketController()
	doReg("GET", "/market-templates", templateCtrl.GetTemplateList)
	doReg("GET", "/market-templates/list", templateCtrl.GetTemplateList)
	doReg("GET", "/market-templates/official", templateCtrl.GetOfficialTemplates)
	doReg("GET", "/market-templates/search", templateCtrl.SearchTemplates)
	doReg("GET", "/market-templates/my-downloads", templateCtrl.GetMyDownloads)
	doReg("GET", "/market-templates/:id", templateCtrl.GetTemplateByID)
	doReg("POST", "/market-templates", templateCtrl.CreateTemplate)
	doReg("POST", "/market-templates/:id/download", templateCtrl.DownloadTemplate)
	doReg("POST", "/market-templates/:id/rate", templateCtrl.RateTemplate)

	dashCtrl := opsctrl.NewDashboardScreenController()
	doReg("GET", "/dashboard-screens", dashCtrl.GetScreenList)
	doReg("GET", "/dashboard-screens/list", dashCtrl.GetScreenList)
	doReg("GET", "/dashboard-screens/:id", dashCtrl.GetScreenByID)
	doRegAdmin("POST", "/dashboard-screens", dashCtrl.CreateScreen)
	doRegAdmin("PUT", "/dashboard-screens/:id", dashCtrl.UpdateScreen)
	doRegAdmin("DELETE", "/dashboard-screens/:id", dashCtrl.DeleteScreen)
	doReg("GET", "/dashboard-screens/:id/data", dashCtrl.GetDashboardData)
	doReg("GET", "/dashboard-screens/:id/activities", dashCtrl.GetRealtimeActivities)
	doReg("GET", "/dashboards", dashCtrl.GetScreenList)
	doReg("GET", "/dashboards/list", dashCtrl.GetScreenList)
	doReg("GET", "/dashboards/:id", dashCtrl.GetScreenByID)
	doReg("GET", "/dashboards/:id/data", dashCtrl.GetDashboardData)
	doReg("GET", "/dashboards/data", dashCtrl.GetDashboardData)
	doReg("GET", "/dashboards/activities", dashCtrl.GetRealtimeActivities)
	doReg("GET", "/dashboards/public/:code", dashCtrl.PublicViewScreen)
	doReg("GET", "/dashboard-screen/list", dashCtrl.GetScreenList)
	doReg("GET", "/dashboard-screen", dashCtrl.GetScreenList)

	funnelCtrl := opsctrl.NewConversionFunnelController()
	doReg("GET", "/conversion-funnels", funnelCtrl.GetFunnel)
	doReg("GET", "/conversion-funnels/list", funnelCtrl.GetFunnel)
	doReg("GET", "/conversion-funnels/stage", funnelCtrl.GetStageDetails)
	doReg("GET", "/analytics/funnel", funnelCtrl.GetFunnel)
	doReg("GET", "/analytics/funnel/stage", funnelCtrl.GetStageDetails)
	doReg("GET", "/conversion-funnel", funnelCtrl.GetFunnel)
	doReg("GET", "/conversion-funnel/list", funnelCtrl.GetFunnel)
	doReg("GET", "/conversion-funnel/stage", funnelCtrl.GetStageDetails)

	aiProdCtrl := opsctrl.NewAIProductivityController()
	doReg("GET", "/ai-productivity/overview", aiProdCtrl.GetReport)
	doReg("GET", "/ai-productivity/trend", aiProdCtrl.GetDailyTrend)

	journeyCtrl := controller.NewCustomerJourneyController()
	doReg("GET", "/customer-journey/dashboard", journeyCtrl.GetOverview)
	doReg("GET", "/customer-journey/overview", journeyCtrl.GetOverview)
	doReg("GET", "/customer-journey/stages", journeyCtrl.ListStages)
	doReg("GET", "/customer-journey/by-stage", journeyCtrl.ListByStage)
	doReg("POST", "/customer-journey/touch", journeyCtrl.TouchCustomer)
	doReg("POST", "/customer-journey/transition", journeyCtrl.TransitionStage)

	sysCfgCtrl := controller.NewSystemConfigController()
	doReg("GET", "/system/configs", sysCfgCtrl.GetConfig)
	doReg("GET", "/system/configs/list", sysCfgCtrl.GetConfig)
	doReg("GET", "/system/configs/:key", sysCfgCtrl.GetConfig)
	doRegAdmin("PUT", "/system/configs/:key", sysCfgCtrl.SaveConfig)

	obsCtrl := controller.NewObsConfigController()
	doReg("GET", "/obs-configs", obsCtrl.GetConfigList)
	doReg("GET", "/obs-configs/list", obsCtrl.GetConfigList)
	doReg("GET", "/obs-configs/default", obsCtrl.GetDefaultConfig)
	doReg("GET", "/obs-configs/:id", obsCtrl.GetConfig)

	doRegAdmin("POST", "/obs-configs", obsCtrl.CreateConfig)
	doRegAdmin("PUT", "/obs-configs/:id", obsCtrl.UpdateConfig)
	doRegAdmin("DELETE", "/obs-configs/:id", obsCtrl.DeleteConfig)
	doRegAdmin("POST", "/obs-configs/:id/test", obsCtrl.TestConnection)
	doRegAdmin("POST", "/obs-configs/:id/default", obsCtrl.SetDefault)

	materialCtrl := contentctrl.NewMaterialController(contentService.NewMaterialService())
	doReg("GET", "/material-library/list", materialCtrl.GetMaterialList)
	doReg("GET", "/material-library", materialCtrl.GetMaterialList)
	doReg("GET", "/material-library/categories", materialCtrl.GetMaterialCategories)
	doReg("GET", "/material-library/stats", materialCtrl.GetMaterialStats)
	doReg("POST", "/material-library", materialCtrl.UploadMaterial)
	doRegAdmin("DELETE", "/material-library/:id", materialCtrl.DeleteMaterial)

	sysMonCtrl := controller.NewSystemMonitorController()
	doReg("GET", "/system-monitor/metrics", sysMonCtrl.GetMetrics)
	doReg("GET", "/system-monitor/health", sysMonCtrl.GetHealth)

	csatCtrl := controller.NewCSATController()
	doReg("GET", "/csat/stats", csatCtrl.Stats)
	doReg("GET", "/csat/template", csatCtrl.GetTemplate)

	doRegAdmin("PUT", "/csat/template", csatCtrl.SaveTemplate)
	doReg("GET", "/csat/trend", csatCtrl.Trend)
	doReg("GET", "/csat/negative", csatCtrl.Negative)
	doReg("POST", "/customer-sessions/:id/csat", csatCtrl.Submit)
	doReg("POST", "/customer-sessions/:id/csat/trigger", csatCtrl.Trigger)

	csPlusCtrl := controller.NewCustomerServicePlusController()
	doReg("GET", "/user-segments", csPlusCtrl.ListSegments)

	doReg("GET", "/office-hours", csPlusCtrl.GetOfficeHours)
	doReg("PUT", "/office-hours", csPlusCtrl.SaveOfficeHours)
	doReg("PUT", "/customer-sessions/:id/priority", csPlusCtrl.SetSessionPriority)
	doReg("POST", "/customer-sessions/:id/snooze", csPlusCtrl.SnoozeSession)
	doReg("DELETE", "/customer-sessions/:id/snooze", csPlusCtrl.UnsnoozeSession)

	macroCtrl := controller.NewMacroController()
	doRegAdmin("GET", "/macros", macroCtrl.List)
	doRegAdmin("POST", "/macros", macroCtrl.Create)
	doRegAdmin("DELETE", "/macros/:id", macroCtrl.Delete)
	doRegAdmin("POST", "/macros/:id/apply", macroCtrl.Apply)

	growthCtrl := controller.NewGrowthController()
	doRegAdmin("GET", "/webhook-subscriptions", growthCtrl.ListWebhookSubs)
	doRegAdmin("POST", "/webhook-subscriptions", growthCtrl.CreateWebhookSub)
	doRegAdmin("DELETE", "/webhook-subscriptions/:id", growthCtrl.DeleteWebhookSub)
	doReg("PUT", "/customers/:id/custom-attributes", growthCtrl.SetCustomAttributes)
	doRegAdmin("POST", "/saved-views", growthCtrl.CreateSavedView)
	doReg("GET", "/saved-views", growthCtrl.ListSavedViews)
	doRegAdmin("DELETE", "/saved-views/:id", growthCtrl.DeleteSavedView)
	doRegAdmin("POST", "/report-subscriptions", growthCtrl.CreateReportSub)
	doReg("GET", "/report-subscriptions", growthCtrl.ListReportSubs)
	doRegAdmin("DELETE", "/report-subscriptions/:id", growthCtrl.DeleteReportSub)
	doRegAdmin("POST", "/report-subscriptions/send-now", growthCtrl.SendReportsNow)
	doReg("GET", "/customer-sessions/:id/transcript", growthCtrl.Transcript)
	doReg("GET", "/analytics/ai-performance", growthCtrl.AIPerformance)

	dncCtrl := controller.NewDNCController()
	doRegAdmin("GET", "/dnc", dncCtrl.List)
	doRegAdmin("POST", "/dnc", dncCtrl.Block)
	doRegAdmin("POST", "/dnc/block-phone", dncCtrl.BlockByPhone)
	doRegAdmin("DELETE", "/dnc/:one_id", dncCtrl.Unblock)
	doRegAdmin("GET", "/dnc/:one_id/blocked", dncCtrl.IsBlocked)

	sessionAIRepo := repository.NewSessionAIRepo(gormDB)
	sessionAISvc := service.NewSessionAIService(sessionAIRepo)
	sessionAICtrl := controller.NewSessionAIController(sessionAISvc)
	doReg("POST", "/customer-sessions/:id/ai-summary", sessionAICtrl.Generate)

	ruleCtrl := controller.NewRuleEngineController()
	doRegAdmin("GET", "/automation-rules", ruleCtrl.List)
	doRegAdmin("POST", "/automation-rules", ruleCtrl.Create)
	doRegAdmin("DELETE", "/automation-rules/:id", ruleCtrl.Delete)
	doRegAdmin("POST", "/automation-rules/:id/toggle", ruleCtrl.Toggle)
	doRegAdmin("POST", "/automation-rules/fire", ruleCtrl.Fire)
	doReg("GET", "/customer-sessions/:id/ai-summary", sessionAICtrl.Get)
	doReg("POST", "/user-segments", csPlusCtrl.CreateSegment)
	doReg("POST", "/customer-sessions/:id/edit-lock", csPlusCtrl.AcquireEditLock)
	doReg("DELETE", "/customer-sessions/:id/edit-lock", csPlusCtrl.ReleaseEditLock)
	doReg("GET", "/customer-sessions/:id/edit-lock", csPlusCtrl.GetEditLock)
	doReg("POST", "/customer-sessions/:id/internal-notes", csPlusCtrl.AddInternalNote)
	doReg("GET", "/customer-sessions/:id/internal-notes", csPlusCtrl.ListInternalNotes)
	doReg("POST", "/customer-sessions/:id/apply-tag-rule", csPlusCtrl.ApplyTagRule)
	doReg("GET", "/session-tag/rules", csPlusCtrl.ListTagRules)
	doReg("POST", "/session-tag/rules", csPlusCtrl.SaveTagRule)
	doReg("GET", "/customer-service/agent-status-board", csPlusCtrl.GetAgentStatusBoard)
	doReg("GET", "/quick-reply/folders", csPlusCtrl.ListQuickReplyFolders)
	doReg("POST", "/quick-reply/folders", csPlusCtrl.CreateQuickReplyFolder)
	doReg("POST", "/quick-reply/folders/:id/reorder", csPlusCtrl.ReorderQuickReplyFolder)
	doReg("DELETE", "/quick-reply/folders/:id", csPlusCtrl.DeleteQuickReplyFolder)

	doReg("GET", "/customer-service/ai-suggestions", aiSuggestionCtrl.GetSuggestions)

	notifCtrlAlias := controller.NewNotificationController(service.NewNotificationService(gormDB))
	doReg("POST", "/mentions/:id/read", notifCtrlAlias.MarkRead)
	doReg("GET", "/mentions/mine", notifCtrlAlias.List)
	systemInfoCtrl := controller.NewSystemInfoController()
	doReg("GET", "/system/menus", systemInfoCtrl.SystemMenus)

	domainDB := gormDB
	domainPoolRepo := repository.NewDomainPoolRepository(domainDB)
	domainCtrl := controller.NewDomainPoolController(
		service.NewDomainPoolService(domainDB),
		service.NewDomainHealthService(domainDB, domainPoolRepo),
	)
	doReg("GET", "/domain-pool", domainCtrl.List)
	doReg("GET", "/domain-pool/list", domainCtrl.List)
	doReg("GET", "/domain-pool/:id", domainCtrl.GetByID)

	doRegAdmin("POST", "/domain-pool", domainCtrl.Create)
	doRegAdmin("PUT", "/domain-pool/:id", domainCtrl.Update)
	doRegAdmin("DELETE", "/domain-pool/:id", domainCtrl.Delete)

	doRegAdmin("POST", "/domain-pool/check-all", domainCtrl.CheckAllDomains)
	doReg("GET", "/domain-pool/health", domainCtrl.HealthCheckAll)
	doReg("GET", "/domain-pool/alerts", domainCtrl.ListAlerts)
	doRegAdmin("POST", "/domain-pool/alerts/:id/resolve", domainCtrl.ResolveAlert)
	doReg("GET", "/domain-pool/:id/blacklist", domainCtrl.CheckBlacklist)
	doRegAdmin("POST", "/domain-pool/:id/check", domainCtrl.CheckDomainByID)
	doRegAdmin("POST", "/domain-pool/:id/rotate", domainCtrl.RotateToBackup)
	doRegAdmin("POST", "/domain-pool/:id/suspend", domainCtrl.SuspendDomain)

	backupGapCtrl := controller.NewBackupGapController()
	doRegAdmin("GET", "/backup/list", backupGapCtrl.List)
	doRegAdmin("GET", "/backup/stats", backupGapCtrl.Stats)
	doRegAdmin("GET", "/backup/strategy", backupGapCtrl.GetStrategy)
	doRegAdmin("PUT", "/backup/strategy", backupGapCtrl.SaveStrategy)
	doRegAdmin("POST", "/backup/create", backupGapCtrl.Create)
	doRegAdmin("GET", "/backup/:id/preview", backupGapCtrl.Preview)
	doRegAdmin("POST", "/backup/:id/restore", backupGapCtrl.Restore)
	doRegAdmin("DELETE", "/backup/:id", backupGapCtrl.Delete)
	doRegAdmin("GET", "/backups/stats", backupGapCtrl.Stats)

	ragEvalCtrl := controller.NewRagEvalGapController()
	doReg("GET", "/rag/eval/latest", ragEvalCtrl.Latest)
	doReg("GET", "/rag/eval/runs", ragEvalCtrl.Runs)
	doReg("POST", "/rag/eval/run", ragEvalCtrl.Run)
	doReg("POST", "/rag/eval/upload", ragEvalCtrl.Upload)
	doReg("GET", "/rag/eval/diff", ragEvalCtrl.Diff)

	analyticsGapCtrl := controller.NewAnalyticsGapController()
	doReg("GET", "/analytics/cohort", analyticsGapCtrl.Cohort)
	doReg("GET", "/analytics/path", analyticsGapCtrl.Path)

	emailGapCtrl := controller.NewEmailGapController()
	doReg("GET", "/email/deliverability", emailGapCtrl.Deliverability)
	doReg("GET", "/email/bounces/breakdown", emailGapCtrl.BounceBreakdown)
	doReg("GET", "/email/domain-reputation", emailGapCtrl.DomainReputation)
	doRegAdmin("POST", "/email/domains/:id/suspend", emailGapCtrl.SuspendDomain)
	doReg("POST", "/email/test-send", emailGapCtrl.TestSend)
	doReg("GET", "/user-segments/rfm", emailGapCtrl.RFMMatrix)
	doReg("GET", "/user-segments/rfm/stats", emailGapCtrl.RFMMatrixStats)
	doRegAdmin("POST", "/message-hub/dlq/batch-retry", csPlusCtrl.DLQBatchRetry)
	doReg("GET", "/message-hub/dlq", csPlusCtrl.DLQList)
	doRegAdmin("POST", "/message-hub/dlq/:id/retry", csPlusCtrl.DLQRetryOne)
	doRegAdmin("DELETE", "/message-hub/dlq/:id", csPlusCtrl.DLQDrop)
	doReg("GET", "/knowledge/playground/presets", emailGapCtrl.PlaygroundPresets)
	doReg("PATCH", "/knowledge/documents/:id/public-visibility", controller.NewHelpCenterController().SetVisibility)
	doReg("PATCH", "/knowledge/documents/:id/help-center-status", controller.NewHelpCenterController().SetStatus)
	doReg("GET", "/knowledge/help-center/top", controller.NewHelpCenterController().TopArticles)
	doReg("POST", "/knowledge/help-center/retrieval-test", controller.NewHelpCenterController().RetrievalTest)
	doRegAdmin("POST", "/clues/import/apply-suggestions", emailGapCtrl.ClueApplySuggestions)
	doRegAdmin("POST", "/clues/:id/merge", emailGapCtrl.ClueMerge)
	doRegAdmin("POST", "/clues/force-create", emailGapCtrl.ClueForceCreate)

	integrationCtrl := controller.NewIntegrationController()
	doReg("GET", "/integrations/list", integrationCtrl.GetAccountList)
	doReg("GET", "/integrations/:id", integrationCtrl.GetAccountByID)

	opLogCtrl := controller.NewOperationLogController()
	doRegAdmin("GET", "/operation-logs", opLogCtrl.GetList)
	doRegAdmin("GET", "/operation-logs/list", opLogCtrl.GetList)
	doRegAdmin("GET", "/operation-logs/:id", opLogCtrl.GetByID)
	doRegAdmin("GET", "/operation-logs/statistics", opLogCtrl.GetStatistics)
	doRegAdmin("GET", "/operation-logs/export", opLogCtrl.ExportLogs)
	doRegAdmin("POST", "/operation-logs/clean", opLogCtrl.CleanLogs)
	doRegAdmin("POST", "/operation-logs/:id/rollback", opLogCtrl.RollbackLog)

	churnCtrl := opsctrl.NewChurnPredictionController()
	doReg("GET", "/churn-prediction", churnCtrl.GetChurnPredictions)
	doReg("GET", "/churn-prediction/list", churnCtrl.GetChurnPredictions)
	doReg("GET", "/churn-prediction/users", churnCtrl.GetHighRiskUsers)
	doReg("GET", "/churn-prediction/warnings", churnCtrl.GetChurnWarnings)
	doReg("GET", "/churn-prediction/unhandled-warnings", churnCtrl.GetUnhandledWarnings)
	doReg("GET", "/churn-prediction/statistics", churnCtrl.GetChurnStatistics)
	doReg("GET", "/churn-prediction/risk-distribution", churnCtrl.GetRiskDistribution)
	doReg("GET", "/churn-prediction/model-config", churnCtrl.GetModelConfig)
	doRegAdmin("POST", "/churn-prediction/model-config", churnCtrl.SaveModelConfig)
	doReg("GET", "/churn/prediction", churnCtrl.GetChurnPrediction)
	doReg("GET", "/churn/predictions", churnCtrl.GetChurnPredictions)
	doReg("GET", "/churn/high-risk-users", churnCtrl.GetHighRiskUsers)
	doReg("GET", "/churn/warnings", churnCtrl.GetChurnWarnings)
	doReg("GET", "/churn/unhandled-warnings", churnCtrl.GetUnhandledWarnings)
	doReg("GET", "/churn/model-config", churnCtrl.GetModelConfig)
	doReg("GET", "/churn/statistics", churnCtrl.GetChurnStatistics)
	doReg("GET", "/churn/risk-distribution", churnCtrl.GetRiskDistribution)
}
