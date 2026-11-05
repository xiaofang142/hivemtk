package router

import (
	contentctrl "hivemtk-user/internal/content/controller"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupMaterialRoutes(auth *gin.RouterGroup) {
	materialCtrl := contentctrl.NewMaterialController(contentservice.NewMaterialService(contentservice.NewMaterialDeps(dbutil.GetDB())))

	auth.GET("/material/list", materialCtrl.GetMaterialList)
	auth.POST("/material", materialCtrl.UploadMaterial)
	auth.POST("/material/upload", materialCtrl.UploadMaterial)
	auth.PUT("/material/:id/usage", materialCtrl.UpdateMaterialUsage)
	auth.POST("/material/:id/usage", materialCtrl.UpdateMaterialUsage)
	auth.GET("/material/stats", materialCtrl.GetMaterialStats)
	auth.GET("/material/categories", materialCtrl.GetMaterialCategories)
	auth.GET("/material/categories/:id", materialCtrl.GetMaterialCategoryByID)
	auth.GET("/material/selector", materialCtrl.GetMaterialSelector)
	auth.GET("/material/:id", materialCtrl.GetMaterialByID)

	materialAdmin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		materialAdmin.DELETE("/material/:id", materialCtrl.DeleteMaterial)
		materialAdmin.POST("/material/categories", materialCtrl.CreateMaterialCategory)
		materialAdmin.PUT("/material/categories/:id", materialCtrl.UpdateMaterialCategory)
		materialAdmin.DELETE("/material/categories/:id", materialCtrl.DeleteMaterialCategory)
	}
}

func setupClueRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	clueRepo := repository.NewClueRepositoryWithDB(gormDB)
	clueScoreSvc := service.NewClueScoreService()
	clueCtrl := controller.NewClueController(service.NewClueService(clueRepo, clueScoreSvc))
	auth.GET("/clue/list", clueCtrl.GetClueList)
	auth.GET("/clue/statistics", clueCtrl.GetClueStatistics)
	auth.GET("/clues/list", clueCtrl.GetClueList)
	auth.GET("/clues/statistics", clueCtrl.GetClueStatistics)
	auth.GET("/clues/type", clueCtrl.GetClueTypes)

	clueAdmin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		clueAdmin.DELETE("/clues/delete/:id", clueCtrl.DeleteClue)
		clueAdmin.POST("/clues/import", clueCtrl.ImportClues)
	}

	clueScoreCtrl := controller.NewClueScoreController(clueScoreSvc)
	auth.POST("/clue/score", middleware.RequirePermission("clues.write"), clueScoreCtrl.ScoreClue)
	auth.POST("/clue/score-all", middleware.RequirePermission("clues.write"), clueScoreCtrl.ScoreAll)
	auth.GET("/clue/score/:clue_id", clueScoreCtrl.GetByClueID)
	auth.GET("/clue/score/list", clueScoreCtrl.ListByGrade)
	auth.POST("/clue/engagement", middleware.RequirePermission("clues.write"), clueScoreCtrl.RecordEngagement)
}

func setupLeadMiningRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewLeadMiningController()
	auth.GET("/lead-mining/config", ctrl.GetConfig)
	auth.GET("/lead-mining/status", ctrl.GetStatus)
	admin := auth.Group("/lead-mining", middleware.AdminAuthMiddleware())
	admin.POST("/config", ctrl.SaveConfig)
}

func setupCustomerRFMRoutes(auth *gin.RouterGroup) {
	rfmCtrl := controller.NewCustomerRFMController()
	auth.POST("/customer-rfm/compute", rfmCtrl.ComputeForCustomer)
	auth.GET("/customer-rfm/:customer_id", rfmCtrl.GetByCustomerID)
	auth.GET("/customer-rfm/list", rfmCtrl.ListBySegment)
	auth.GET("/customer-rfm/distribution", rfmCtrl.Distribution)
	admin := auth.Group("/customer-rfm", middleware.AdminAuthMiddleware())
	admin.POST("/compute-all", rfmCtrl.ComputeAll)
}

func setupRecoveryQueueRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewRecoveryQueueController()
	auth.GET("/recovery-queue/list", ctrl.ListByStage)
	auth.GET("/recovery-queue/distribution", ctrl.Distribution)
	auth.GET("/recovery-queue/ready", ctrl.ListReadyForAttempt)

	// 写口收在 admin 档：入队/推进/完结/取消改的是"要不要继续给流失客户发消息"，
	// 与三条只读口（登录态可读）分开注册。路径与只读口同一风格：
	// 动作是末段（enqueue），带 ID 的动作是 /:id/<verb>。
	admin := auth.Group("/recovery-queue", middleware.AdminAuthMiddleware())
	{
		admin.POST("/enqueue", ctrl.Enqueue)
		admin.POST("/:id/attempt", ctrl.MarkAttempt)
		admin.POST("/:id/recovered", ctrl.MarkRecovered)
		admin.POST("/:id/cancel", ctrl.Cancel)
	}
}
