package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAuthRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	authCtrl := controller.NewAuthController()
	auth.POST("/auth/refresh-token", authCtrl.RefreshToken)
	auth.POST("/auth/logout", authCtrl.Logout)
	auth.GET("/auth/current-user", authCtrl.GetCurrentUser)
	auth.POST("/auth/change-password", authCtrl.ChangePassword)

	auth.POST("/auth/mfa/setup", authCtrl.SetupMFA)
	auth.POST("/auth/mfa/confirm", authCtrl.ConfirmMFASetup)
	auth.POST("/auth/mfa/disable", authCtrl.DisableMFA)
	auth.GET("/auth/mfa/status", authCtrl.GetMFAStatus)

	auth.POST("/auth/mfa/backup-codes", authCtrl.GenerateBackupCodes)

	auth.GET("/auth/login-events", authCtrl.ListLoginEvents)
	auth.GET("/auth/security-alerts", authCtrl.ListSecurityAlerts)
	auth.POST("/auth/security-alerts/:id/resolve", authCtrl.ResolveSecurityAlert)

	anomalyCtrl := controller.NewAnomalyLoginDetectorController()
	auth.GET("/auth/anomaly/login-events", anomalyCtrl.ListLoginEvents)
	auth.GET("/auth/anomaly/alerts", anomalyCtrl.ListAlerts)
	auth.POST("/auth/anomaly/alerts/:id/resolve", anomalyCtrl.ResolveAlert)
	auth.POST("/auth/anomaly/alerts/:id/ignore", anomalyCtrl.IgnoreAlert)

	auth.GET("/auth/password-policy", authCtrl.GetPasswordPolicy)
	auth.PUT("/auth/password-policy", authCtrl.SavePasswordPolicy)

	notifCtrl := controller.NewNotificationController(service.NewNotificationService(gormDB))
	auth.GET("/auth/notifications", notifCtrl.List)
	auth.POST("/auth/notifications/:id/read", notifCtrl.MarkRead)
	auth.POST("/auth/notifications/read-all", notifCtrl.MarkAllRead)
	auth.GET("/auth/notifications/unread-count", notifCtrl.UnreadCount)

	auth.GET("/notifications", notifCtrl.List)
	auth.GET("/notifications/unread-count", notifCtrl.UnreadCount)
	auth.POST("/notifications/:id/read", notifCtrl.MarkRead)
	auth.POST("/notifications/read-all", notifCtrl.MarkAllRead)

}

func setupUserRoutes(auth *gin.RouterGroup) {
	userCtrl := controller.NewSystemUserController()
	auth.GET("/user/list", userCtrl.GetUsers)
	auth.GET("/users", userCtrl.GetUsers)
	auth.GET("/users/search", userCtrl.SearchUsers)
	auth.GET("/user/:id", userCtrl.GetUser)
	auth.GET("/users/:id", userCtrl.GetUser)

	admin := auth.Group("")
	admin.Use(middleware.AdminAuthMiddleware())
	{
		admin.POST("/user", userCtrl.CreateUser)
		admin.POST("/users", userCtrl.CreateUser)
		admin.PUT("/user/:id", userCtrl.UpdateUser)
		admin.PUT("/users/:id", userCtrl.UpdateUser)
		admin.DELETE("/user/:id", userCtrl.DeleteUser)
		admin.DELETE("/users/:id", userCtrl.DeleteUser)
		admin.PUT("/user/:id/password", userCtrl.ResetPassword)
		admin.PUT("/users/:id/password", userCtrl.ResetPassword)
	}
}

func setupAlertRoutes(auth *gin.RouterGroup) {
	alertCtrl := controller.NewAlertRuleController()

	auth.GET("/alerts/rules", alertCtrl.List)
	auth.GET("/alerts/rules/:id", alertCtrl.GetByID)
	auth.GET("/alerts/histories", alertCtrl.ListHistory)

	auth.GET("/monitor/alerts/unread", alertCtrl.Unread)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/alerts/rules", alertCtrl.Create)
		admin.PUT("/alerts/rules/:id", alertCtrl.Update)
		admin.DELETE("/alerts/rules/:id", alertCtrl.Delete)
		admin.PUT("/alerts/rules/status", alertCtrl.SetStatus)
		admin.POST("/alerts/histories/resolve", alertCtrl.ResolveHistory)
	}
}

