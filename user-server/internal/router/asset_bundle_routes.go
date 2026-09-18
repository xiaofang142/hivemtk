package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAssetBundleRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	assetBundleRepo := repository.NewAssetBundleRepository(gormDB)
	versionLogRepo := repository.NewAssetBundleVersionLogRepository(gormDB)
	assetBundleSvc := service.NewAssetBundleService(assetBundleRepo, versionLogRepo)
	localAssetRepo := repository.NewLocalAssetRepository(gormDB)
	localAssetDataRepo := repository.NewLocalAssetDataRepository(gormDB)
	localAssetSyncLogRepo := repository.NewLocalAssetSyncLogRepository(gormDB)
	localAssetSvc := service.NewLocalAssetService(localAssetRepo, localAssetDataRepo, localAssetSyncLogRepo, platform.NewPlatformAPIClient(), gormDB)
	assetBundleSvc.SetLocalLoader(localAssetSvc)
	service.SetAssetBundleResolver(assetBundleSvc)
	ctrl := controller.NewAssetBundleController(assetBundleSvc)

	for _, g := range []*gin.RouterGroup{
		auth.Group("/v1"),
		auth,
	} {
		ctrl.Register(g)
	}
}

func setupAssetMarketRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	gdb := gormDB
	ar := repository.NewLocalAssetRepository(gdb)
	dr := repository.NewLocalAssetDataRepository(gdb)
	sr := repository.NewLocalAssetSyncLogRepository(gdb)
	pc := platform.NewPlatformAPIClient()
	h := controller.NewAssetMarketController(
		service.NewLocalAssetService(ar, dr, sr, pc, gdb),
		service.NewAssetMarketService(pc),
		service.NewAIAgentService(),
	)

	groups := []*gin.RouterGroup{
		auth.Group("/v1/asset-market"),
	}
	for _, market := range groups {
		market.GET("/list", h.ListMarket)
		market.GET("/detail/:asset_id", h.MarketDetail)
		market.POST("/purchase", h.Purchase)
		market.POST("/sync", h.Sync)
		market.POST("/report-usage", h.ReportUsage)
		market.GET("/my-purchases", h.MyPurchases)
		market.POST("/bind", h.BindToAgent)
	}

	localGroups := []*gin.RouterGroup{
		auth.Group("/v1/local-assets"),
	}
	for _, local := range localGroups {
		local.GET("", h.ListLocal)
		local.GET("/sync-log", h.SyncLog)
		local.GET("/:id", h.GetLocal)
		local.POST("", h.CreateLocal)
		local.PUT("/:id", h.UpdateLocal)
		local.DELETE("/:id", h.DeleteLocal)
		local.PUT("/:id/toggle-active", h.ToggleActive)
	}
}
