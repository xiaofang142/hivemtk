package controller

import (
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// KeywordEnhanceController GEO 关键词数据增强控制器。
type KeywordEnhanceController struct {
	svc *service.KeywordEnhanceService
}

// NewKeywordEnhanceController 构造关键词数据增强控制器。
func NewKeywordEnhanceController(svc *service.KeywordEnhanceService) *KeywordEnhanceController {
	return &KeywordEnhanceController{svc: svc}
}

// @Summary 分析历史关键词表现
// @Tags      keyword-enhance
// @Router    /api/geo/keyword-enhance/analyze [get]
// Analyze 分析历史关键词表现
// GET /geo/keyword-enhance/analyze?brand_name=
func (c *KeywordEnhanceController) Analyze(ctx *gin.Context) {
	result, err := c.svc.AnalyzeHistoricalPerformance(ctx.Request.Context(), ctx.Query("brand_name"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "分析失败")
		return
	}
	response.Success(ctx, result, "分析完成")
}

// @Summary 用历史数据增强现有关键词
// @Tags      keyword-enhance
// @Router    /api/geo/keyword-enhance/enhance [post]
// Enhance 用历史数据增强现有关键词
// POST /geo/keyword-enhance/enhance
func (c *KeywordEnhanceController) Enhance(ctx *gin.Context) {
	var req struct {
		Keywords  []string `json:"keywords" binding:"required"`
		BrandName string   `json:"brand_name" binding:"required"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.EnhanceKeywordWithData(ctx.Request.Context(), req.Keywords, req.BrandName)
	if err != nil {
		response.ErrorFromDB(ctx, err, "增强失败")
		return
	}
	response.Success(ctx, result, "增强完成")
}
