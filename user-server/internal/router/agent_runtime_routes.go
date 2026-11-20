package router

import (
	"context"

	"hivemtk-user/internal/aiagent/agent/lifecycle"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/controller"

	"github.com/gin-gonic/gin"
)

func setupInferenceRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewAgentInferenceController(app.GetInferenceOrchestrator, app.GetGlobalPermissionChecker)
	auth.POST("/agent/inference/run", ctrl.Run)
	auth.GET("/agent/inference/stats", ctrl.Stats)
}

func setupToolPermissionRoutes(auth *gin.RouterGroup) {
	ctrl := controller.NewAgentInferenceController(app.GetInferenceOrchestrator, app.GetGlobalPermissionChecker)
	auth.GET("/tools/permission/default", ctrl.GetPermissionDefault)
	auth.PUT("/tools/permission/default", ctrl.SetPermissionDefault)
	auth.GET("/tools/permission/global", ctrl.GetGlobalWhitelist)
	auth.POST("/tools/permission/global", ctrl.AddGlobalWhitelist)
	auth.GET("/tools/permission/agents", ctrl.ListConfiguredAgents)
	auth.GET("/tools/permission/agents/:agent_id", ctrl.GetAgentWhitelist)
	auth.POST("/tools/permission/agents/:agent_id", ctrl.SetAgentWhitelist)
	auth.DELETE("/tools/permission/agents/:agent_id", ctrl.RemoveAgentWhitelist)
}

func setupAgentLifecycleRoutes(auth *gin.RouterGroup) {
	var run func(context.Context, uint, *lifecycle.LifecycleRequest) (*lifecycle.LifecycleResult, error)
	if rt := app.GetAgentLifecycleRuntime(); rt != nil {
		run = rt.Run
	}
	ctrl := controller.NewAgentLifecycleController(run)
	auth.POST("/agent/lifecycle/run", ctrl.Run)
}
