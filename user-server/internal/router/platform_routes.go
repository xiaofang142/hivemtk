package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupWhatsappRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	whatsappCtrl := controller.NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(gormDB)))
	// 群发控制器的依赖全部在装配层构造（controller 不 import repository / gorm ——
	// depguard 的 controller-layer 清单，且那是五层架构的破口）。
	whatsappGroupMsgCtrl := controller.NewGroupMessagingController(
		whatsappCtrl.GetService(),
		service.NewClueServiceDefault(service.NewClueScoreService()),
		service.NewMessageQueueService(),
		service.NewWhatsAppTemplateService(),
	)

	auth.GET("/whatsapp/accounts", whatsappCtrl.ListAccounts)
	auth.GET("/whatsapp/login-status", whatsappCtrl.LoginStatus)
	auth.GET("/whatsapp/drafts", whatsappCtrl.ListDrafts)
	auth.GET("/whatsapp/jobs", whatsappCtrl.ListJobs)
	auth.GET("/whatsapp/jobs/:id", whatsappCtrl.GetJob)
	// 进度读侧跟列表/详情同档：一个角色能列出这批任务、能读到它的详情，
	// 就应当能读到它的进度。挂在 admin 组里时批量矩阵页的进度条对非超管恒空转
	// （POST /jobs 仍是 admin，"能不能发起"没有跟着放开）。
	auth.GET("/whatsapp/jobs/:id/progress", whatsappGroupMsgCtrl.GetJobProgress)
	auth.GET("/whatsapp/accounts/:id/login/status", whatsappCtrl.LoginStatus)
	auth.GET("/whatsapp/lead-groups", whatsappGroupMsgCtrl.GetLeadGroups)
	auth.GET("/whatsapp/group-messaging/status/:queue_id", whatsappGroupMsgCtrl.GetMessageStatus)
	auth.GET("/whatsapp/group-messaging/records", whatsappGroupMsgCtrl.GetSendRecords)
	auth.GET("/whatsapp/templates", whatsappGroupMsgCtrl.GetTemplates)
	auth.GET("/whatsapp/templates/:id", whatsappGroupMsgCtrl.GetTemplateByID)

	admin := auth.Group("/whatsapp", middleware.AdminAuthMiddleware())
	{
		admin.POST("/accounts", whatsappCtrl.CreateAccount)
		admin.PUT("/accounts/:id", whatsappCtrl.UpdateAccount)
		admin.DELETE("/accounts/:id", whatsappCtrl.DeleteAccount)
		admin.POST("/accounts/:id/login/start", whatsappCtrl.StartLogin)
		admin.POST("/start-login", whatsappCtrl.StartLogin)
		admin.POST("/drafts", whatsappCtrl.CreateDraft)
		admin.PUT("/drafts/:id", whatsappCtrl.UpdateDraft)
		admin.DELETE("/drafts/:id", whatsappCtrl.DeleteDraft)
		admin.POST("/jobs", whatsappCtrl.CreateJob)
		admin.DELETE("/jobs/:id", whatsappCtrl.DeleteJob)
		admin.POST("/group-messaging/send", whatsappGroupMsgCtrl.SelectGroupAndSendMessage)
		admin.POST("/bulk-send", whatsappGroupMsgCtrl.BulkSend)
		admin.POST("/templates", whatsappGroupMsgCtrl.CreateTemplate)
		admin.PUT("/templates/:id", whatsappGroupMsgCtrl.UpdateTemplate)
		admin.DELETE("/templates/:id", whatsappGroupMsgCtrl.DeleteTemplate)
	}
}

func setupTelegramRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	telegramAccountCtrl := controller.NewTelegramAccountController(
		service.NewTelegramService(repository.NewTelegramAccountRepositoryWithDB(gormDB)),
	)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	telegramAccountCtrl.RegisterRoutes(admin)
	gateCtrl := controller.NewTelegramGateController(service.NewTelegramGateService())
	gateCtrl.RegisterRoutes(admin)
}

func setupFeishuRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	feishuCtrl := controller.NewFeishuAccountController(
		service.NewFeishuService(repository.NewFeishuAccountRepositoryWithDB(gormDB)),
		service.NewFeishuIntegrationService(
			repository.NewFeishuAccountRepositoryWithDB(gormDB),
			repository.NewFeishuMessageRepositoryWithDB(gormDB),
		),
	)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	feishuCtrl.RegisterRoutes(admin)
}

func setupWhatsAppCloudRoutes(auth *gin.RouterGroup, whatsappCloudSvc *service.WhatsAppCloudService, gormDB *gorm.DB) {
	waCtrl := controller.NewWhatsAppCloudAccountController(whatsappCloudSvc,
		service.NewWhatsAppCloudIntegrationService(repository.NewWhatsAppCloudAccountRepositoryWithDB(gormDB)))
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	waCtrl.RegisterRoutes(admin)
}

func setupDingTalkAppRoutes(auth *gin.RouterGroup, dingtalkAppSvc *service.DingTalkAppService) {
	dtCtrl := controller.NewDingTalkAppAccountController(dingtalkAppSvc)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	dtCtrl.RegisterRoutes(admin)
}

func setupWechatRoutes(auth *gin.RouterGroup, gormDB *gorm.DB, ingressSvc *service.InboxIngressService) {
	wechatCtrl := controller.NewWechatController(service.NewWechatService())
	if ingressSvc != nil {
		wechatCtrl.SetIngressSvc(ingressSvc)
	}
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	wechatCtrl.RegisterRoutes(admin)
}

func setupWechatWebhookRoutes(r *gin.RouterGroup, gormDB *gorm.DB, ingressSvc *service.InboxIngressService) {
	wechatCtrl := controller.NewWechatController(service.NewWechatService())
	if ingressSvc != nil {
		wechatCtrl.SetIngressSvc(ingressSvc)
	}
	wechatCtrl.RegisterWebhookRoutes(r)
}

func setupTiktokRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	tiktokCardCtrl := controller.NewTikTokCardController(
		cardsvc.NewTikTokCardService(),
	)
	tiktokCardCtrl.RegisterRoutes(auth)
}

func setupQQRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	qqAccountCtrl := controller.NewQQAccountController(service.NewQQService())
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	qqAccountCtrl.RegisterRoutes(admin)
}
