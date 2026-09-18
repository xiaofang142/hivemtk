package controller

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SystemConfigController 系统配置控制器
type SystemConfigController struct {
	svc *service.SystemConfigService
}

// NewSystemConfigController 创建系统配置控制器实例
func NewSystemConfigController() *SystemConfigController {
	return &SystemConfigController{svc: service.NewSystemConfigService()}
}

// GetConfig 获取系统配置
func (c *SystemConfigController) GetConfig(ctx *gin.Context) {
	config, err := c.svc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, config, "success")
}

// SaveConfig 保存系统配置
func (c *SystemConfigController) SaveConfig(ctx *gin.Context) {
	var req *model.SystemConfig
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if req == nil || isEmptySystemConfigRequest(req) {
		response.Error(ctx, http.StatusBadRequest, "请求体不能为空：至少提供一个配置字段（如 site_name）")
		return
	}
	config, err := c.svc.SaveConfig(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, config, "success")
}

// isEmptySystemConfigRequest 判断请求是否未携带任何业务字段。
// 防御空 body {} 把站点配置（site_name 等）整体清空。
func isEmptySystemConfigRequest(cfg *model.SystemConfig) bool {
	return cfg.Name == "" && cfg.WebsiteURL == "" && cfg.LogoURL == "" &&
		cfg.ThemeColor == "" && cfg.SEOKeywords == "" && cfg.SEODescription == "" &&
		cfg.ServicePhone == "" && cfg.ServiceEmail == "" &&
		cfg.ICPRecord == "" && cfg.PoliceRecord == "" &&
		!cfg.EnableRegister && !cfg.EnableEmailMarketing && !cfg.EnableRAG &&
		!cfg.MaintenanceMode && cfg.MaxUsers == 0 && cfg.MaxUploadSizeMB == 0
}
