package router

import (
	knowledgectrl "hivemtk-user/internal/aiagent/knowledge/controller"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	emailsvc "hivemtk-user/internal/service/email"
	smssvc "hivemtk-user/internal/service/sms"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupPlatformRoutes(platform *gin.RouterGroup, platformCtrl *controller.PlatformController) {
	platform.GET("/dashboard", platformCtrl.GetDashboard)

	platform.GET("/merchant/list", platformCtrl.GetMerchantList)
	platform.GET("/merchant/:id/stats", platformCtrl.GetMerchantStats)

	platform.GET("/message/list", platformCtrl.GetMessageList)
	platform.GET("/message/latest", platformCtrl.GetLatestMessage)
	platform.POST("/message/send", platformCtrl.SendMessage)
	platform.POST("/message/:id/read", platformCtrl.MarkPlatformMessageRead)

	platform.GET("/user/list", platformCtrl.GetUserList)
	platform.POST("/user", platformCtrl.CreateUser)
	platform.DELETE("/user/:id", platformCtrl.DeleteUser)

	platform.GET("/stats/system", platformCtrl.GetSystemStats)
	platform.GET("/stats/overview", platformCtrl.GetPlatformStats)
	platform.GET("/stats/merchant", platformCtrl.GetPlatformMerchantStats)
}

func setupPublicRoutes(public *gin.RouterGroup, liveCodeController *controller.LiveCodeController, db *gorm.DB) {

	systemInfoCtrl := controller.NewSystemInfoController()
	public.GET("/health", systemInfoCtrl.Health)
	// 这条口有两种读者，要的字段不一样：Playground 只要 platform_enabled（决定「上架到
	// 官方商城」入口渲不渲染），而且它可能在令牌刷新完成前就发请求，匿名必须能拿到 200；
	// 「系统监控」页要的是主机名 / CPU / 内存 / 磁盘 / uptime 这台机器的画像，而它的菜单
	// 节点 systemMonitor 挂在 siteConfig（Roles=["admin"]）下。
	// 所以收口点不是「这条口要不要登录」，而是「按身份回哪一片字段」：
	// OptionalAuth 只认身份不拦人，字段裁剪在 handler 里（见 publicSystemInfo）。
	public.GET("/system/info", middleware.OptionalAuthMiddleware(), systemInfoCtrl.GetSystemInfo)

	public.POST("/auth/login", middleware.BruteForceGuard("auth.login"), controller.NewAuthController().Login)

	public.POST("/auth/mfa/verify", middleware.BruteForceGuard("auth.mfa"), controller.NewAuthController().VerifyMFALogin)

	// 自助注册：受 system_config.enable_register 开关 gate（控制器内 403），
	// BruteForceGuard 防灌号；公开站点配置供前端渲染注册入口。
	public.POST("/auth/register", middleware.BruteForceGuard("auth.register"), controller.NewAuthController().Register)
	public.GET("/public/config", controller.NewAuthController().PublicConfig)

	systemInitCtrl := controller.NewSystemInitController()
	authCtrl := controller.NewAuthController()
	public.GET("/system/init-status", systemInitCtrl.GetInitStatus)
	// 首次安装的三条写入口：公开可达是必需的（还没登录态），但它们只能在这台实例
	// 还没装完之前起作用——装完之后再来一发就是"匿名给自己造一个超管"，
	// 所以每条都挂 SetupOnlyGuard（已初始化 → 403）。
	// 门只挂在路由层不够：service.AuthService.InitAdmin 还会再核对一次库里既有超管数，
	// 这样任何人把这些前缀挂到别的组、或新增一个别名，都越不过去同一道判定。
	setupOnly := middleware.SetupOnlyGuard()
	public.POST("/system/init-admin", setupOnly, authCtrl.InitAdmin)
	public.POST("/system/init-complete", setupOnly, systemInitCtrl.InitComplete)
	public.POST("/system/create-default-admin", setupOnly, authCtrl.CreateDefaultAdmin)

	redirectCtrl := controller.NewRedirectController(
		service.NewShortLinkService(repository.NewShortLinkRepository(db), repository.NewDomainPoolRepository(db), repository.NewShortLinkAccessRepository(db)),
		service.NewDouyinCardService(db),
		service.NewKuaishouCardService(db),
		service.NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(db), service.NewShortLinkService(repository.NewShortLinkRepository(db), repository.NewDomainPoolRepository(db), repository.NewShortLinkAccessRepository(db)), service.NewDomainPoolService(repository.NewDomainPoolRepository(db), repository.NewDomainBlacklistRepository(db))),
		service.NewXianyuCardService(db),
		service.NewDouyinCardStatsService(repository.NewDouyinCardStatsRepository(db)),
		service.NewKuaishouCardStatsService(repository.NewKuaishouCardStatsRepository(db)),
		service.NewXiaohongshuCardStatsService(repository.NewXiaohongshuCardStatsRepository(db)),
		service.NewXianyuCardStatsService(db),
		service.NewTikTokCardService(),
	)
	public.GET("/s/:code", middleware.PublicCodeTenant(db, "short_links", "short_code"), redirectCtrl.RedirectShortLink)
	public.GET("/l/:code", middleware.PublicCodeTenant(db, "live_codes", "short_link"), liveCodeController.RedirectLiveCode)

	public.GET("/livecode/:id", middleware.PublicStringIDTenant(db, "live_codes"), liveCodeController.RenderLiveCodePage)
	public.POST("/livecode/:id/click", middleware.PublicStringIDTenant(db, "live_codes"), liveCodeController.RecordClick)

	deps := wirePublicDependencies(db)
	public.POST("/knowledge-merchant/external/import", deps.knowledgeMerchantCtrl.ExternalImport)

	deps.emailUnsubscribeCtrl.RegisterRoutes(public, nil)

	deps.smsUnsubscribeCtrl.RegisterRoutes(public, nil)

	deps.smsDeliveryTrackerCtrl.RegisterRoutes(public, nil)

	deps.emailOpenTrackerCtrl.RegisterRoutes(public, nil)

	// 邮件追踪像素/点击重定向：邮件客户端直连、无 JWT，故挂 public 组
	// （authed 侧指标查询在 setupEmailRoutes 注册）。此前该控制器零接线，运行时 404。
	deps.emailTrackingCtrl.RegisterRoutes(public, nil)

	// 入站消息入口：X-Ingress-Secret 是实例级凭证，区分不了租户。on 模式下
	// 调用方必须再带 X-Tenant-Code（或 ?tenant_code=）声明这批消息归谁——
	// 没声明时这里不猜，下游隔离 callback 直接 fail-closed。
	ingressGrp := public.Group("/chat/ingress", middleware.IngressSecretAuth(), middleware.PublicTenantHint(tenantCodeResolver))
	ingressGrp.POST("", deps.inboxIngressCtrl.Ingress)
}

