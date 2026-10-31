package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// VerificationController GEO AI 搜索验证控制器。
type VerificationController struct {
	svc *service.VerificationService
}

// NewVerificationController 构造验证控制器。
func NewVerificationController(svc *service.VerificationService) *VerificationController {
	return &VerificationController{svc: svc}
}

// @Summary 执行 AI 搜索验证
// @Tags      verification
// @Router    /api/geo/verification/verify [post]
// VerifyArticle 执行 AI 搜索验证
// POST /geo/verification/verify
func (c *VerificationController) VerifyArticle(ctx *gin.Context) {
	var req dto.VerifyRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.VerifyArticle(ctx.Request.Context(), req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "AI 搜索验证失败")
		return
	}
	response.Success(ctx, result, "AI 搜索验证成功")
}

// @Summary 负面提及监控
// @Tags      verification
// @Router    /api/geo/verification/negative [post]
// MonitorNegative 负面提及监控
// POST /geo/verification/negative
func (c *VerificationController) MonitorNegative(ctx *gin.Context) {
	var req struct {
		BrandName string `json:"brand_name"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.MonitorNegative(ctx.Request.Context(), req.BrandName)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "负面监控失败")
		return
	}
	response.Success(ctx, result, "负面监控成功")
}

// @Summary 获取验证结果
// @Tags      verification
// @Router    /api/geo/verification/results/{article_id} [get]
// GetVerifyResults 获取验证结果
// GET /geo/verification/results/:article_id
func (c *VerificationController) GetVerifyResults(ctx *gin.Context) {
	articleID := ctx.Param("article_id")
	if articleID == "" {
		response.Error(ctx, http.StatusBadRequest, "文章ID不能为空")
		return
	}
	result, err := c.svc.GetVerifyResults(ctx.Request.Context(), articleID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取验证结果失败")
		return
	}
	response.Success(ctx, result, "获取验证结果成功")
}
