package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupPilotContractRoutes 仅挂租户管理员档，安装身份不能通过公开/普通登录口泄露。
func setupPilotContractRoutes(tenantAdmin *gin.RouterGroup, database *gorm.DB) {
	svc := service.NewPilotContractService(repository.NewPilotContractRepository(database), nil)
	ctrl := controller.NewPilotContractController(svc)
	tenantAdmin.GET("/pilot/contract", ctrl.Get)
	tenantAdmin.PUT("/pilot/contract", ctrl.Save)
	tenantAdmin.POST("/pilot/contract/validate", ctrl.Validate)
}
