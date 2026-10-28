package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupCapabilityRuntimeRoutes D01：能力运行视图只读口。
// 前端配置器/账号诊断与后端执行校验消费同一份视图，避免"菜单有但后端无执行路径"。
func setupCapabilityRuntimeRoutes(auth *gin.RouterGroup, db *gorm.DB) {
	svc := service.NewCapabilityRuntimeService(
		repository.NewChannelOverviewRepository(db),
		repository.NewExternalKBConnectorRepo(),
	)
	ctrl := controller.NewCapabilityRuntimeController(svc)
	auth.GET("/capabilities", ctrl.List)
}