func setupAccountRoutes(auth *gin.RouterGroup) {
	accountCtrl := controller.NewAccountController()
	auth.GET("/account/list", accountCtrl.GetAccounts)
	auth.GET("/account/:id", accountCtrl.GetAccount)
	auth.GET("/accounts/list", accountCtrl.GetAccounts)
	admin := auth.Group("", middleware.AdminAuthMiddleware())
	admin.POST("/account", accountCtrl.CreateAccount)
	admin.PUT("/account/:id", accountCtrl.UpdateAccount)
	admin.DELETE("/account/:id", accountCtrl.DeleteAccount)
	admin.POST("/accounts/create", accountCtrl.CreateAccount)
	admin.PUT("/accounts/update/:id", accountCtrl.UpdateAccount)
	admin.DELETE("/accounts/delete/:id", accountCtrl.DeleteAccount)
}

func setupShortLinkRoutes(auth *gin.RouterGroup, public *gin.RouterGroup, gormDB *gorm.DB) {
	shortLinkCtrl := controller.NewShortLinkController(service.NewShortLinkService(gormDB))
	shortLinkStatsCtrl := controller.NewShortLinkStatsController(service.NewShortLinkService(gormDB))

	auth.GET("/short-link/list", shortLinkCtrl.GetList)
	auth.GET("/short-link/:id", shortLinkCtrl.GetByID)
	auth.GET("/short-link/:id/stats", shortLinkStatsCtrl.GetStats)
	auth.GET("/short-link/all-stats", shortLinkStatsCtrl.GetAllStats)
	auth.GET("/shortlink/list", shortLinkCtrl.GetList)
	auth.GET("/shortlink/:id", shortLinkCtrl.GetByID)
	auth.GET("/shortlink/:id/stats", shortLinkStatsCtrl.GetStats)
	auth.GET("/shortlink/stats/all", shortLinkStatsCtrl.GetAllStats)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/short-link", shortLinkCtrl.Create)
		admin.PUT("/short-link/:id", shortLinkCtrl.Update)
		admin.DELETE("/short-link/:id", shortLinkCtrl.Delete)
		admin.POST("/shortlink/create", shortLinkCtrl.Create)
		admin.PUT("/shortlink/update", shortLinkCtrl.Update)
		admin.DELETE("/shortlink/delete/:id", shortLinkCtrl.Delete)
		admin.POST("/shortlink/generate", shortLinkCtrl.Create)
		admin.POST("/shortlink/:id/share", shortLinkStatsCtrl.ShareShortLink)
	}
	public.POST("/short-link/access", shortLinkCtrl.AccessShortLink)
	public.POST("/shortlink/access", shortLinkCtrl.AccessShortLink)
}

func setupLiveCodeRoutes(auth *gin.RouterGroup, liveCodeController *controller.LiveCodeController) {

	auth.GET("/live-code/list", liveCodeController.GetList)
	auth.GET("/live-codes/list", liveCodeController.GetList)
	auth.GET("/live-code/:id", liveCodeController.GetByID)
	auth.GET("/live-code/:id/stats", liveCodeController.GetStats)
	auth.GET("/live-code/:id/qr-codes", liveCodeController.GetQRCodes)
	auth.GET("/live-code/:id/qr-stats", liveCodeController.GetQRStats)
	auth.GET("/live-codes/:id", liveCodeController.GetByID)
	auth.GET("/live-codes/:id/stats", liveCodeController.GetStats)
	auth.GET("/live-codes/:id/qrcodes", liveCodeController.GetQRCodes)
	auth.GET("/live-codes/qrcodes/:qr_id/stats", liveCodeController.GetQRStats)

	admin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		admin.POST("/live-code", liveCodeController.Create)
		admin.PUT("/live-code/:id", liveCodeController.Update)
		admin.DELETE("/live-code/:id", liveCodeController.Delete)
		admin.POST("/live-code/:id/qr-code", liveCodeController.GenerateQRCode)
		admin.POST("/live-code/:id/share", liveCodeController.Share)
		admin.DELETE("/live-code/:id/qr-codes/:qr_id", liveCodeController.DeleteLiveCodeQR)
		admin.PUT("/live-code/:id/qr-codes/:qr_id", liveCodeController.UpdateLiveCodeQR)
		admin.POST("/live-codes/create", liveCodeController.Create)
		admin.PUT("/live-codes/:id/update", liveCodeController.Update)
		admin.DELETE("/live-codes/:id/delete", liveCodeController.Delete)
		admin.POST("/live-codes/:id/qrcodes/create", liveCodeController.GenerateQRCode)
		admin.POST("/live-codes/:id/share", liveCodeController.Share)
		admin.DELETE("/live-codes/qrcodes/:qr_id/delete", liveCodeController.DeleteLiveCodeQR)
		admin.PUT("/live-codes/qrcodes/:qr_id/update", liveCodeController.UpdateLiveCodeQR)
	}
}

func setupEmailRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	emailListCtrl := controller.NewEmailListController()
	emailSmtpCtrl := controller.NewEmailSmtpController()
	emailDraftCtrl := controller.NewEmailDraftController()
	emailJobsCtrl := controller.NewEmailJobsController()
	emailSendCtrl := controller.NewEmailSendController()

	emailAdmin := auth.Group("", middleware.AdminAuthMiddleware())

	auth.GET("/email/list", emailListCtrl.GetEmailListList)
	auth.GET("/email/list/:id", emailListCtrl.GetEmailListDetail)
	emailAdmin.POST("/email/list", emailListCtrl.CreateEmailList)
	emailAdmin.PUT("/email/list/:id", emailListCtrl.UpdateEmailList)
	emailAdmin.DELETE("/email/list/:id", emailListCtrl.DeleteEmailList)
	emailAdmin.POST("/email/list/:id/trace", emailListCtrl.TraceEmail)

	auth.GET("/email/smtp", emailSmtpCtrl.GetEmailSmtpList)
	auth.GET("/email/smtp/:id", emailSmtpCtrl.GetEmailSmtp)
	emailAdmin.POST("/email/smtp", emailSmtpCtrl.CreateEmailSmtp)
	emailAdmin.PUT("/email/smtp/:id", emailSmtpCtrl.UpdateEmailSmtp)
	emailAdmin.DELETE("/email/smtp/:id", emailSmtpCtrl.DeleteEmailSmtp)

	auth.GET("/email/drafts", emailDraftCtrl.GetEmailDraftList)
	auth.POST("/email/drafts", emailDraftCtrl.CreateEmailDraft)
	auth.GET("/email/drafts/:id", emailDraftCtrl.GetEmailDraftDetail)
	auth.PUT("/email/drafts/:id", emailDraftCtrl.UpdateEmailDraft)
	auth.DELETE("/email/drafts/:id", emailDraftCtrl.DeleteEmailDraft)

	auth.GET("/email/jobs", emailJobsCtrl.GetEmailJobsList)
	emailAdmin.POST("/email/jobs", emailJobsCtrl.CreateEmailJobs)
	emailAdmin.DELETE("/email/jobs/:id", emailJobsCtrl.DeleteEmailJobs)
	auth.GET("/email/jobs/:id", emailJobsCtrl.GetEmailJobsDetail)

	emailAdmin.POST("/email/send", emailSendCtrl.SendEmail)

	emailUnsubscribeRepo := repository.NewEmailUnsubscribeRepository(gormDB)
	emailUnsubscribeCtrl := controller.NewEmailUnsubscribeController(
		service.NewEmailUnsubscribeService(emailUnsubscribeRepo),
	)
	emailUnsubscribeCtrl.RegisterRoutes(nil, auth)

	emailOpenTrackerCtrl := controller.NewEmailOpenTrackerController(
		service.NewEmailOpenTrackerService(nil, nil),
	)
	emailOpenTrackerCtrl.RegisterRoutes(nil, auth)

	// 邮件追踪：任务指标 / 事件列表 / 区间聚合（后台管理，需 JWT）。
	// 此前 NewEmailTrackingController 全树零调用方，3 条 /email/track/* 鉴权路由
	// 从未注册（运行时 404）；公开侧像素/点击由 setupPublicRoutes 挂在 public 组。
	emailTrackingRepo := repository.NewEmailTrackingRepository(gormDB)
	emailTrackingCtrl := controller.NewEmailTrackingController(
		service.NewEmailTrackingService(emailTrackingRepo),
	)
	emailTrackingCtrl.RegisterRoutes(nil, auth)
}

func setupSmsRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	smsRepo := repository.NewSmsRepository()
	smsCtrl := controller.NewSmsController(service.NewSmsService(smsRepo))
	smsCtrl.RegisterRoutes(auth)

	smsUnsubscribeRepo := repository.NewSmsUnsubscribeRepository(gormDB)
	smsUnsubscribeCtrl := controller.NewSmsUnsubscribeController(
		service.NewSmsUnsubscribeService(smsUnsubscribeRepo),
	)
	smsUnsubscribeCtrl.RegisterRoutes(nil, auth)

	smsDeliveryTrackerCtrl := controller.NewSmsDeliveryTrackerController(
		service.NewSmsDeliveryTrackerService(gormDB, nil, nil),
	)
	smsDeliveryTrackerCtrl.RegisterRoutes(nil, auth)
}
