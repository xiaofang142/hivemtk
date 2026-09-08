package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// AgentSettingsController Agent Loop 运行期调参控制器
type AgentSettingsController struct{}

// NewAgentSettingsController 构造
func NewAgentSettingsController() *AgentSettingsController {
	return &AgentSettingsController{}
}

// GetConfig 读取 Agent Loop 运行期调参（GET /api/agent/settings）
func (c *AgentSettingsController) GetConfig(ctx *gin.Context) {
	cfg, err := service.LoadAgentSettingsConfig(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, 500, err.Error())
		return
	}
	response.Success(ctx, cfg, "ok")
}

// SaveConfig 保存 Agent Loop 运行期调参（PUT /api/agent/settings）
func (c *AgentSettingsController) SaveConfig(ctx *gin.Context) {
	var cfg service.AgentSettingsConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, "请求体格式错误: "+err.Error())
		return
	}
	if cfg.MaxLoopIterations != 0 && cfg.MaxLoopIterations < 2 {
		response.Error(ctx, 400, "max_loop_iterations 必须 >= 2（否则工具调用无法产出答案）")
		return
	}
	if err := service.SaveAgentSettingsConfig(ctx.Request.Context(), &cfg); err != nil {
		response.Error(ctx, 500, err.Error())
		return
	}
	response.Success(ctx, cfg, "ok")
}
