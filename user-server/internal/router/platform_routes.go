package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupWhatsappRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	whatsappCtrl := controller.NewWhatsappController()
	whatsappGroupMsgCtrl := controller.NewGroupMessagingController(
		whatsappCtrl.GetService(),
		service.NewClueService(),
		service.NewMessageQueueService(gormDB),
		service.NewWhatsAppTemplateService(gormDB),
	)

	auth.GET("/whatsapp/accounts", whatsappCtrl.ListAccounts)
	auth.GET("/whatsapp/login-status", whatsappCtrl.LoginStatus)
	auth.GET("/whatsapp/drafts", whatsappCtrl.ListDrafts)
	auth.GET("/whatsapp/jobs", whatsappCtrl.ListJobs)
	auth.GET("/whatsapp/jobs/:id", whatsappCtrl.GetJob)
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
		admin.POST("/templates", whatsappGroupMsgCtrl.CreateTemplate)
		admin.PUT("/templates/:id", whatsappGroupMsgCtrl.UpdateTemplate)
		admin.DELETE("/templates/:id", whatsappGroupMsgCtrl.DeleteTemplate)
	}
}

func setupTelegramRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	telegramAccountCtrl := controller.NewTelegramAccountController(service.NewTelegramService(gormDB))
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	telegramAccountCtrl.RegisterRoutes(admin)
}

func setupFeishuRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	feishuCtrl := controller.NewFeishuAccountController(service.NewFeishuService(gormDB), service.NewFeishuIntegrationService(gormDB))
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	feishuCtrl.RegisterRoutes(admin)
}

func setupWhatsAppCloudRoutes(auth *gin.RouterGroup, whatsappCloudSvc *service.WhatsAppCloudService, gormDB *gorm.DB) {
	waCtrl := controller.NewWhatsAppCloudAccountController(whatsappCloudSvc, service.NewWhatsAppCloudIntegrationService(gormDB))
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	waCtrl.RegisterRoutes(admin)
}

func setupDingTalkAppRoutes(auth *gin.RouterGroup, dingtalkAppSvc *service.DingTalkAppService) {
	dtCtrl := controller.NewDingTalkAppAccountController(dingtalkAppSvc)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	dtCtrl.RegisterRoutes(admin)
}

func setupWechatRoutes(auth *gin.RouterGroup, gormDB *gorm.DB, ingressSvc *service.InboxIngressService) {
	wechatCtrl := controller.NewWechatController(service.NewWechatService(gormDB))
	if ingressSvc != nil {
		wechatCtrl.SetIngressSvc(ingressSvc)
	}
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	wechatCtrl.RegisterRoutes(admin)
}

func setupWechatWebhookRoutes(r *gin.RouterGroup, gormDB *gorm.DB, ingressSvc *service.InboxIngressService) {
	wechatCtrl := controller.NewWechatController(service.NewWechatService(gormDB))
	if ingressSvc != nil {
		wechatCtrl.SetIngressSvc(ingressSvc)
	}
	wechatCtrl.RegisterWebhookRoutes(r)
}

func setupTiktokRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	tiktokCardCtrl := controller.NewTikTokCardController(
		service.NewTikTokCardServiceWithDB(gormDB),
	)
	tiktokCardCtrl.RegisterRoutes(auth)
}

func setupQQRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	qqAccountCtrl := controller.NewQQAccountController(service.NewQQService(gormDB))
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	qqAccountCtrl.RegisterRoutes(admin)
}
