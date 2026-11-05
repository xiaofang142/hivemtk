package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ToolIntegrationConfigController 工具集成配置控制器
type ToolIntegrationConfigController struct{}

// NewToolIntegrationConfigController 构造
func NewToolIntegrationConfigController() *ToolIntegrationConfigController {
	return &ToolIntegrationConfigController{}
}

// @Summary      读取工具集成配置（GET /api/agent/tool-integrations）
// @Tags      tool-integration-config
// @Router    /api/agent/tool-integrations [get]
// GetConfig 读取工具集成配置（GET /api/agent/tool-integrations）
func (c *ToolIntegrationConfigController) GetConfig(ctx *gin.Context) {
	cfg, err := service.LoadToolIntegrationConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "ok")
}

// @Summary      保存工具集成配置（PUT /api/agent/tool-integrations）
// @Tags      tool-integration-config
// @Router    /api/agent/tool-integrations [put]
// SaveConfig 保存工具集成配置（PUT /api/agent/tool-integrations）
func (c *ToolIntegrationConfigController) SaveConfig(ctx *gin.Context) {
	var cfg service.ToolIntegrationConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, "请求体格式错误: "+err.Error())
		return
	}
	if err := service.SaveToolIntegrationConfig(ctx.Request.Context(), &cfg); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "ok")
}
