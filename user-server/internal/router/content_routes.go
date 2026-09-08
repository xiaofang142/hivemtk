package router

import (
	contentctrl "hivemtk-user/internal/content/controller"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupDomainPoolRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	database := gormDB
	domainPoolRepo := repository.NewDomainPoolRepository(database)
	domainPoolSvc := service.NewDomainPoolService(database)
	healthSvc := service.NewDomainHealthService(database, domainPoolRepo)
	domainPoolCtrl := controller.NewDomainPoolController(domainPoolSvc, healthSvc)
	auth.GET("/domainpool/list", domainPoolCtrl.List)
	auth.GET("/domainpool/:id", domainPoolCtrl.GetByID)

	admin := auth.Group("/domainpool", middleware.AdminAuthMiddleware())
	{
		admin.POST("", domainPoolCtrl.Create)
		admin.PUT("/:id", domainPoolCtrl.Update)
		admin.DELETE("/:id", domainPoolCtrl.Delete)
		admin.POST("/check-domain", domainPoolCtrl.CheckDomain)
		admin.POST("/check-all", domainPoolCtrl.CheckAllDomains)
		admin.POST("/create", domainPoolCtrl.Create)
		admin.POST("/checkall", domainPoolCtrl.CheckAllDomains)
		admin.PUT("/update", domainPoolCtrl.Update)
	}
}

func setupMaterialRoutes(auth *gin.RouterGroup) {
	materialCtrl := contentctrl.NewMaterialController(contentservice.NewMaterialService())

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

func setupClueRoutes(auth *gin.RouterGroup) {
	clueCtrl := controller.NewClueController()
	auth.GET("/clue/list", clueCtrl.GetClueList)
	auth.GET("/clue/statistics", clueCtrl.GetClueStatistics)
	auth.GET("/clues/list", clueCtrl.GetClueList)
	auth.GET("/clues/statistics", clueCtrl.GetClueStatistics)
	auth.GET("/clues/type", clueCtrl.GetClueTypes)

	clueAdmin := auth.Group("", middleware.AdminAuthMiddleware())
	{
		clueAdmin.DELETE("/clue/:id", clueCtrl.DeleteClue)
		clueAdmin.POST("/clue/import", clueCtrl.ImportClues)
		clueAdmin.DELETE("/clues/delete/:id", clueCtrl.DeleteClue)
		clueAdmin.POST("/clues/import", clueCtrl.ImportClues)
	}

	clueScoreCtrl := controller.NewClueScoreController()
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
	admin := auth.Group("/recovery-queue", middleware.AdminAuthMiddleware())
	{
		admin.POST("/enqueue", ctrl.Enqueue)
		admin.POST("/:id/attempt", ctrl.MarkAttempt)
		admin.POST("/:id/recovered", ctrl.MarkRecovered)
		admin.POST("/:id/cancel", ctrl.Cancel)
	}
}
