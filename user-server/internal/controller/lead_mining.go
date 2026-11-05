package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type LeadMiningController struct{}

func NewLeadMiningController() *LeadMiningController {
	return &LeadMiningController{}
}

// @Summary      获取线索挖掘配置
// @Tags      lead-mining
// @Router    /api/lead-mining/config [get]
func (c *LeadMiningController) GetConfig(ctx *gin.Context) {
	cfg, err := service.GetLeadMiningConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "success")
}

// @Summary      获取线索挖掘运行状态
// @Tags      lead-mining
// @Router    /api/lead-mining/status [get]
func (c *LeadMiningController) GetStatus(ctx *gin.Context) {
	status := service.GetStatus()
	response.Success(ctx, status, "success")
}

// @Summary      请求体不能为空：至少提供一个配置字段（如 keywords、requirement）
// @Tags      lead-mining
// @Router    /api/lead-mining/config [post]
func (c *LeadMiningController) SaveConfig(ctx *gin.Context) {
	var cfg dto.LeadMiningConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, err.Error())
		return
	}
	// 防御空 body {} 全零值覆盖配置
	if isEmptyLeadMiningConfig(&cfg) {
		response.Error(ctx, 400, "请求体不能为空：至少提供一个配置字段（如 keywords、requirement）")
		return
	}
	if err := service.SaveLeadMiningConfig(ctx.Request.Context(), &cfg); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, cfg, "success")
}

// isEmptyLeadMiningConfig 判断请求是否未携带任何业务字段
func isEmptyLeadMiningConfig(cfg *dto.LeadMiningConfig) bool {
	return !cfg.Enabled && len(cfg.Keywords) == 0 && len(cfg.Tags) == 0 &&
		cfg.Requirement == "" && len(cfg.Channels) == 0 &&
		cfg.MinIntentScore == 0 && cfg.Model == ""
}
