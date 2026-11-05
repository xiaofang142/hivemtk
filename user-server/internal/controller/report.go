// report_controller.go LLM 观测报表控制器（T3，五层 L2）。
//
// 设计文档：docs/architecture/design/T3-observability-reports.md
// 端点：
//
//	GET /api/reports/llm/overview     总览 KPI（含环比）
//	GET /api/reports/llm/timeseries   时间序列
//	GET /api/reports/llm/breakdown    维度分解
//	GET /api/reports/llm/errors       错误聚合+明细
//
// 权限：登录态（auth 组）；租户维度由 GORM 隔离 callback 强制（本仓隔离判据
// = 表有 tenant_id 列）。平台级跨租户查询走独立 Platform 端点（下一步接入）。
package controller

import (
	"net/http"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ReportController LLM 报表控制器
type ReportController struct {
	svc *service.ReportService
}

// NewReportController 构造（svc 未装配时端点回 503，与 sales-workbench 同口径）
func NewReportController(svc *service.ReportService) *ReportController {
	return &ReportController{svc: svc}
}

// reportRange 解析 from/to 日期参数（默认近 7 天，上限 92 天）
func reportRange(ctx *gin.Context) (time.Time, time.Time, bool) {
	now := time.Now()
	to := now
	from := now.AddDate(0, 0, -7)
	if v := ctx.Query("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		from = t
	}
	if v := ctx.Query("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		to = t.Add(24 * time.Hour) // 含当日
	}
	if to.Sub(from) > 92*24*time.Hour || to.Before(from) {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

// GetOverview GET /api/reports/llm/overview
//
//	@Tags     reports
//	@Router   /api/reports/llm/overview [get]
func (c *ReportController) GetOverview(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "报表服务未装配")
		return
	}
	from, to, ok := reportRange(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误：from/to 非法或跨度超 92 天")
		return
	}
	data, err := c.svc.Overview(ctx.Request.Context(), from, to, ctx.Query("channel"), ctx.Query("source"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, data, "ok")
}

// GetTimeseries GET /api/reports/llm/timeseries
//
//	@Tags     reports
//	@Router   /api/reports/llm/timeseries [get]
func (c *ReportController) GetTimeseries(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "报表服务未装配")
		return
	}
	from, to, ok := reportRange(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误：from/to 非法或跨度超 92 天")
		return
	}
	data, err := c.svc.Timeseries(ctx.Request.Context(), from, to,
		ctx.Query("model"), ctx.Query("scenario"), ctx.Query("channel"), ctx.Query("source"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, data, "ok")
}

// GetBreakdown GET /api/reports/llm/breakdown?by=model|scenario|channel|error_class|provider
//
//	@Tags     reports
//	@Router   /api/reports/llm/breakdown [get]
func (c *ReportController) GetBreakdown(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "报表服务未装配")
		return
	}
	from, to, ok := reportRange(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误：from/to 非法或跨度超 92 天")
		return
	}
	data, err := c.svc.Breakdown(ctx.Request.Context(), from, to, ctx.Query("by"),
		ctx.Query("model"), ctx.Query("scenario"), ctx.Query("channel"), ctx.Query("source"))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, data, "ok")
}

// GetErrors GET /api/reports/llm/errors
//
//	@Tags     reports
//	@Router   /api/reports/llm/errors [get]
func (c *ReportController) GetErrors(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "报表服务未装配")
		return
	}
	from, to, ok := reportRange(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误：from/to 非法或跨度超 92 天")
		return
	}
	page, pageSize := 1, 20
	if v := ctx.Query("page"); v != "" {
		if n, err := atoiDefault(v, 1); err == nil && n > 0 {
			page = n
		}
	}
	if v := ctx.Query("page_size"); v != "" {
		if n, err := atoiDefault(v, 20); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	data, err := c.svc.ErrorReport(ctx.Request.Context(), from, to, ctx.Query("error_class"), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, data, "ok")
}

func atoiDefault(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errNotNumber
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

type errNotNumberType struct{}

func (errNotNumberType) Error() string { return "not a number" }

var errNotNumber = errNotNumberType{}
