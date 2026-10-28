package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupReachDeferredRoutes D02：主动触达延迟出站的管理口（查询/取消）。
// 挂在 auth 组：租户隔离由 callback 注入，取消是写操作（仅 pending 可取消）。
func setupReachDeferredRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	q := service.NewPersistentQuietHoursQueue(repository.NewReachDeferredOutboundRepository(db))
	ctrl := controller.NewReachDeferredOutboundController(q)

	auth.GET("/reach/deferred-outbound", ctrl.List)
	auth.POST("/reach/deferred-outbound/:id/cancel", ctrl.Cancel)
}
