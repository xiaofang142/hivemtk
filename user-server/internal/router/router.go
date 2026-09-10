package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/bridge"
	channelgw "hivemtk-user/internal/channelgw"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/controller"
	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/monitor"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/trace_learning"
	"hivemtk-user/internal/service/translation"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var allowedCORSOrigins = parseCORSOrigins(os.Getenv("CORS_ALLOW_ORIGINS_USER"))

var sseAllowedCORSOrigins = parseCORSOrigins(os.Getenv("SSE_CORS_ALLOW_ORIGINS"))

func isLocalEnv() bool {
	for _, k := range []string{"APP_ENV", "MODE"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
		switch v {
		case "", "dev", "development", "debug", "test", "local":
			return true
		}
	}
	return false
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return "https"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func isSameOrigin(origin string, r *http.Request) bool {
	if origin == "" || r == nil || r.Host == "" {
		return false
	}
	return origin == requestScheme(r)+"://"+r.Host
}

func sseOriginAllowed(origin string, r *http.Request) bool {
	if isSameOrigin(origin, r) {
		return true
	}
	if origin == "" {
		return false
	}
	for _, list := range [][]string{sseAllowedCORSOrigins, allowedCORSOrigins} {
		for _, a := range list {
			if a == origin {
				return true
			}
		}
	}
	return false
}

func parseCORSOrigins(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allow := false
		if origin != "" {
			switch {
			case strings.HasPrefix(origin, "chrome-extension://"):

				allowedExts := strings.Split(os.Getenv("CORS_ALLOWED_EXTENSIONS"), ",")
				for _, ext := range allowedExts {
					if strings.TrimSpace(ext) != "" && origin == strings.TrimSpace(ext) {
						allow = true
						break
					}
				}
				// fail-closed：未配置白名单时不再放行任意扩展来源
				if !allow {
					logger.Infof("[CORS] 拒绝未列入 CORS_ALLOWED_EXTENSIONS 的扩展来源: %s", origin)
				}
			default:

				fullPath := c.Request.URL.Path
				if strings.HasSuffix(fullPath, "/outbox/sse") || strings.Contains(fullPath, "outbox/sse") {
					allow = sseOriginAllowed(origin, c.Request)
				} else {
					for _, a := range allowedCORSOrigins {
						if a == origin {
							allow = true
							break
						}
					}
				}
			}
		}
		if allow {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Requested-With,X-Trace-Id,Last-Event-ID,Cache-Control")
		c.Header("Access-Control-Expose-Headers", "Last-Event-ID,X-Trace-Id")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

var HealthRedis Pinger

// SetHealthRedis 由 main 在启动时注入 Redis 客户端（仅当 REDIS_HOST 配置可达时）。
func SetHealthRedis(p Pinger) {
	HealthRedis = p
}

func Setup(r *gin.Engine, gormDB *gorm.DB) {

	var whatsappCloudSvc *service.WhatsAppCloudService
	var webhookSvc *service.WebhookService
	var dingtalkAppSvc *service.DingTalkAppService

	uploadDir := os.Getenv("STORAGE_LOCAL_BASE_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}
	_ = os.MkdirAll(uploadDir, 0o750)
	r.Static("/files", uploadDir)
	logger.Infof("[Router] static file server registered: /files -> %s", uploadDir)

	r.HandleMethodNotAllowed = true
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"code":    "METHOD_NOT_ALLOWED_405",
			"message": "请求方法不被支持",
			"method":  c.Request.Method,
			"path":    c.Request.URL.Path,
		})
	})

	r.Use(corsMiddleware())
	r.Use(gin.Recovery())

	r.Use(middleware.LocaleMiddleware())

	r.Use(middleware.ContextMiddleware())

	app.InitEventBus()

	injectMiddlewarePorts()

	r.GET("/health", HealthCheck(HealthRedis, gormDB))
	r.GET("/healthz", LivenessCheck())
	r.GET("/readyz", ReadinessCheck(HealthRedis, gormDB))

	// 路由表调试端点：默认仅本地/开发环境开放；生产需显式 ENABLE_DEBUG_ROUTES=true
	if os.Getenv("ENABLE_DEBUG_ROUTES") == "true" || isLocalEnv() {
		r.GET("/__debug__/routes", controller.DebugRoutesHandler(r))
	}

	r.Use(middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		RPS:        1000,
		BucketSize: 20000,
		Enabled:    true,
		ExemptPaths: []string{
			"/api/bridge/ingest",
			"/api/ws/channel",
		},
	}))

	r.Use(middleware.TraceMiddleware())

	r.Use(middleware.APIInteractionLogger())

	r.Use(middleware.AuditMiddleware())

	liveCodeController := controller.NewLiveCodeController(service.NewLiveCodeService(gormDB))

	platformCtrl := controller.NewPlatformController()

	app.InitGlobalToolExecutor()
	app.InitGlobalToolRouter()
	app.RegisterAllAgentTools(gormDB)
	app.InitInferenceOrchestrator()

	engine := app.BuildSalesEngine(gormDB)
	kbRepo := repository.NewKnowledgeBaseRepository(gormDB)
	orchestrator := app.BuildSmartOrchestrator(engine, kbRepo, gormDB)
	aiAgentSvcGlobal := service.NewAIAgentService()
	channelBindingSvcGlobal := service.NewChannelAgentBindingService()
	csAgentSvcGlobal := service.NewCustomerServiceAgentService()
	orchestrator.SetCustomerServiceAgentService(context.Background(), csAgentSvcGlobal)

	langResolver := translation.NewLangConfigResolver(
		repository.NewChatChannelRepository(),
		repository.NewAIAgentRepository(),
	)

	service.InitAssetResolver(gormDB)
	contentservice.SetWorkflowAssetResolver(func(ctx context.Context) (json.RawMessage, bool) {
		if r := service.GetAssetResolver(); r != nil {
			if w, ok := r.GetActiveWorkflow(ctx); ok && w != nil {
				if b, err := json.Marshal(w); err == nil {
					return b, true
				}
			}
		}
		return nil, false
	})

	public := r.Group("/api")
	{
		setupPublicRoutes(public, liveCodeController, platformCtrl, gormDB)
		setupChatPublicRoutes(public, gormDB, orchestrator, langResolver)
		setupSSORoutes(public, gormDB)
		setupSelfServiceRoutes(public, gormDB)

		hcCtrl := controller.NewHelpCenterController()
		public.GET("/public/help-center/categories", hcCtrl.Categories)
		public.GET("/public/help-center/articles", hcCtrl.Articles)
		public.GET("/public/help-center/articles/:id", hcCtrl.ArticleDetail)
		public.GET("/public/help-center/search", hcCtrl.Search)
	}

	setupChatPublicWebSocket(r, langResolver)

	setupCardShareRoutes(r, gormDB)

	setupEmbedStaticRoutes(r)

	auth := r.Group("/api")
	auth.Use(middleware.AICrawlerMonitor(func(engine, path, ua, ip string) {

		go func() {
			if gormDB == nil {
				return
			}
			_ = gormDB.WithContext(context.Background()).Create(&geomodel.GeoCrawlerVisit{
				Engine:    engine,
				Path:      path,
				UserAgent: ua,
				IP:        ip,
			}).Error
		}()
	}))

	monitor.RegisterRoutes(auth)

	bridgeTokenCtrl := controller.NewBridgeTokenController()
	auth.GET("/bridge/token/status", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.GetStatus)
	auth.POST("/bridge/token/reset", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.ResetBridgeToken)
	tlCtrl := controller.NewTraceLearningController(trace_learning.Global())
	auth.POST("/monitor/trace-eval/trigger", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), tlCtrl.TriggerEval)
	auth.GET("/monitor/trace-eval/logs", middleware.JWTAuthMiddleware(), tlCtrl.EvalLogs)
	auth.GET("/monitor/knowledge-weights", middleware.JWTAuthMiddleware(), tlCtrl.KnowledgeWeights)

	auth.Use(middleware.JWTAuthMiddleware())
	{
		setupAuthRoutes(auth, gormDB)

		setupUserRoutes(auth)

		setupAccountRoutes(auth)

		setupAlertRoutes(auth)

		setupShortLinkRoutes(auth, public, gormDB)

		setupLiveCodeRoutes(auth, liveCodeController)

		setupEmailRoutes(auth, gormDB)

		setupSmsRoutes(auth, gormDB)

		setupCardRoutes(auth, gormDB)

		setupCardStatsRoutes(auth, gormDB)

		setupDomainPoolRoutes(auth, gormDB)

		setupMaterialRoutes(auth)

		setupClueRoutes(auth)
		SetupGeoRoutes(auth, gormDB)
		SetupBrowserAutomationRoutes(auth, r, gormDB)
		setupLeadMiningRoutes(auth)

		setupCustomerRFMRoutes(auth)

		setupRecoveryQueueRoutes(auth)

		systemAdmin := auth.Group("")
		systemAdmin.Use(middleware.AdminAuthMiddleware())
		setupSystemRoutes(systemAdmin)

		setupSystemUserRoutes(auth)

		setupConfigParamRoutes(auth, gormDB)

		setupRoleRoutes(auth)

		setupPermissionRoutes(auth)

		setupRagRoutes(auth, gormDB)

		setupKnowledgeBaseRoutes(auth)

		setupWhatsappRoutes(auth, gormDB)

		whatsappCloudSvc = service.NewWhatsAppCloudService(gormDB)
		setupWhatsAppCloudRoutes(auth, whatsappCloudSvc, gormDB)

		webhookSvc = service.NewWebhookService(gormDB)
		webhookSvc.SetAgentBindingService(context.Background(), channelBindingSvcGlobal)
		webhookSvc.SetSmartOrchestrator(context.Background(), orchestrator)
		dingtalkAppSvc = service.NewDingTalkAppService(gormDB, webhookSvc)

		setupDingTalkAppRoutes(auth, dingtalkAppSvc)

		setupTelegramRoutes(auth, gormDB)

		setupQQRoutes(auth, gormDB)

		setupFeishuRoutes(auth, gormDB)

		bridgeIngressSvc := service.NewInboxIngressService()

		setupWechatRoutes(auth, gormDB, bridgeIngressSvc)

		setupTiktokRoutes(auth, gormDB)

		setupWeComRoutes(auth, gormDB)

		setupCustomerServiceRoutes(auth, aiAgentSvcGlobal, langResolver)

		copilotCtrl := controller.NewManageCoPilotController()
		auth.GET("/manage/co-pilot/config", copilotCtrl.GetConfig)

		smartRouterCtrl := controller.NewManageSmartRouterController()

		ragEvalCtrl := controller.NewManageRagEvalController()
		auth.GET("/manage/rag-eval/runs", ragEvalCtrl.List)
		auth.GET("/manage/rag-eval/runs/:id", ragEvalCtrl.Detail)

		dataExportCtrl := controller.NewManageDataExportController()
		auth.GET("/manage/data-export/:customer_id", dataExportCtrl.Export)

		typingPredictCtrl := controller.NewManageTypingPredictController()
		auth.GET("/manage/typing-predict", typingPredictCtrl.Predict)

		handoffCtrl := controller.NewHandoffChainController()
		auth.GET("/manage/session-chain/sla-config", handoffCtrl.GetAutoResolveConfig)
		auth.GET("/manage/rules", handoffCtrl.ListRules)

		manageAdmin := auth.Group("/manage", middleware.AdminAuthMiddleware())
		{
			manageAdmin.POST("/co-pilot/evaluate", copilotCtrl.Evaluate)
			manageAdmin.PUT("/co-pilot/config", copilotCtrl.SetConfig)
			manageAdmin.POST("/smart-router/match", smartRouterCtrl.MatchAgent)
			manageAdmin.POST("/rag-eval/run", ragEvalCtrl.Run)
			manageAdmin.PUT("/session-chain/sla-config", handoffCtrl.SaveAutoResolveConfig)
			manageAdmin.POST("/session-chain/reopen", handoffCtrl.ReopenOnInboundMessage)
			manageAdmin.POST("/rules", handoffCtrl.CreateRule)
			manageAdmin.DELETE("/rules/:id", handoffCtrl.DeleteRule)
			manageAdmin.PUT("/rules/:id/toggle", handoffCtrl.ToggleRule)
		}

		app.SetBridgeIngressSvc(bridgeIngressSvc)
		bridgeHandler := bridge.NewBridgeIngestHandler(bridgeIngressSvc)

		messageHubRepo := repository.NewMessageHubRepositoryWithDB(gormDB)
		bridgeHandler.SetOutboxQuerier(messageHubRepo)

		service.SetGlobalSSEPublisher(func(channel, accountID string, hubID uint64, convID, msgType, receiverID, content string, isAIReply bool, createdAt time.Time) {
			bridge.GlobalSSEBus.Publish(bridge.SSEEvent{
				ID:             strconv.FormatUint(hubID, 10),
				Event:          "new_outbound",
				ConversationID: convID,
				MsgType:        msgType,
				ReceiverID:     receiverID,
				Seq:            int(hubID),
				Data: map[string]any{
					"hub_id":          hubID,
					"platform":        channel,
					"account_id":      accountID,
					"conversation_id": convID,
					"content":         content,
					"msg_type":        msgType,
					"receiver_id":     receiverID,
					"is_ai_reply":     isAIReply,
				},
				Timestamp: createdAt,
			})
		})

		tooluseBridgeAdapter := bridge.NewBridgeReachAdapter(
			app.NewIntegrationReachAdapterFromDB(gormDB),
			bridgeIngressSvc,
		)
		bridge.GlobalBridgeReachAdapter = tooluseBridgeAdapter

		douyinLeadMiner := service.NewWebhookService(gormDB).DouyinLeadMiner()
		bridgeHandler.SetLeadMiner(douyinLeadMiner)

		bridgeWS := r.Group("/api")
		bridgeWS.Use(middleware.InitGuard())

		bridgeWS.Use(middleware.BridgeIngressGuard())

		bridgeWS.POST("/bridge/ingest", bridgeHandler.HandleHTTPIngest)
		bridgeWS.GET("/bridge/outbox", bridgeHandler.GetBridgeOutbox)
		bridgeWS.POST("/bridge/outbox/ack", bridgeHandler.AckBridgeOutbox)

		bridgeWS.GET("/bridge/outbox/sse", bridgeHandler.HandleOutboxSSE)

		bridgeWS.GET("/bridge/capabilities", controller.NewBridgeCapabilitiesController().GetCapabilities)

		bridgeWS.POST("/mcp", controller.NewMCPController().Handle)

		channelPipeline := channelgw.NewPipeline(bridgeIngressSvc)
		channelWSTransport := channelgw.NewWSTransport(channelPipeline, channelgw.Default)
		bridgeWS.GET("/ws/channel", channelWSTransport.HandleWS)

		tracing.Init(gormDB)
		tooluse.ToolTraceSink = tracing.ReportToolCall

		bridgeRepo := bridge.NewBridgeAccountRepository(gormDB)
		bridge.RegisterBridgeAccountRepo(bridgeRepo)
		bridge.RegisterOwnershipChecker(func(ctx context.Context, userID uint, channel, accountID string) (bool, error) {
			acc, err := bridgeRepo.GetByChannelAccount(ctx, channel, accountID)

			if err != nil {
				return false, err
			}
			if acc == nil {
				return false, nil
			}
			return acc.UserID == userID, nil
		})
		bridgeAccountCtrl := controller.NewBridgeAccountController()
		bridgeAccountCtrl.RegisterRoutes(auth)

		if bridgeIngressSvc != nil && webhookSvc != nil {
			bridgeIngressSvc.SetAITrigger(webhookSvc)
			webhookSvc.SetIngressSvc(bridgeIngressSvc)
			logger.Infof("[Bridge] bridge AITrigger 已注入（抖音/小红书/TikTok 网页私信 AI 链路已连通）")
		}
		if bridgeIngressSvc != nil {
			bridgeIngressSvc.SetInboxService(service.NewInboxService())
			logger.Infof("[Bridge] bridge InboxService 已注入（统一收件箱会话同步已连通）")
			bridgeIngressSvc.SetLeadMining(service.NewLeadMiningService())
			logger.Infof("[Bridge] bridge LeadMining 已注入（线索发掘异步监听已连通）")
		}

		setupChatChannelAdminRoutes(auth, gormDB)

		setupI18nRoutes(auth, gormDB)

		setupEventRoutes(auth)

		setupMessageRoutes(auth, gormDB)

		setupPlatformAccountRoutes(auth)

		setupWeComHealthRoutes(auth, gormDB)

		setupIntentRoutes(auth, gormDB)

		setupDialogueMemoryRoutes(auth, gormDB)

		setupReachPipelineRoutes(auth, gormDB)

		setupProactiveReachRoutes(auth, gormDB)

		setupChannelOverviewRoutes(auth, gormDB)

		wechatWebhookGroup := r.Group("/api")
		setupWechatWebhookRoutes(wechatWebhookGroup, gormDB, bridgeIngressSvc)

		setupSOPRoutes(auth, gormDB)

		setupWorkflowOrchestratorRoutes(auth, gormDB)

		setupLLMRoutingRoutes(auth)

		setupLLMProviderRoutes(auth)
		setupTraceRoutes(auth)
		setupSSEDashboardRoutes(auth)

		setupAnalyticsRoutes(auth)

		setupObjectionHandlerRoutes(auth)

		setupCustomerJourneyRoutes(auth)

		setupQualityRoutes(auth)

		setupSecurityAuditRoutes(auth, gormDB)

		setupBatchRoutes(auth)

		setupAIContentRoutes(auth)

		setupUserSegmentRoutes(auth)

		setupMarketingFlowRoutes(auth)

		setupCustomReportRoutes(auth)

		setupDashboardRoutes(auth, public)

		setupTemplateRoutes(auth)

		setupScriptRoutes(auth)

		setupABTestRoutes(auth)

		setupTuningRoutes(auth)

		setupAssetMarketRoutes(auth, gormDB)

		setupAssetBundleRoutes(auth, gormDB)

		setupChurnRoutes(auth)

		setupIntegrationRoutes(auth)

		setupRateQuotaRoutes(auth)

		setupPromptRoutes(auth)

		setupTypingPredictRoutes(auth)

		setupCommunityRoutes(auth)

		setupBackupRoutes(auth)

		setupMigrationRoutes(auth, gormDB)

		auth.POST("/upload", controller.UploadFile)

		setupToolDebugRoutes(auth)

		app.SetupToolPermissionRoutes(auth)

		setupAIToolConfigRoutes(auth, gormDB)

		app.SetupInferenceRoutes(auth)

		aiAgentCtrl := controller.NewAIAgentControllerWithService(aiAgentSvcGlobal)
		aiAgentCtrl.SetSalesEngine(engine)
		aiAgentCtrl.RegisterRoutes(auth)

		channelBindingCtrl := controller.NewChannelAgentBindingControllerWithService(channelBindingSvcGlobal)
		channelBindingCtrl.RegisterRoutes(auth)

		csAgentMountCtrl := controller.NewCustomerServiceAgentControllerWithService(csAgentSvcGlobal)
		csAgentMountCtrl.RegisterRoutes(auth)

		setupCompetitorFeatureRoutes(auth)

		setupFrontendAliases(auth, r, gormDB)
	}

	webhookCtrl := controller.NewWebhookController(webhookSvc)
	webhookCtrl.SetWhatsAppCloudService(whatsappCloudSvc)
	webhookCtrl.SetDingTalkAppService(dingtalkAppSvc)
	webhookCtrl.SetFeishuService(service.NewFeishuService(gormDB))
	webhookCtrl.SetSalesEngine(engine)
	webhookCtrl.SetSmartOrchestrator(orchestrator)
	webhookCtrl.SetLangResolver(langResolver)
	webhookCtrl.RegisterRoutes(r)

	webhookCtrl.SetAgentBindingService(channelBindingSvcGlobal)

	go service.ReconcileTelegramWebhooks(service.NewTelegramService(gormDB))
	service.StartGateSweeper(gormDB)

	platform := r.Group("/api/platform")
	platform.Use(middleware.InitGuard())
	platform.Use(middleware.JWTAuthMiddleware())
	platform.Use(middleware.AdminAuthMiddleware())
	{
		setupPlatformRoutes(platform, platformCtrl)
	}
}
