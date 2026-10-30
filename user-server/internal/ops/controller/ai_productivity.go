// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/reporting"

	"github.com/gin-gonic/gin"
)

// AIProductivityController AI 产能分析控制器
type AIProductivityController struct {
	svc *service.AIProductivityService
}

// NewAIProductivityController 创建控制器
func NewAIProductivityController() *AIProductivityController {
	return &AIProductivityController{svc: service.NewAIProductivityService()}
}

// GetReport 获取产能报告
func (c *AIProductivityController) GetReport(ctx *gin.Context) {
	start, end := parseTimeRange(ctx)
	rep, err := c.svc.BuildReport(ctx.Request.Context(), start, end)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "报告生成失败: "+err.Error())
		return
	}
	response.Success(ctx, rep, "报告生成成功")
}

// GetDailyTrend 日趋势
func (c *AIProductivityController) GetDailyTrend(ctx *gin.Context) {
	// days 会被服务层归一化（<=0 或 >90 回落默认窗口），控制器回显的 days 必须
	// 用同一个归一化值，否则 days=0 的响应里 "days":0 而 trend 实际是 30 天。
	defaultDays := reporting.DefaultWindowDays(ctx.Request.Context())
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", strconv.Itoa(defaultDays)))
	if days <= 0 || days > 90 {
		days = defaultDays
	}
	trend, err := c.svc.DailyTrend(ctx.Request.Context(), days)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "趋势查询失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"trend": trend, "days": days}, "查询成功")
}
