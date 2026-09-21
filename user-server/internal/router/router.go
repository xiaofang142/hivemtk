package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
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
	// /files 托管改走守卫版（同源可执行扩展名 403 + nosniff/sandbox），
	// 替代裸 r.Static，堵素材库/渠道媒体任意扩展名落盘后的同源直出。
	r.GET("/files/*filepath", serveUploadsGuarded(uploadDir))
	logger.Infof("[Router] static file server registered (guarded): /files -> %s", uploadDir)

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

	// 全局请求体封顶（R19）：注册位置必须在**任何会读 body 的中间件与鉴权之前** ——
	// gin 的引擎级 Use 只对注册它之后登记的路由生效，晚一步就等于给已注册的路由留口子。
	// multipart 由中间件内部跳过，其内存占用改由下面的 MaxMultipartMemory 收（gin 默认 32MB 偏大）。
	r.Use(middleware.BodyLimit(middleware.BodyLimitFromEnv()))
	r.MaxMultipartMemory = middleware.MaxMultipartMemoryBytes()

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
	// 订单草稿运行时（T-P2-06）：必须在 BuildSmartOrchestrator 之前，后者按全局运行时
	// 决定挂不挂生产者。默认旗子 off ⇒ 这一步只是打一行"未装配"日志，不产生任何协程。
	app.InitOrderDraftRuntime(gormDB)

	// 异步审批运行时（T-P3-02）：清扫器 + 挂起/恢复桥。放在 SOP 调度器之后、路由之前 ——
	// 桥要在第一笔流量进来之前挂上，否则会出现"审批服务已能收单、流程侧无人被叫醒"的窗口。
	// 默认旗子 off ⇒ 这一步只打一行"未装配"日志，不产生任何协程。
	app.InitApprovalRuntime(gormDB)

	// 统一待办底座（T-P3-03）：同样必须在 BuildSmartOrchestrator 之前 —— 编排器按全局
	// 服务决定挂不挂"转人工 → 投递会话待办"的生产者。本竖没有旗子：底座拿不到 DB 句柄
	// 就是不装配，此时下面那组 /api/human-tasks/* 端点全部回 503（不会回一个空列表骗人）。
	app.InitHumanTaskRuntime(gormDB)

	// 商机底座（T-P4-04）：同一位置约束（在编排器与路由之前）。本竖没有旗子，也不产生协程 ——
	// 不装配就是 /api/opportunity/* 全部回 503，不会回一个空列表骗人。
	app.InitOpportunityRuntime(gormDB)

	engine := app.BuildSalesEngine(gormDB)
	// 双模式生命周期（T-P5-02 / W-4）：被动复用上面这个会话引擎实例，主动走 SOP 编排出口。
	// 放在 BuildSalesEngine 之后、路由之前 —— 路由挂的必须是这个已装配的运行时。
	app.InitAgentLifecycles(gormDB, engine)
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

		// 电商订单回调（T-P2-02）：外部平台没有会话凭证，只能走公开组 + HMAC 验签。
		// 与 auth 组里那条旧路径并存，旧路径已标 deprecation。
		setupOrderWebhookRoutes(public)

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

		// ⚠️ 2026-09-16（审计 DB-07）：此处原先是 `_ = ...Create(...).Error`，
		// 写入失败被完全丢弃。而 GeoCrawlerVisit 当时又**没登记进 AutoMigrate**，
		// 全新部署根本不会建 geo_crawler_visits 表 —— 于是"GEO 爬虫访问统计"会
		// 永久为 0 且**日志里一行都没有**。
		//
		// 建表已补进 internal/pkg/db/migrate.go；这里再把错误显性化：
		// 即便将来表再次出问题，也会在日志里留痕，而不是静默归零。
		go func() {
			if gormDB == nil {
				return
			}
			if err := gormDB.WithContext(context.Background()).Create(&geomodel.GeoCrawlerVisit{
				Engine:    engine,
				Path:      path,
				UserAgent: ua,
				IP:        ip,
			}).Error; err != nil {
				logger.Warnf("[GEO] 记录 AI 爬虫访问失败（engine=%s path=%s）: %v", engine, path, err)
			}
		}()
	}))

	bridgeTokenCtrl := controller.NewBridgeTokenController()
	auth.GET("/bridge/token/status", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.GetStatus)
	auth.POST("/bridge/token/reset", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.ResetBridgeToken)
	tlCtrl := controller.NewTraceLearningController(trace_learning.Global())
	auth.POST("/monitor/trace-eval/trigger", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), tlCtrl.TriggerEval)
	auth.GET("/monitor/trace-eval/logs", middleware.JWTAuthMiddleware(), tlCtrl.EvalLogs)
	auth.GET("/monitor/knowledge-weights", middleware.JWTAuthMiddleware(), tlCtrl.KnowledgeWeights)

	auth.Use(middleware.JWTAuthMiddleware())
	{
		// 业务链路监控 /api/monitor/*（health/anomalies/node-health/latency/
		// lifecycle/traces/trace-tree）—— 返回会话链路与业务指标，必须登录态可见。
		//
		// ⚠️ 必须挂在 auth.Use(JWTAuthMiddleware()) 之后：gin 的 RouterGroup.Use
		// 只在注册时把当时的 handler 链快照进路由，对**之前**注册的路由不生效。
		// 历史缺陷：该行原位于 294 行 Use 之前，导致 7 个监控接口全部匿名可访问
		// （启动日志中 handler 数为 11，而需登录的 /api/users 为 12）。
		monitor.RegisterRoutes(auth)

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

		setupMaterialRoutes(auth)

		setupClueRoutes(auth)
		SetupGeoRoutes(auth, gormDB)
		SetupBrowserAutomationRoutes(auth, r, gormDB)
		setupLeadMiningRoutes(auth)

		setupCustomerRFMRoutes(auth)

		setupRecoveryQueueRoutes(auth)

		// 统一人工待办 /api/human-tasks/*（T-P3-03）：必须挂在这个块里 —— auth 组在本块
		// 入口已 Use(JWTAuthMiddleware())，动作端点的操作者身份取自令牌里的 user_id。
		setupHumanTaskRoutes(auth)

		// 审批详情与裁决 /api/approvals/*（T-P3-04）：待办池里 kind=approval 那几行的
		// 唯一出口。与上一行同一位置约束（身份取自令牌），且必须在
		// app.InitApprovalRuntime 之后 —— 它决定这里是全局实例还是"回 503 的空壳"。
		setupApprovalRoutes(auth)

		// 商机 /api/opportunity/*（T-P4-04）：与上一行同一位置约束（写入口的操作者身份
		// 取自令牌，虽然本卡还不记 actor —— 那件事归哪张表还没拍板）。
		setupOpportunityRoutes(auth)

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
			// R-B1：总线 Data 与 DB 补拉路径逐键一致（msg_id 必带——扩展端复合去重键
			// msg_id|conversation_id 依赖它；曾缺 msg_id 致同会话第二条起静默丢消息）。
			// msg_id 与落库行同源：service.ContentHashMsgID ≡ DeliverBridgeOutbound 赋值处。
			bridge.GlobalSSEBus.Publish(bridge.BuildOutboundSSEEvent(bridge.OutboundEventData{
				HubID:          hubID,
				MsgID:          service.ContentHashMsgID(channel, convID, content),
				Platform:       channel,
				AccountID:      accountID,
				ConversationID: convID,
				Content:        content,
				MsgType:        msgType,
				ReceiverID:     receiverID,
				IsAIReply:      isAIReply,
				CreatedAt:      createdAt,
			}))
		})

		tooluseBridgeAdapter := bridge.NewBridgeReachAdapter(
			app.NewIntegrationReachAdapterFromDB(gormDB),
			bridgeIngressSvc,
		)
		bridge.GlobalBridgeReachAdapter = tooluseBridgeAdapter

		douyinLeadMiner := webhookSvc.DouyinLeadMiner()
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

		setupOrderDraftRoutes(auth)

		setupLTCRoutes(auth)

		app.SetupToolPermissionRoutes(auth)

		setupAIToolConfigRoutes(auth, gormDB)

		app.SetupInferenceRoutes(auth)

		// 智能体双模式运行入口（T-P5-02）：按 agent_id 读出 agent_mode 再分派。
		app.SetupAgentLifecycleRoutes(auth)

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

	// Swagger 文档路由（dev-only）：RegisterSwaggerRoutes 内部以 ENABLE_SWAGGER=true 且仅本机访问双重门控，
	// 未启用时直接 return，不向生产暴露任何文档端点。
	RegisterSwaggerRoutes(r)
}
