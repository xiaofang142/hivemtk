package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// setupLeadRoutes US2 线索管理路由（T044）。
// 仅做 URL→Controller 映射 + 依赖装配：通知出口在装配点用 setter 注入，无内联闭包。
// 写口收在 AdminAuth 档；读口沿用 auth 组，数据范围由 LeadScopedListService 强制过滤。
func setupLeadRoutes(auth *gin.RouterGroup) {
	archiveSvc := service.NewLeadClueArchiveService()
	archiveSvc.SetNotifier(service.NewLeadNotifyService())

	ctl := controller.NewLeadController(
		service.NewLeadRuleService(),
		service.NewLeadEvidenceService(),
		service.NewLeadScopedListService(),
		archiveSvc,
		service.NewLeadMiningNotifyRouteService(),
	)

	g := auth.Group("/manage/leads")
	{
		g.GET("/rules", ctl.ListRules)
		g.GET("/notify-routes", ctl.ListNotifyRoutes)
		g.GET("/messages", ctl.ListScopedMessages)
		g.GET("/customers", ctl.ListScopedCustomers)
		g.GET("/judgements/:judgement_id/hits", ctl.GetJudgementHits)
	}

	admin := auth.Group("/manage/leads", middleware.AdminAuthMiddleware())
	{
		admin.POST("/rules", ctl.SaveRule)
		admin.DELETE("/rules/:id", ctl.DeleteRule)
		admin.POST("/notify-routes", ctl.CreateNotifyRoute)
		admin.DELETE("/notify-routes/:route_id", ctl.DeleteNotifyRoute)
		admin.POST("/archive", ctl.Archive)
	}
}
