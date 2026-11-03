package controller

import (
	"net/http"
	"strings"

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
	// 未选模型时后端无法验证：给出可执行 400（原来是 500「AI 搜索验证失败」，用户不知道下一步做什么）
	if len(req.Models) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请选择至少一个验证模型后再验证（当前部署没有可用的云端模型时，请先在「模型提供商」启用一个可访问的模型）")
		return
	}
	result, err := c.svc.VerifyArticle(ctx.Request.Context(), req)
	if err != nil {
		// 服务层错误带具体原因（如 provider 连接失败），直接透出比「服务器内部错误」可执行
		msg := err.Error()
		if !strings.Contains(msg, "AI 搜索验证失败") {
			msg = "AI 搜索验证失败: " + msg
		}
		response.Error(ctx, http.StatusInternalServerError, msg)
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
