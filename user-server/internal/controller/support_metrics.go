package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// SupportMetricsController 指标看板接口：自助解决率 / 转人工率 / 会话轮次 / 知识库召回准确率。
type SupportMetricsController struct {
	svc service.SupportMetricsService
}

// NewSupportMetricsController 构造指标看板控制器。
func NewSupportMetricsController(svc service.SupportMetricsService) *SupportMetricsController {
	return &SupportMetricsController{svc: svc}
}

// GetSupportMetrics GET /api/metrics/support?days=30
func (c *SupportMetricsController) GetSupportMetrics(ctx *gin.Context) {
	if !c.svc.Available() {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "指标看板数据层不可用", gin.H{})
		return
	}
	days, err := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	if err != nil {
		days = 30
	}

	metrics, err := c.svc.Collect(ctx.Request.Context(), days)
	if err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "获取指标看板失败", gin.H{})
		return
	}
	response.Success(ctx, metrics, "ok")
}
