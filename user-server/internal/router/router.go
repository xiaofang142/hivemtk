package router

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/bridge"
	channelgw "hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/controller"
	georepo "hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/monitor"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
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
		// X-Bridge-Token 必须在允许头里：整组 /api/bridge/* 只认这一枚凭证头（不认 Authorization），
		// 而浏览器对自定义头要先过预检——这里没列出它，跨源网页调用方就会在预检阶段被拦，
		// 拿到的报错与「凭证不对」长得完全不同，很容易被当成服务端故障。
		//
		// X-Chat-* 那四枚是前台聊天窗的调用面：/api/chat/public/* 用 X-Chat-Visitor-Id 做会话归属
		// （controller/chat_public.go 里缺它直接回 400「X-Chat-Visitor-Id 必填」），用
		// X-Chat-Channel-Id 选渠道。仓库自带的 iframe 窗与 API 同源、不会触发预检，所以现网没炸过；
		// 但 ADR 的跨域/CDN 部署（docs/architecture/adr/ADR-011-chat-widget-embed.md §6.2）要求
		// 接入方自建前端直连 /api/chat/public/*，那种页面一发预检就死在允许头上，报错读起来像
		// 「服务端 403」而不是「头没登记」。
		//
		// 反过来，机器对机器的签名/凭证头（X-Ingress-Secret、X-Webhook-Secret、X-API-KEY、
		// 各渠道签名头、X-Knowledge-Token）一律不加：那些没有浏览器调用方，加进允许头只是把
		// 凭证的可达面平白摊给任意被放行的源。
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Requested-With,X-Trace-Id,Last-Event-ID,Cache-Control,X-Bridge-Token,X-Chat-App-Key,X-Chat-Channel-Id,X-Chat-Visitor-Id,X-Chat-Visitor-Token")
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

	RegisterFilesRoute(r)

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

	// 全局请求体封顶：注册位置必须在**任何会读 body 的中间件与鉴权之前** ——
	// gin 的引擎级 Use 只对注册它之后登记的路由生效，晚一步就等于给已注册的路由留口子。
	// multipart 由中间件内部跳过，其内存占用改由下面的 MaxMultipartMemory 收（gin 默认 32MB 偏大）。
	r.Use(middleware.BodyLimit(middleware.BodyLimitFromEnv()))
	r.MaxMultipartMemory = middleware.MaxMultipartMemoryBytes()

	r.Use(middleware.LocaleMiddleware())

	r.Use(middleware.ContextMiddleware())

	app.InitEventBus()

	injectMiddlewarePorts(gormDB)

	r.GET("/health", HealthCheck(HealthRedis, gormDB))
	r.GET("/healthz", LivenessCheck())
	r.GET("/readyz", ReadinessCheck(HealthRedis, gormDB))

	// 路由表调试端点：默认仅本地/开发环境开放；生产需显式 ENABLE_DEBUG_ROUTES=true
	//
	// "本地/开发开放"只管住了"要不要注册这条路"这一半；一旦注册，它就是**引擎级裸路由**，
	// 本机端口上的任何进程（以及转发到本机的反向代理、隧道、同网络的访客）都能一次拿走
	// 整张路由表——那是把全部攻击面按方法和路径名清单化地交给对方。注册条件不动（开发期
	// 确实要看路由），但读它需要超管身份：这条是调试面，不是前端功能面，加守卫不会碰到任何页面。
	if os.Getenv("ENABLE_DEBUG_ROUTES") == "true" || isLocalEnv() {
		r.GET("/__debug__/routes", middleware.JWTAuthMiddleware(), middleware.AdminAuthMiddleware(), controller.DebugRoutesHandler(r))
	}

	// 全局限流两条：装配期读一次塞进限流器构造，改完需重启才生效
	// （种子 ratelimit.global_* 标 Restart=true）。走直连范式——本包已 import service。
	// 默认值 1000/20000 即接线前 router 写死的生产值，行为零变更。
	r.Use(middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		RPS:        float64(service.GlobalConfigParam().GetInt(context.Background(), "ratelimit", "global_rps", 1000)),
		BucketSize: service.GlobalConfigParam().GetInt(context.Background(), "ratelimit", "global_bucket", 20000),
		Enabled:    true,
		ExemptPaths: []string{
			"/api/bridge/ingest",
			"/api/ws/channel",
		},
	}))

	// 路径级配额限流：自过滤——未列入 ratelimit.path_quotas 的路径直接放行；
	// 列入的路径按「每客户端（IP）× 路径」独立令牌桶计数（杜绝全站共享桶被单租户打满）。
	// 桶参数每请求热读参数中心（60s 缓存），改表后已有桶在参数变化时按新值重建。
	// 位置必须在 JWT 之前——未登录请求也计数，这是保护昂贵端点（AI/外部订单/trace-eval）的本意。
	r.Use(middleware.PathQuotaMiddleware())

	r.Use(middleware.TraceMiddleware())

	r.Use(middleware.APIInteractionLogger())

	r.Use(middleware.AuditMiddleware())

	liveCodeController := controller.NewLiveCodeController(service.NewLiveCodeService())

	platformCtrl := controller.NewPlatformController()

	app.InitGlobalToolExecutor()
	app.InitGlobalToolRouter()
	app.RegisterAllAgentTools(gormDB)
	app.InitInferenceOrchestrator()
	// 订单草稿运行时：必须在 BuildSmartOrchestrator 之前，后者按全局运行时
	// 决定挂不挂生产者。默认旗子 off ⇒ 这一步只是打一行"未装配"日志，不产生任何协程。
	app.InitOrderDraftRuntime(gormDB)

	// 销售工作台读侧：必须排在 InitOrderDraftRuntime 之后 —— 它要借草稿竖
	// 装配好的草稿/旅程/跟进服务（进程内内存，本进程唯一的写入方就在那条竖）。
	// 没有旗子：装配本身零行为变化，借不到就少几块数据并出声，不回空壳当"今天没单子"。
	app.InitSalesWorkbenchRuntime()

	// 异步审批运行时：清扫器 + 挂起/恢复桥。放在 SOP 调度器之后、路由之前 ——
	// 桥要在第一笔流量进来之前挂上，否则会出现"审批服务已能收单、流程侧无人被叫醒"的窗口。
	// 默认旗子 off ⇒ 这一步只打一行"未装配"日志，不产生任何协程。
	app.InitApprovalRuntime(gormDB)

	// 统一待办底座：同样必须在 BuildSmartOrchestrator 之前 —— 编排器按全局
	// 服务决定挂不挂"转人工 → 投递会话待办"的生产者。本竖没有旗子：底座拿不到 DB 句柄
	// 就是不装配，此时下面那组 /api/human-tasks/* 端点全部回 503（不会回一个空列表骗人）。
	app.InitHumanTaskRuntime(gormDB)

	// 待办 SLA 超时闭环：排在 InitHumanTaskRuntime 之后（读的就是它那张表），
	// 在路由之前装配。三态旗子 FF_LTC_HANDOFF_SLA_JOB（off|shadow|enforce），默认 off ⇒
	// 只打一行"不装配"日志、不产生协程。enforce 档逾期即发站内提醒、拖过一个 SLA 窗口再升级。
	app.InitHumanTaskSLA(gormDB)

	// Bad Case 底座：同一位置约束（在编排器与路由之前）。本竖也没有旗子 ——
	// 编排器按全局服务决定挂不挂"这一轮低质 → 留一条痕"的标记器，不装配时
	// 下面那组 /api/bad-cases/* 全部回 503。
	app.InitBadCaseRuntime(gormDB)

	// 知识库变更底座：同一位置约束（在编排器与路由之前），但**多一条**顺序要求 ——
	// 它必须排在 InitApprovalRuntime 之后，装配点优先复用全局审批服务（裁决入口在
	// /api/approvals/*）；那把旗子是 off 时就地构造一份只用于入队/读结论的，并出声
	// "没人能在 HTTP 上批准这一条"（见 app/kb_release_wiring.go 文件头）。
	// 本竖不加装配旗子：闸门已经是两道（FF_LTC_KB_CHANGE_GATE × 逐库 governed），
	// 而变更行在发布之前一行都不进 knowledge_chunks ⇒ 装上不等于线上有变化。
	app.InitKBReleaseRuntime(gormDB)

	// 业务通知口：同一位置约束（在编排器与路由之前）。排在这三个底座
	// （草稿/坏例/知识库变更）之后是读法上的顺序 —— 它只登记一台通知服务，
	// 不依赖前面任何一个的实例。本竖没有旗子、也不产生协程：拿到 DB 即装配，
	// 拿不到就全局置空（生产者全部 no-op，业务动作照常成功，只是铃铛不响）。
	app.InitBusinessNotifier(gormDB)

	// 商机底座：同一位置约束（在编排器与路由之前）。本竖没有旗子，也不产生协程 ——
	// 不装配就是 /api/opportunity/* 全部回 503，不会回一个空列表骗人。
	app.InitOpportunityRuntime(gormDB)

	// 报价两条腿（生成 / 发送）：必须排在 InitApprovalRuntime 与
	// InitHumanTaskRuntime **之后** —— 装配点优先复用全局审批服务（裁决入口在 /api/approvals/*），
	// 并在那一刻取待办出口。同样排在 InitGlobalToolExecutor 之后：发送腿那份外发服务要挂
	// 发送前闸门，而闸门的裁决来源是工具执行器装配时建的。本竖没有旗子也不产生协程，
	// 但会在 FF_LTC_APPROVAL_RESUME=off 时点名"批不了"这件事（见 quote_wiring.go 文件头）。
	app.InitQuoteRuntime(gormDB)

	// 账单派生腿：同一位置约束，排在报价之后只是读法上的顺序（本竖不复用
	// 报价的服务实例，两把句柄各自 WithDB 注入）。不装配的代价不是一句 503，
	// 是 `accepted` 在全系统没有写入口 ⇒ 回款域没有起点，所以这里必须出声。
	app.InitBillRuntime(gormDB)

	// 回款腿：位置约束有**两处**而不是一处。除"排在路由之前"之外，
	// 它还必须早于下面 setupIntegrationRoutes 里那一次 NewIntegrationController ——
	// 那条回调路（外部电商推"这笔钱到了"）用的就是这一份实例，晚于构造则永远记不到钱。
	// 判据与顺序各由 order_webhook_payment_wiring_test.go 与 router 侧的用例守着。
	app.InitPaymentRuntime(gormDB)

	// 邮件排水运行时：必须在路由之前 —— /api/email/* 读的是同一个 status 字段的
	// 那一列，装配点晚于流量的话，列表里会出现"排水器刚把它改成发送中、读侧还在按待发送
	// 统计"的错位。本竖没有旗子：拿不到 DB 句柄就是不装配，不装配就是存量状态
	// （行永远停在 status=0），不会假装自己在发。
	app.InitEmailRuntime(gormDB)

	engine := app.BuildSalesEngine(gormDB)
	// 双模式生命周期：被动复用上面这个会话引擎实例，主动走 SOP 编排出口。
	// 放在 BuildSalesEngine 之后、路由之前 —— 路由挂的必须是这个已装配的运行时。
	app.InitAgentLifecycles(gormDB, engine)
	kbRepo := repository.NewKnowledgeBaseRepository(gormDB)
	orchestrator := app.BuildSmartOrchestrator(engine, kbRepo, gormDB)
	aiAgentRepoGlobal := repository.NewAIAgentRepositoryWithDB(gormDB)
	aiAgentSvcGlobal := service.NewAIAgentServiceWithRepo(aiAgentRepoGlobal)
	channelBindingSvcGlobal := service.NewChannelAgentBindingServiceWithRepos(
		repository.NewChannelAgentBindingRepositoryWithDB(gormDB), aiAgentRepoGlobal, aiAgentSvcGlobal,
	)
	csAgentSvcGlobal := service.NewCustomerServiceAgentServiceWithRepos(
		repository.NewCustomerServiceAgentRepositoryWithDB(gormDB),
		aiAgentRepoGlobal,
		repository.NewAgentStatusRepositoryWithDB(gormDB),
		aiAgentSvcGlobal,
	)
	orchestrator.SetCustomerServiceAgentService(context.Background(), csAgentSvcGlobal)

	langResolver := translation.NewLangConfigResolver(
		repository.NewChatChannelRepositoryWithDB(gormDB),
		aiAgentRepoGlobal,
	)

	service.InitAssetResolver(repository.NewLocalAssetRepository(gormDB))

	// SSO 服务只建一次：登录页/回调（公开档）与租户自配 IdP 读写（admin 档）共用
	// 同一份按租户缓存的适配器集合。
	ssoCtrl := controller.NewSSOController(service.NewSSOService(repository.NewSSOIdentityRepository(gormDB), repository.NewSystemConfigKVRepositoryWithDB(gormDB)))

	public := r.Group("/api")
	{
		setupPublicRoutes(public, liveCodeController, gormDB)
		setupChatPublicRoutes(public, gormDB, orchestrator, langResolver)
		setupSSORoutes(public, ssoCtrl)
		setupSelfServiceRoutes(public, gormDB)
		setupLegacyRoutes(public) // 已下线的 /api/knowledge 顶层域名：410 + 迁移指引（见 legacy_routes.go）

		// 电商订单回调：外部平台没有会话凭证，只能走公开组 + HMAC 验签。
		// 与 auth 组里那条旧路径并存，旧路径已标 deprecation。
		setupOrderWebhookRoutes(public)

		// 租户自助开户（T030）：公开档唯一可写端点，off 模式由 Service 判 4104
		setupTenantRoutes(public, nil, nil)

		// 功能模块市场：未登录也能逛（public 档只放 浏览/付款指引/监管告知 三类读口，
		// 下单与查单在 auth 档）。刻意不给它挂模块闸 —— 市场正是买模块的入口，
		// 拦了就永远买不了。
		setupModuleRoutes(public, nil, nil, nil)

		hcCtrl := controller.NewHelpCenterController()
		public.GET("/public/help-center/categories", hcCtrl.Categories)
		public.GET("/public/help-center/articles", hcCtrl.Articles)
		public.GET("/public/help-center/articles/:id", hcCtrl.ArticleDetail)
		public.GET("/public/help-center/search", hcCtrl.Search)

		// 闲鱼卡片落地页的三条埋点写口：页面无登录态，所以挂在公开组；
		// 凭证是渲染时按卡片 ID 签发的 HMAC token，handler 内校验后才写库。
		setupXianyuCardTrackRoutes(public, gormDB)
	}

	setupChatPublicWebSocket(r, langResolver)

	setupCardShareRoutes(r, gormDB)

	setupEmbedStaticRoutes(r)

	var crawlerVisitSvc *geoservice.CrawlerVisitService
	if gormDB != nil {
		crawlerVisitSvc = geoservice.NewCrawlerVisitService(georepo.NewGeoCrawlerVisitRepository(gormDB))
	}

	auth := r.Group("/api")
	auth.Use(middleware.AICrawlerMonitor(func(engine, path, ua, ip string) {
		if crawlerVisitSvc == nil {
			return
		}
		// 写库失败必须留痕，不能静默归零。
		go func() {
			if err := crawlerVisitSvc.RecordAnonymous(context.Background(), engine, path, ua, ip); err != nil {
				logger.Warnf("[GEO] 记录 AI 爬虫访问失败（engine=%s path=%s）: %v", engine, path, err)
			}
		}()
	}))

	bridgeTokenCtrl := controller.NewBridgeTokenController()
	auth.GET("/bridge/token/status", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.GetStatus)
	auth.POST("/bridge/token/reset", middleware.JWTAuthMiddleware(), middleware.RequireAdminMiddleware(), bridgeTokenCtrl.ResetBridgeToken)

	auth.Use(middleware.JWTAuthMiddleware())
	// 租户上下文中间件（specs/001-saas-multi-tenant T009）：必须在 JWT 之后——
	// 消费其写入的 tenant_id claim。off 模式绑定默认租户（等价现状）；
	// on 模式无有效租户 claim 即 401（fail-closed）。
	auth.Use(middleware.TenantMiddleware())
	// 租户/模块市场鉴权档由本文件下方（auth 组与 systemAdmin 组就绪处）挂载，
	// 见 setupTenantRoutes(nil, auth, systemAdmin) / setupModuleRoutes(nil, auth, systemAdmin)。
	// 仅超管能力面（备份/恢复/重启、全局配置写、LLM 与 MCP 凭据、功能开关放量、
	// 代他人重置口令……）集中升档的那一道守卫，判据见 middleware.RoutePolicyGuard。
	//
	// 位置必须在 JWT 之后：守卫从 ctx 读 role，早于 JWT 就等于每条命中都走"无身份 ⇒ 401"。
	// 也必须在任何 auth 路由注册之后、注册动作之前：gin 在注册时快照当时的 handler 链，
	// 这一行之前注册的那两条 /api/bridge/token/* 带不到它，它们靠内联
	// RequireAdminMiddleware 收口；任何在能力表里有归属的接口都不许注册在这一行之前。
	// 能力面守卫的第二档判据要先有数据源：菜单授予来自库里的 role_menu_permissions，
	// 装配期注一次，守卫内自带 60s 缓存与 stale-while-error。
	// 漏这一行的后果是**拦**（守卫把"查不到授予"按无授予判），不是放行，
	// 所以新增装配路径时必须一起带上它。
	middleware.SetMenuGrantProvider(service.NewMenuService().GrantProvider)
	auth.Use(middleware.RoutePolicyGuard())
	// 行级数据隔离：判"这一行数据是不是你的"，与上一行的能力面守卫相互独立
	// （能力面判角色有没有这个功能的门，这里判调用者有没有这一行的权限），两道都过才到 handler。
	//
	// 判据与登记表见 model.DataScopeRow；漏装配查询源（SetOwnerLookup）的后果是**拦**，
	// 与上面的菜单授予源同一条 fail-closed 口径。
	middleware.SetOwnerLookup(service.NewDataScopeService().LookupOwner)
	auth.Use(middleware.DataScopeGuard())

	// ============================================================
	// 功能模块闸（.specify/002-modular-subscription · FR-5.4）
	//
	// 位置必须在**鉴权之后、RoutePolicyGuard 之前**：
	//   - 在鉴权之后：匿名请求不该从模块闸得到"未开通模块"的答案，
	//     那会泄露本系统的模块结构；未开通判定只对已登录用户有意义。
	//   - 在 RoutePolicyGuard 之前：超管是「本系统内权限最高」，不是
	//     「买了所有模块」。闸放在权限之后，超管就成了绕过付费的后门。
	//
	// 本组没有显式 RequireModule(k)：模块归属由路径模式表
	// （middleware/module_route_patterns.go）判定，逐路由标注会漏挂。
	// ============================================================
	moduleGateSvc := service.NewModuleGateService()
	middleware.SetModuleGateChecker(moduleGateAdapter{gate: moduleGateSvc})
	moduleGateSvc.InstallWiring(context.Background())
	// 订阅闸（003 T022）：与模块闸同位置挂接（鉴权后、角色权限前）。
	// 仅 on 模式装配 —— 离线单机没有订阅概念（middleware 内部还有一次
	// IsMultiTenant 纵深判定，误挂也不会锁死离线用户）。
	middleware.SetSubscriptionLockChecker(subscriptionGateAdapter{svc: service.NewSubscriptionMirrorService()})
	if tenant.IsMultiTenant() {
		auth.Use(middleware.GuardSubscriptionPath())
	}
	auth.Use(middleware.GuardModulePath())

	{
		// 业务链路监控 /api/monitor/*（health/anomalies/node-health/latency/
		// lifecycle/traces/trace-tree）—— 返回会话链路与业务指标，必须登录态可见。
		//
		// ⚠️ 必须挂在 auth.Use(JWTAuthMiddleware()) 之后：gin 的 RouterGroup.Use
		// 只在注册时把当时的 handler 链快照进路由，对**之前**注册的路由不生效。
		// 挂错位置会让这 7 个监控接口全部匿名可访问。
		monitor.RegisterRoutes(auth)

		// 统一看板（D2 AI 全链路可观测性 + B2 渠道健康，specs/007）：只读、
		// 登录态可见，与 monitor 同组同调用点（鉴权语义一致）。
		RegisterObservabilityDashboardRoutes(auth, gormDB)

		// 链路自学习监控口：/monitor/trace-eval/logs 与 /monitor/knowledge-weights 在
		// 能力表里归 systemTrace，注册必须在 auth.Use(RoutePolicyGuard()) 之后，
		// 否则那两行表对它们不生效（gin 注册时快照当时的 handler 链）。
		tlCtrl := controller.NewTraceLearningController(trace_learning.Global())
		auth.POST("/monitor/trace-eval/trigger", middleware.RequireAdminMiddleware(), tlCtrl.TriggerEval)
		auth.GET("/monitor/trace-eval/logs", tlCtrl.EvalLogs)
		auth.GET("/monitor/knowledge-weights", tlCtrl.KnowledgeWeights)

		setupAuthRoutes(auth, gormDB)

		setupUserRoutes(auth)

		setupAccountRoutes(auth, gormDB)

		setupAlertRoutes(auth, gormDB)

		setupShortLinkRoutes(auth, public, gormDB)

		setupLiveCodeRoutes(auth, liveCodeController)

		setupEmailRoutes(auth, gormDB)

		setupSmsRoutes(auth, gormDB)

		setupCardRoutes(auth, gormDB)

		setupCardStatsRoutes(auth, gormDB)

		setupMaterialRoutes(auth)

		setupClueRoutes(auth, gormDB)
		SetupGeoRoutes(auth, gormDB)
		SetupBrowserAutomationRoutes(auth, r, gormDB)
		SetupEgoLiteRoutes(auth, r, gormDB) // ego-lite 腿（M1 骨架：runner-ws + 状态 + token 轮换）
		setupLeadMiningRoutes(auth)

		setupCustomerRFMRoutes(auth)

		setupRecoveryQueueRoutes(auth)

		// 统一人工待办 /api/human-tasks/*：必须挂在这个块里 —— auth 组在本块
		// 入口已 Use(JWTAuthMiddleware())，动作端点的操作者身份取自令牌里的 user_id。
		setupHumanTaskRoutes(auth)

		// Bad Case 队列与评测集导出 /api/bad-cases/*：与上一行同一位置约束
		// （打标/撤销/补录三个写入口的操作者身份取自令牌里的 user_id）。
		setupBadCaseRoutes(auth)

		// 审批详情与裁决 /api/approvals/*：待办池里 kind=approval 那几行的
		// 唯一出口。与上一行同一位置约束（身份取自令牌），且必须在
		// app.InitApprovalRuntime 之后 —— 它决定这里是全局实例还是"回 503 的空壳"。
		setupApprovalRoutes(auth)

		// 知识库变更与发布 /api/kb-changes/* + /api/kb-releases/*：与上一行同一
		// 位置约束（六个写入口的操作者身份取自令牌），且必须在 app.InitKBReleaseRuntime 之后。
		setupKBReleaseRoutes(auth)

		// 商机 /api/opportunity/*：与上一行同一位置约束（写入口的操作者身份
		// 取自令牌，虽然本处还不记 actor —— 那件事归哪张表还没拍板）。
		setupOpportunityRoutes(auth)

		// 报价 /api/quote/*：同一位置约束，而且比商机那条更硬 ——
		// 发送那一条的操作者要落进 sales_events.owner_id（"谁把这一版发出去的"），
		// 那个身份只来自令牌；匿名可访问等于审计里所有人都可以是任何人。
		setupQuoteRoutes(auth)

		// 账单 /api/bill/*：同一位置约束。这一条比报价那条更要紧 ——
		// 它是 `accepted` 在整个系统里的唯一生产写入口，匿名可访问等于任何人都能
		// 在任意一条报价上开出一张应收。
		setupBillRoutes(auth)

		// 两个 admin 档，按**数据域**分，不按"路由组"一刀切：
		//
		//   tenantAdmin —— 操作**租户表**（system_config / system_config_kv /
		//     obs_config / tenant_config_params …）的管理端点。租户 admin 本就该
		//     读写自己租户的这些数据（隔离 callback 已经把范围限死在本租户），
		//     所以守卫是 AdminAuthMiddleware（只判 role=="admin"）。
		//
		//   systemAdmin —— 操作**全局表与实例运行态**的管理端点（tenants 租户清单 /
		//     停用租户 / 备份恢复 / 重启 / 迁移记录 / 路由表导出）。这些是平台运营语义，
		//     守卫是 PlatformOperatorMiddleware：SaaS 模式下"admin"角色同时表示
		//     平台运营方与租户管理员，只判 role 区分不出来 ⇒ 租户 admin 会拿到
		//     看到/停用别的租户、重启实例的权限。平台运营方 = 本实例**首个租户**
		//     的管理员（tenants 表 ID 最小）；off 模式恒放行。
		//
		// 【为什么必须分两档】平台运营方守卫是"降敌意"方向（把本该能访问的人挡在外面），
		// 一旦套在整个 admin 组上就会**误伤租户内管理功能**——第一轮就是这么把
		// 「租户 admin 读自己的站点配置 / 工具集成 / Agent 设置 / OBS 配置 / 参数覆盖」
		// 全部打成 403 的。分档判据是**端点操作的数据落在哪张表**，不是"挂在哪个组"。
		//
		// 详见 middleware.PlatformOperatorMiddleware 与
		// .specify/bugs/cross-tenant-admin-privilege-escalation/。
		tenantAdmin := auth.Group("")
		tenantAdmin.Use(middleware.AdminAuthMiddleware())

		systemAdmin := auth.Group("")
		systemAdmin.Use(middleware.PlatformOperatorMiddleware())
		setupSystemRoutes(tenantAdmin, systemAdmin, gormDB)

		// 租户运营端点（T033）：跨租户可见数据 → 平台运营方守卫；
		// 公开档的 register 已在 setupTenantRoutes(public) 注册，此处补登录档与 admin 档
		setupTenantRoutes(nil, auth, systemAdmin)

		// 功能模块市场（下单/查单/我的模块）与 LLM 余额、套餐。
		// sync-logs 落 module_sync_logs（租户表）⇒ 租户档；
		// uncovered-routes 是进程内路由表（全局）⇒ 平台运营方档。
		setupModuleRoutes(nil, auth, tenantAdmin, systemAdmin)

		setupSystemUserRoutes(auth)

		// plainAuth 只挂只读的 Runtime；五个全量/写端点走租户 admin 档
		// （config-params 写的是本租户的覆盖行 tenant_config_params，见 config_param.go
		//  的 UpdateValue：on 模式落租户覆盖、不动平台默认值）
		setupConfigParamRoutes(auth, tenantAdmin, systemAdmin, gormDB)

		// 租户自配 IdP（R5-4）：写入的是本租户的 client_secret 与授权端点，
		// 与参数中心覆盖写同档，只挂租户 admin 组。
		setupSSOConfigRoutes(tenantAdmin, ssoCtrl)

		// 业务字典（阶段四）。dictionaries / dictionary_transitions 是**全局只读共享**
		// （specs/001 data-model.md：「dictionary 等纯只读共享数据 | 全局只读」），
		// 写端点改动的是全体租户共用的字典项 ⇒ 平台运营方档。
		setupDictionaryRoutes(auth, systemAdmin, gormDB)

		// 内容安全闸 /api/manage/content-guard/*（词库/策略/审查记录/处置）：
		// 五张 guard_* 表都是租户表（隔离 callback 限死在本租户），
		// 改词库/冻结/解冻决定"谁的内容能被放行" ⇒ 租户 admin 档。
		// 运行时由 app.InitContentGuardRuntime 在 router.Setup 之前装配；
		// 未装配时端点回 503（路由照挂，前端能拿到明确信号而不是 404）。
		setupContentGuardRoutes(tenantAdmin)

		setupRoleRoutes(auth)

		setupPermissionRoutes(auth)

		// 菜单权限（RBAC）：GET /api/menus（本人可见树）+ /api/system/menus 与
		// /api/system/roles/:code/menus 分配读写（RequireAdmin 在文件内挂）。
		setupMenuRoutes(auth)

		setupRagRoutes(auth, gormDB)

		setupKnowledgeBaseRoutes(auth, gormDB)

		setupWhatsappRoutes(auth, gormDB)

		whatsappCloudSvc = service.NewWhatsAppCloudService(repository.NewWhatsAppCloudAccountRepositoryWithDB(gormDB))
		setupWhatsAppCloudRoutes(auth, whatsappCloudSvc, gormDB)

		webhookSvc = service.NewWebhookService()
		webhookSvc.SetAgentBindingService(context.Background(), channelBindingSvcGlobal)
		webhookSvc.SetSmartOrchestrator(context.Background(), orchestrator)
		dingtalkAppSvc = service.NewDingTalkAppService(webhookSvc)

		setupDingTalkAppRoutes(auth, dingtalkAppSvc)

		setupTelegramRoutes(auth, gormDB)

		setupQQRoutes(auth, gormDB)

		setupFeishuRoutes(auth, gormDB)

		bridgeIngressSvc := service.NewInboxIngressService()

		setupWechatRoutes(auth, gormDB, bridgeIngressSvc)

		setupTiktokRoutes(auth, gormDB)

		setupWeComRoutes(auth, gormDB)

		setupCustomerServiceRoutes(auth, aiAgentSvcGlobal, langResolver, gormDB)

		copilotCtrl := controller.NewManageCoPilotController()
		auth.GET("/manage/co-pilot/config", copilotCtrl.GetConfig)

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

		sloCtrl := controller.NewSLOController()
		auth.GET("/manage/slo", sloCtrl.List)

		{
		}

		app.SetBridgeIngressSvc(bridgeIngressSvc)
		bridgeHandler := bridge.NewBridgeIngestHandler(bridgeIngressSvc)

		messageHubRepo := repository.NewMessageHubRepositoryWithDB(gormDB)
		bridgeHandler.SetOutboxQuerier(messageHubRepo)

		service.SetGlobalSSEPublisher(func(ctx context.Context, channel, accountID string, hubID uint64, convID, msgType, receiverID, content, mediaURL string, isAIReply bool, createdAt time.Time) {
			// R-B1：总线 Data 与 DB 补拉路径逐键一致（msg_id 必带——扩展端复合去重键
			// msg_id|conversation_id 依赖它；曾缺 msg_id 致同会话第二条起静默丢消息）。
			// msg_id 与落库行同源：service.ContentHashMsgID ≡ DeliverBridgeOutbound 赋值处。
			// 用 PublishCtx 而非 Publish：认领那一步要 UPDATE message_hub（租户表），
			// on 模式下 ctx 不带租户会被隔离守卫 fail-closed 拒绝
			// （"[SSE] 出站认领报错，放弃本次推送"）。ctx 由 service 侧派生，
			// 已带租户归属。
			bridge.GlobalSSEBus.PublishCtx(ctx, bridge.BuildOutboundSSEEvent(bridge.OutboundEventData{
				HubID:          hubID,
				MsgID:          service.ContentHashMsgID(channel, convID, content),
				Platform:       channel,
				AccountID:      accountID,
				ConversationID: convID,
				Content:        content,
				MsgType:        msgType,
				MediaURL:       mediaURL,
				ReceiverID:     receiverID,
				IsAIReply:      isAIReply,
				CreatedAt:      createdAt,
			}))
		})

		douyinLeadMiner := webhookSvc.DouyinLeadMiner()
		bridgeHandler.SetLeadMiner(douyinLeadMiner)

		bridgeWS := r.Group("/api")
		bridgeWS.Use(middleware.InitGuard())

		bridgeWS.Use(middleware.BridgeIngressGuard())

		bridgeWS.POST("/bridge/ingest", bridgeHandler.HandleHTTPIngest)
		bridgeWS.GET("/bridge/outbox", bridgeHandler.GetBridgeOutbox)
		bridgeWS.POST("/bridge/outbox/ack", bridgeHandler.AckBridgeOutbox)

		// B1-a：选择器健康上报。挂在桥接组里（只认实例级桥接凭证），与读判定结果
		// 的 GET 分开——见 controller.BridgeSelectorHealthController 文件头。
		selectorHealthCtrl := controller.NewBridgeSelectorHealthController(
			service.NewSelectorHealthService(repository.NewBridgeSelectorHealthRepository(gormDB)))
		bridgeWS.POST("/bridge/selector-health", selectorHealthCtrl.Ingest)

		bridgeWS.GET("/bridge/outbox/sse", bridgeHandler.HandleOutboxSSE)

		bridgeWS.GET("/bridge/capabilities", controller.NewBridgeCapabilitiesController().GetCapabilities)

		// MCP 工具调用入口单独一条门：它挂在桥接组里时，BRIDGE_INGEST_AUTH=off 这条
		// 「内网扩展可不带凭证上报」的逃生阀会把**服务端工具调用**一起放开，
		// 而且每台运营机器上的扩展凭证会变成工具调用凭证。判据见 middleware.MCPGuard。
		mcpGroup := r.Group("/api")
		mcpGroup.Use(middleware.InitGuard())
		// 租户解析器在这里注入而不是由 middleware 直接 import service：service 已经
		// import middleware，反向依赖会成环。on 模式下 MCP 的两条凭证链各自定归属——
		// 凭证行自带（凭证对）或调用方显式声明（单值密钥 + tenant_code），判据见 MCPGuard。
		mcpGroup.Use(middleware.MCPGuard(tenantCodeResolver))
		mcpGroup.POST("/mcp", controller.NewMCPController().Handle)

		channelPipeline := channelgw.NewPipeline(bridgeIngressSvc)
		channelWSTransport := channelgw.NewWSTransport(channelPipeline, channelgw.Default)
		// 渠道 WS 的归属只能由 register 帧的 (channel, account_id) 自证；解析函数住在
		// bridge 包，而 bridge 已经 import channelgw——反向依赖会成环，所以在这里接线。
		channelWSTransport.TenantBinder = bridge.BridgeTenantForRequest
		bridgeWS.GET("/ws/channel", channelWSTransport.HandleWS)

		tracing.Init(gormDB)
		tooluse.ToolTraceSink = tracing.ReportToolCall

		bridgeRepo := bridge.NewBridgeAccountRepository(gormDB)
		bridge.RegisterBridgeAccountRepo(bridgeRepo)
		// 归属判据：user_id=0 是租户资产（HTTP 登记入口只认实例级桥接凭证，没有用户身份），
		// 对租户内用户可见；非零是显式认领，只对本人放行。查无此行（含被隔离 callback
		// 挡掉的跨租户行）⇒ 不归当前用户，不能当成"尚未注册所以放行"。
		bridge.RegisterOwnershipChecker(func(ctx context.Context, userID uint, channel, accountID string) (bool, uint, error) {
			acc, err := bridgeRepo.GetByChannelAccount(ctx, channel, accountID)
			if err != nil {
				return false, 0, err
			}
			if acc == nil {
				return false, 0, nil
			}
			return acc.UserID == 0 || acc.UserID == userID, acc.TenantID, nil
		})
		bridgeAccountCtrl := controller.NewBridgeAccountController()
		bridgeAccountCtrl.RegisterRoutes(auth)

		// B1-a 读侧：运营面看判定结果（登录态）。与上面 bridgeWS 里的 POST 上报口
		// 刻意挂在不同组：扩展凭证不该读得到「全渠道健康度」这张运营表。
		auth.GET("/bridge/selector-health", selectorHealthCtrl.List)

		// 桥接扩展安装包下载口：浏览器扩展不上架商店，平台运营上传、租户在此下载。
		// 平台关闭时该服务返回"当前部署未接入平台"，不是 500。
		controller.NewBridgeExtensionController(
			service.NewBridgeExtensionService(platform.NewPlatformAPIClient()),
		).RegisterRoutes(auth)

		if bridgeIngressSvc != nil && webhookSvc != nil {
			bridgeIngressSvc.SetAITrigger(webhookSvc)
			webhookSvc.SetIngressSvc(bridgeIngressSvc)
			logger.Infof("[Bridge] bridge AITrigger 已注入（抖音/小红书/TikTok 网页私信 AI 链路已连通）")
		}
		if bridgeIngressSvc != nil {
			bridgeIngressSvc.SetInboxService(service.NewInboxService())
			logger.Infof("[Bridge] bridge InboxService 已注入（统一收件箱会话同步已连通）")
			bridgeIngressSvc.SetLeadMining(service.NewLeadMiningService(repository.NewClueRepositoryWithDB(gormDB)))
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
		setupSSEDashboardRoutes(auth, gormDB)

		setupAnalyticsRoutes(auth)

		setupObjectionHandlerRoutes(auth, gormDB)

		setupCustomerJourneyRoutes(auth)

		setupQualityRoutes(auth)

		// R-8 同址约束：401 遥测记录器在路由装配前注入句柄（middleware 自持 repository，
		// platform 仓 PlatformAuditMiddleware 同一先例）。未授权访问的归属人是攻击者不是
		// 租户，落 platform_config_kv 部署级键（与 D-2 would_deny 同款载体）。
		middleware.InitSecurityRecorder(gormDB)
		setupSecurityAuditRoutes(auth, gormDB)

		setupSecurityRulesRoutes(auth, gormDB)

		setupSupportMetricsRoutes(auth, gormDB)

		// T3 观测报表：读侧 + 聚合定时任务（docs/architecture/design/T3-observability-reports.md）
		SetupReportRoutes(auth, gormDB)

		setupBatchRoutes(auth)

		setupAIContentRoutes(auth)

		setupUserSegmentRoutes(auth)

		setupMarketingFlowRoutes(auth)

		setupCustomReportRoutes(auth)

		setupDashboardRoutes(auth, public)

		// R39-K7/K8：知识库模板与大屏模板（业务闭环 C-6 收编）。读/apply/rate
		// 登录档，模板写/publish/clone 租户 admin——分档判据见 r39_panel_routes.go 头注。
		setupR39PanelRoutes(auth, gormDB)

		setupTemplateRoutes(auth)

		setupScriptRoutes(auth)

		setupABTestRoutes(auth)

		setupTuningRoutes(auth, gormDB)

		setupAssetMarketRoutes(auth, gormDB)

		setupAssetBundleRoutes(auth, gormDB)

		setupChurnRoutes(auth)

		setupIntegrationRoutes(auth)

		setupRateQuotaRoutes(auth)

		setupPromptRoutes(auth)

		setupTypingPredictRoutes(auth)

		setupCommunityRoutes(auth, gormDB)

		setupBackupRoutes(systemAdmin, gormDB)

		setupMigrationRoutes(auth, gormDB)

		auth.POST("/upload", controller.UploadFile)

		setupToolDebugRoutes(auth)

		setupOrderDraftRoutes(auth)

		setupLTCRoutes(auth)

		app.SetupToolPermissionRoutes(auth)

		setupAIToolConfigRoutes(auth, gormDB)

		app.SetupInferenceRoutes(auth)

		// 智能体双模式运行入口：按 agent_id 读出 agent_mode 再分派。
		app.SetupAgentLifecycleRoutes(auth)

		aiAgentCtrl := controller.NewAIAgentControllerWithService(aiAgentSvcGlobal)
		aiAgentCtrl.SetSalesEngine(engine)
		aiAgentCtrl.RegisterRoutes(auth)

		channelBindingCtrl := controller.NewChannelAgentBindingControllerWithService(channelBindingSvcGlobal)
		channelBindingCtrl.RegisterRoutes(auth)

		csAgentMountCtrl := controller.NewCustomerServiceAgentControllerWithService(csAgentSvcGlobal)
		csAgentMountCtrl.RegisterRoutes(auth)

		setupCompetitorFeatureRoutes(auth, gormDB)

		setupFrontendAliases(auth, systemAdmin, r, gormDB)
	}

	webhookCtrl := controller.NewWebhookController(webhookSvc)
	webhookCtrl.SetWhatsAppCloudService(whatsappCloudSvc)
	webhookCtrl.SetDingTalkAppService(dingtalkAppSvc)
	webhookCtrl.SetFeishuService(service.NewFeishuService(repository.NewFeishuAccountRepositoryWithDB(gormDB)))
	webhookCtrl.SetSalesEngine(engine)
	webhookCtrl.SetSmartOrchestrator(orchestrator)
	webhookCtrl.SetLangResolver(langResolver)
	webhookCtrl.RegisterRoutes(r)

	webhookCtrl.SetAgentBindingService(channelBindingSvcGlobal)

	go service.ReconcileTelegramWebhooks(service.NewTelegramService(repository.NewTelegramAccountRepositoryWithDB(gormDB)))
	service.StartGateSweeper(service.NewTelegramGateService())

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
