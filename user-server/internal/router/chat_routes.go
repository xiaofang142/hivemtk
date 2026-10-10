package router

import (
	"context"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"
	"hivemtk-user/internal/websocket"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupChatPublicRoutes(public *gin.RouterGroup, db *gorm.DB, orchestrator *service.SmartCSOrchestrator, langResolver *translation.LangConfigResolver) {
	channelSvc := service.MustNewChatChannelService(db)
	agentBindingSvc := service.NewChannelAgentBindingService()
	visitorSvc := service.NewVisitorChatService(context.Background(), db, channelSvc, orchestrator, agentBindingSvc)

	chatPublic := public.Group("/chat/public")
	chatPublic.Use(middleware.AppKeyResolve(chatChannelResolver{svc: channelSvc}))
	chatPublic.Use(middleware.LangResolverMiddleware(langResolver))
	chatPublic.Use(middleware.VisitorRateLimitMiddleware())

	chatPublic.Use(middleware.SanitizeMiddleware())

	ctrl := controller.NewChatPublicController(visitorSvc, channelSvc)

	chatPublic.GET("/channel/:app_key/info", ctrl.GetChannelInfoByAppKey)

	chatPublic.POST("/sessions", ctrl.OpenSession)
	chatPublic.GET("/sessions/active", ctrl.GetActiveSession)
	chatPublic.GET("/sessions/recent-closed", ctrl.GetRecentClosedSessions)
	chatPublic.GET("/sessions/:session_id/messages", ctrl.GetMessages)
	chatPublic.GET("/sessions/:session_id/offline-messages", ctrl.GetOfflineMessages)
	chatPublic.POST("/sessions/:session_id/messages", ctrl.SendMessage)
	chatPublic.POST("/sessions/:session_id/transfer", ctrl.RequestHumanTransfer)
	chatPublic.POST("/sessions/:session_id/close", ctrl.CloseSession)
	chatPublic.POST("/sessions/:session_id/rate", ctrl.RateSession)
	// 访客从 recent-closed 列表点开更早的会话时，先换回该会话的 visitor_token，
	// 否则这条会话上的 history/offline/close/rate 会全部 403。
	chatPublic.POST("/sessions/:session_id/token", ctrl.ExchangeSessionToken)

	chatPublic.GET("/agents/available", ctrl.CountAvailableAgents)

	chatPublic.GET("/upload-token", ctrl.GetUploadToken)
}

func setupChatPublicWebSocket(r *gin.Engine, langResolver *translation.LangConfigResolver) {
	visitorWS := websocket.NewVisitorWSHandler(dbutil.GetDB())
	visitorWS.SetLangResolver(langResolver)
	r.GET("/api/ws/visitor", visitorWS.HandleVisitorWebSocket)
}

func setupChatChannelAdminRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	channelSvc := service.MustNewChatChannelService(db)
	ctrl := controller.NewChatChannelController(channelSvc)

	auth.GET("/chat-channels", ctrl.List)
	auth.GET("/chat-channels/:channel_id", ctrl.Get)
	admin := auth.Group("/chat-channels", middleware.AdminAuthMiddleware())
	{
		admin.POST("", ctrl.Create)
		admin.PUT("/:channel_id", ctrl.Update)
		admin.DELETE("/:channel_id", ctrl.Delete)
		admin.POST("/:channel_id/rotate-key", ctrl.RotateAppKey)
		admin.POST("/:channel_id/reset-secret", ctrl.ResetAppSecret)
	}
}

func setupWeComRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	wecomCtrl := controller.NewWeComController(service.NewWeComServiceWithDB(gormDB))

	auth.GET("/wecom/accounts", wecomCtrl.GetAccountList)
	auth.GET("/wecom/accounts/:id", wecomCtrl.GetAccountByID)
	auth.GET("/wecom/customers", wecomCtrl.GetCustomerList)
	auth.GET("/wecom/groups", wecomCtrl.GetGroupList)
	auth.GET("/wecom/messages", wecomCtrl.GetMessageList)
	auth.GET("/wecom/tags", wecomCtrl.GetTagList)
	admin := auth.Group("/wecom", middleware.AdminAuthMiddleware())
	{
		admin.POST("/accounts", wecomCtrl.CreateAccount)
		admin.PUT("/accounts/:id", wecomCtrl.UpdateAccount)
		admin.DELETE("/accounts/:id", wecomCtrl.DeleteAccount)
		admin.POST("/accounts/:id/sync-customers", wecomCtrl.SyncCustomers)
		admin.POST("/accounts/:id/sync-groups", wecomCtrl.SyncGroups)
		admin.POST("/accounts/:id/send-message", wecomCtrl.SendMessage)
		admin.POST("/accounts/:id/refresh", wecomCtrl.RefreshAccount)
		admin.POST("/accounts/:id/sync-tags", wecomCtrl.SyncTags)
	}
}

func setupCardShareRoutes(r *gin.Engine, gormDB *gorm.DB) {
	douyinCardCtrl := controller.NewDouyinCardController(service.NewDouyinCardService(gormDB))
	r.GET("/share/douyin/:id", douyinCardCtrl.SharePage)

	kuaishouCardCtrl := controller.NewKuaishouCardController(service.NewKuaishouCardService(gormDB))
	r.GET("/share/kuaishou/:id", kuaishouCardCtrl.SharePage)

	xiaohongshuCardCtrl := controller.NewXiaohongshuCardController(service.NewXiaohongshuCardService(gormDB))
	r.GET("/share/xiaohongshu/:id", xiaohongshuCardCtrl.SharePage)

	xianyuCardCtrl := controller.NewXianyuCardController(service.NewXianyuCardService(gormDB), service.NewXianyuCardStatsService(gormDB))
	r.GET("/share/xianyu/:id", xianyuCardCtrl.SharePage)
}
