package router

import (
	"context"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupWorkflowOrchestratorRoutes(auth *gin.RouterGroup, db *gorm.DB) {

	versionRepo := repository.NewWorkflowVersionRepository(db)
	execRepo := repository.NewWorkflowExecutionRepository(db)
	nodeExecRepo := repository.NewWorkflowNodeExecutionRepository(db)

	svc := service.NewWorkflowOrchestratorService(versionRepo, execRepo, nodeExecRepo)

	registry := service.NewWorkflowNodeExecutorRegistry()
	service.RegisterWorkflowNodeExecutors(registry, svc)
	dispatcher := service.NewWorkflowDispatcher(versionRepo, execRepo, nodeExecRepo, registry, nil)
	dispatcher.Start(context.Background())
	svc.SetDispatcher(dispatcher)

	workflowCtrl := controller.NewWorkflowOrchestratorController(svc)

	auth.GET("/workflows/versions", workflowCtrl.ListVersions)
	auth.GET("/workflows/versions/:id", workflowCtrl.GetVersion)

	adminGroup := auth.Group("/workflows/versions", middleware.AdminAuthMiddleware())
	{
		adminGroup.POST("", workflowCtrl.CreateVersion)
		adminGroup.PUT("/:id", workflowCtrl.UpdateVersion)
		adminGroup.DELETE("/:id", workflowCtrl.DeleteVersion)
		adminGroup.POST("/:id/publish", workflowCtrl.PublishVersion)
		adminGroup.POST("/:id/archive", workflowCtrl.ArchiveVersion)
	}

	auth.GET("/workflows/executions", workflowCtrl.ListExecutions)
	auth.GET("/workflows/executions/:id", workflowCtrl.GetExecution)
	auth.GET("/workflows/executions/:id/nodes", workflowCtrl.GetNodeExecutions)

	execAdmin := auth.Group("", middleware.AdminAuthMiddleware())
	execAdmin.POST("/workflows/execute", workflowCtrl.Execute)
	execAdmin.POST("/workflows/executions/:id/stop", workflowCtrl.StopExecution)
}
