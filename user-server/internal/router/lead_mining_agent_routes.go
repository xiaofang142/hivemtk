package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupLeadMiningAgentRoutes 「线索发现智能体」独立注册路由（FR-026，T047）。
// 仅做 URL→Controller 映射 + 构造注入，无内联闭包；写口收在 AdminAuth 档。
func setupLeadMiningAgentRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {
	ctl := controller.NewLeadMiningAgentController(service.NewLeadMiningAgentService(gormDB))

	g := auth.Group("/manage/lead-mining-agents")
	{
		g.GET("", ctl.List)
		g.GET("/:id", ctl.Get)
	}
	admin := auth.Group("/manage/lead-mining-agents", middleware.AdminAuthMiddleware())
	{
		admin.POST("", ctl.Create)
		admin.PUT("/:id", ctl.Update)
		admin.DELETE("/:id", ctl.Delete)
	}
}
