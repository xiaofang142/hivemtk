package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupMerchantConnectionRoutes(tenantAdmin *gin.RouterGroup, db *gorm.DB) {
	// No provider may be registered here until its exact live contract is verified.
	svc := service.NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), nil)
	mountMerchantConnectionRoutes(tenantAdmin, svc)
}

func mountMerchantConnectionRoutes(tenantAdmin *gin.RouterGroup, svc service.MerchantConnectionService) {
	ctrl := controller.NewMerchantConnectionController(svc)
	// 显式前缀供静态 API 契约审计解析；隐私中间件与路由验收仍校验相同路径。
	group := tenantAdmin.Group("/v1/integrations/merchant-connections")
	group.GET("", ctrl.Status)
	group.POST("/start", ctrl.Start)
	group.POST("/complete", ctrl.Complete)
	group.POST("/refresh", ctrl.Refresh)
	group.POST("/revoke", ctrl.Revoke)
	// Handle trailing slashes directly. Gin's automatic redirect logs the raw URL in debug mode.
	group.GET("/", ctrl.Status)
	group.POST("/start/", ctrl.Start)
	group.POST("/complete/", ctrl.Complete)
	group.POST("/refresh/", ctrl.Refresh)
	group.POST("/revoke/", ctrl.Revoke)
}
