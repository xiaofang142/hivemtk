package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ConfigController GEO 配置控制器。
type ConfigController struct {
	svc *service.ConfigService
}

// NewConfigController 构造配置控制器。
func NewConfigController(svc *service.ConfigService) *ConfigController {
	return &ConfigController{svc: svc}
}

// @Summary 获取 GEO 配置
// @Tags      config
// @Router    /api/geo/config [get]
// GetConfig 获取 GEO 配置
// GET /geo/config
func (c *ConfigController) GetConfig(ctx *gin.Context) {
	result, err := c.svc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取配置失败")
		return
	}
	response.Success(ctx, result, "获取配置成功")
}

// @Summary 更新 GEO 配置（管理员）
// @Tags      config
// @Router    /api/geo/config [put]
// UpdateConfig 更新 GEO 配置（管理员）
// PUT /geo/config
func (c *ConfigController) UpdateConfig(ctx *gin.Context) {
	var req struct {
		Brand            string   `json:"brand"`
		BrandDescription string   `json:"brand_description"`
		Advantages       string   `json:"advantages"`
		Competitors      []string `json:"competitors"`
		Domain           string   `json:"domain"`
		DefaultModel     string   `json:"default_model"`
		VerifyModels     []string `json:"verify_models"`
		NegativeKeywords []string `json:"negative_keywords"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	err := c.svc.UpdateConfig(ctx.Request.Context(), req.Brand, req.BrandDescription,
		req.Advantages, req.Competitors, req.Domain, req.DefaultModel, req.VerifyModels, req.NegativeKeywords)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "更新配置失败")
		return
	}
	response.Success(ctx, nil, "更新配置成功")
}

// @Summary 优化 GEO 配置（管理员）
// @Tags      config
// @Router    /api/geo/config/optimize [post]
// OptimizeConfig 优化 GEO 配置（管理员）
// POST /geo/config/optimize
func (c *ConfigController) OptimizeConfig(ctx *gin.Context) {
	var req struct {
		BrandName   string   `json:"brand_name"`
		Advantages  string   `json:"advantages"`
		Competitors []string `json:"competitors"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.OptimizeConfig(ctx.Request.Context(), req.BrandName, req.Advantages, req.Competitors)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "配置优化失败")
		return
	}
	response.Success(ctx, result, "配置优化成功")
}
