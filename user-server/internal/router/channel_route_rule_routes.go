package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupChannelRouteRuleRoutes 入站路由规则 CRUD（specs/024 T305）。
//
// 挂在 auth 组内（JWT + 租户中间件已在链上）；租户只从 context 取。
func setupChannelRouteRuleRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	ctrl := controller.NewChannelRouteRuleController(
		service.NewChannelRouteRuleService(repository.NewChannelRouteRuleRepository(db)),
	)
	auth.GET("/channel-route-rules", middleware.RequirePermission("channel_routes.read"), ctrl.List)
	auth.POST("/channel-route-rules", middleware.RequirePermission("channel_routes.write"), ctrl.Create)
	auth.PUT("/channel-route-rules/:id", middleware.RequirePermission("channel_routes.write"), ctrl.Update)
	auth.DELETE("/channel-route-rules/:id", middleware.RequirePermission("channel_routes.write"), ctrl.Delete)
}
