package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupCapabilityRuntimeRoutes D01：能力运行视图与租户管理员动作格查询只读口。
// 前端配置器/账号诊断与后端执行校验消费同一份视图，避免"菜单有但后端无执行路径"。
func setupCapabilityRuntimeRoutes(auth, tenantAdmin *gin.RouterGroup, db *gorm.DB) {
	svc := service.NewCapabilityRuntimeService(
		repository.NewChannelOverviewRepository(db),
		repository.NewExternalKBConnectorRepo(),
		repository.NewCapabilityEvidenceRepository(db),
	)
	ctrl := controller.NewCapabilityRuntimeController(svc)
	auth.GET("/capabilities", ctrl.List)
	tenantAdmin.GET("/v1/capabilities/cells", ctrl.ListCells)
	tenantAdmin.GET("/v1/capabilities/resolve", ctrl.ResolveCell)
}
