package router

import (
	knowledgectrl "hivemtk-user/internal/aiagent/knowledge/controller"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

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
	public.GET("/system/info", systemInfoCtrl.GetSystemInfo)

	public.POST("/auth/login", middleware.BruteForceGuard("auth.login"), controller.NewAuthController().Login)

	public.POST("/auth/mfa/verify", middleware.BruteForceGuard("auth.mfa"), controller.NewAuthController().VerifyMFALogin)

	systemInitCtrl := controller.NewSystemInitController()
	authCtrl := controller.NewAuthController()
	public.GET("/system/init-status", systemInitCtrl.GetInitStatus)
	public.POST("/system/init-admin", authCtrl.InitAdmin)
	public.POST("/system/init-complete", systemInitCtrl.InitComplete)
	public.POST("/system/create-default-admin", authCtrl.CreateDefaultAdmin)

	redirectCtrl := controller.NewRedirectController(
		service.NewShortLinkService(db),
		service.NewDouyinCardService(db),
		service.NewKuaishouCardService(db),
		service.NewXiaohongshuCardService(db),
		service.NewXianyuCardService(db),
		service.NewDouyinCardStatsService(db),
		service.NewKuaishouCardStatsService(db),
		service.NewXiaohongshuCardStatsService(db),
		service.NewXianyuCardStatsService(db),
		service.NewTikTokCardServiceWithDB(db),
	)
	public.GET("/s/:code", redirectCtrl.RedirectShortLink)
	public.GET("/l/:code", liveCodeController.RedirectLiveCode)

	public.GET("/livecode/:id", liveCodeController.RenderLiveCodePage)
	public.POST("/livecode/:id/click", liveCodeController.RecordClick)

	deps := wirePublicDependencies(db)
	public.POST("/knowledge-merchant/external/import", deps.knowledgeMerchantCtrl.ExternalImport)

	deps.emailUnsubscribeCtrl.RegisterRoutes(public, nil)

	deps.smsUnsubscribeCtrl.RegisterRoutes(public, nil)

	deps.smsDeliveryTrackerCtrl.RegisterRoutes(public, nil)

	deps.emailOpenTrackerCtrl.RegisterRoutes(public, nil)

	ingressGrp := public.Group("/chat/ingress", middleware.IngressSecretAuth())
	ingressGrp.POST("", deps.inboxIngressCtrl.Ingress)
}

type publicDeps struct {
	knowledgeMerchantCtrl  *knowledgectrl.KnowledgeMerchantController
	emailUnsubscribeCtrl   *controller.EmailUnsubscribeController
	smsUnsubscribeCtrl     *controller.SmsUnsubscribeController
	smsDeliveryTrackerCtrl *controller.SmsDeliveryTrackerController
	emailOpenTrackerCtrl   *controller.EmailOpenTrackerController
	inboxIngressCtrl       *controller.InboxIngressController
}

func wirePublicDependencies(db *gorm.DB) publicDeps {
	emailUnsubscribeRepo := repository.NewEmailUnsubscribeRepository(db)
	smsUnsubscribeRepo := repository.NewSmsUnsubscribeRepository(db)
	return publicDeps{
		knowledgeMerchantCtrl:  knowledgectrl.NewKnowledgeMerchantController(),
		emailUnsubscribeCtrl:   controller.NewEmailUnsubscribeController(service.NewEmailUnsubscribeService(emailUnsubscribeRepo)),
		smsUnsubscribeCtrl:     controller.NewSmsUnsubscribeController(service.NewSmsUnsubscribeService(smsUnsubscribeRepo)),
		smsDeliveryTrackerCtrl: controller.NewSmsDeliveryTrackerController(service.NewSmsDeliveryTrackerService(db, nil, nil)),
		emailOpenTrackerCtrl:   controller.NewEmailOpenTrackerController(service.NewEmailOpenTrackerService(nil, nil)),
		inboxIngressCtrl:       controller.NewInboxIngressController(service.NewInboxIngressService()),
	}
}