type publicDeps struct {
	knowledgeMerchantCtrl  *knowledgectrl.KnowledgeMerchantController
	emailUnsubscribeCtrl   *controller.EmailUnsubscribeController
	smsUnsubscribeCtrl     *controller.SmsUnsubscribeController
	smsDeliveryTrackerCtrl *controller.SmsDeliveryTrackerController
	emailOpenTrackerCtrl   *controller.EmailOpenTrackerController
	emailTrackingCtrl      *controller.EmailTrackingController
	inboxIngressCtrl       *controller.InboxIngressController
}

func wirePublicDependencies(db *gorm.DB) publicDeps {
	emailUnsubscribeRepo := repository.NewEmailUnsubscribeRepository(db)
	smsUnsubscribeRepo := repository.NewSmsUnsubscribeRepository(db)
	return publicDeps{
		knowledgeMerchantCtrl:  knowledgectrl.NewKnowledgeMerchantController(),
		emailUnsubscribeCtrl:   controller.NewEmailUnsubscribeController(emailsvc.NewEmailUnsubscribeService(emailUnsubscribeRepo)),
		smsUnsubscribeCtrl:     controller.NewSmsUnsubscribeController(smssvc.NewSmsUnsubscribeService(smsUnsubscribeRepo)),
		smsDeliveryTrackerCtrl: controller.NewSmsDeliveryTrackerController(smssvc.NewSmsDeliveryTrackerService(nil, nil)),
		emailOpenTrackerCtrl:   controller.NewEmailOpenTrackerController(emailsvc.NewEmailOpenTrackerService(nil, nil)),
		emailTrackingCtrl:      controller.NewEmailTrackingController(emailsvc.NewEmailTrackingService(repository.NewEmailTrackingRepository(db))),
		inboxIngressCtrl:       controller.NewInboxIngressController(service.NewInboxIngressService()),
	}
}
