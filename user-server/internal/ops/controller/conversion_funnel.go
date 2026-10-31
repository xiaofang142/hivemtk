// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"fmt"
	"time"

	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ConversionFunnelController 转化漏斗控制器
type ConversionFunnelController struct {
	svc *service.ConversionFunnelService
}

// NewConversionFunnelController 创建控制器
func NewConversionFunnelController() *ConversionFunnelController {
	return &ConversionFunnelController{svc: service.NewConversionFunnelService()}
}

// GetFunnel 获取漏斗
func (c *ConversionFunnelController) GetFunnel(ctx *gin.Context) {
	start, end, err := parseTimeRangeStrict(ctx)
	if err != nil {
		response.Error(ctx, 400, err.Error())
		return
	}
	report, err := c.svc.BuildFunnelCtx(ctx.Request.Context(), start, end)
	if err != nil {
		errhttp.HandleDBError(ctx, err, "漏斗生成失败")
		return
	}
	response.Success(ctx, report, "漏斗生成成功")
}

// GetStageDetails 阶段详情
func (c *ConversionFunnelController) GetStageDetails(ctx *gin.Context) {
	stage := ctx.Query("stage")
	start, end, err := parseTimeRangeStrict(ctx)
	if err != nil {
		response.Error(ctx, 400, err.Error())
		return
	}
	det, err := c.svc.GetStageDetailsCtx(ctx.Request.Context(), stage, start, end)
	if err != nil {
		errhttp.HandleDBError(ctx, err, "阶段详情失败")
		return
	}
	response.Success(ctx, det, "查询成功")
}

// parseTimeRangeStrict 严格解析 start_time / end_time：
// 非法时刻直接 400（旧口径是静默丢弃 → 用户以为查的是自己的区间，实际拿的是
// 近 30 天默认窗口，且响应里的 start_time/end_time 与请求对不上），并拒绝起止倒置。
func parseTimeRangeStrict(ctx *gin.Context) (time.Time, time.Time, error) {
	var start, end time.Time
	if v := ctx.Query("start_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return start, end, fmt.Errorf("start_time 必须是 RFC3339 时刻（如 2026-10-01T00:00:00+08:00），当前为 %q", v)
		}
		start = t
	}
	if v := ctx.Query("end_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return start, end, fmt.Errorf("end_time 必须是 RFC3339 时刻（如 2026-10-31T23:59:59+08:00），当前为 %q", v)
		}
		end = t
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		return start, end, fmt.Errorf("start_time 不能晚于 end_time")
	}
	return start, end, nil
}

func parseTimeRange(ctx *gin.Context) (time.Time, time.Time) {
	var start, end time.Time
	if v := ctx.Query("start_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t
		}
	}
	if v := ctx.Query("end_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = t
		}
	}
	return start, end
}
