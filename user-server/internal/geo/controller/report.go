package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/reporting"

	"github.com/gin-gonic/gin"
)

// ReportController GEO 报表控制器。
type ReportController struct {
	svc          *service.ReportService
	analyticsSvc *service.GeoDecisionAnalyticsService
	startCrawler func(context.Context) error
}

// NewReportController 构造报表控制器。
func NewReportController(svc *service.ReportService, analyticsSvc *service.GeoDecisionAnalyticsService) *ReportController {
	return &ReportController{svc: svc, analyticsSvc: analyticsSvc, startCrawler: service.StartCrawlerProbe}
}

// @Summary 获取 GEO 汇总报表
// @Tags      report
// @Router    /api/geo/reports/summary [get]
// GetReport 获取 GEO 汇总报表
// GET /geo/reports/summary
func (c *ReportController) GetReport(ctx *gin.Context) {
	startDate := ctx.Query("start_date")
	endDate := ctx.Query("end_date")

	result, err := c.svc.GetReport(ctx.Request.Context(), startDate, endDate)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取汇总报表失败")
		return
	}
	response.Success(ctx, result, "获取汇总报表成功")
}

// @Summary 获取 ROI 分析
// @Tags      report
// @Router    /api/geo/reports/roi [get]
// GetROI 获取 ROI 分析
// GET /geo/reports/roi
func (c *ReportController) GetROI(ctx *gin.Context) {
	provider := ctx.Query("provider")
	model := ctx.Query("model")
	startDate := ctx.Query("start_date")
	endDate := ctx.Query("end_date")

	result, err := c.svc.GetROI(ctx.Request.Context(), provider, model, startDate, endDate)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取 ROI 分析失败")
		return
	}
	response.Success(ctx, result, "获取 ROI 分析成功")
}

// @Summary 获取 API 调用成本
// @Tags      report
// @Router    /api/geo/reports/api-costs [get]
// GetAPICosts 获取 API 调用成本
// GET /geo/reports/api-costs
func (c *ReportController) GetAPICosts(ctx *gin.Context) {
	// 与 summary/roi 一致支持日期范围过滤（原来忽略 start_date/end_date，页面上换日期结果不变）
	result, err := c.svc.GetAPICosts(ctx.Request.Context(), ctx.Query("start_date"), ctx.Query("end_date"))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取 API 成本失败")
		return
	}
	response.Success(ctx, result, "获取 API 成本成功")
}

// @Summary      SOV 竞品率分析
// @Tags      report
// @Router    /api/geo/sov [get]
// ShareOfVoice SOV 竞品率分析
// GET /geo/sov
func (c *ReportController) ShareOfVoice(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	result, err := c.analyticsSvc.GetShareOfVoice(ctx.Request.Context(), ctx.Query("intent"))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "SOV 分析失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      内容类型归因
// @Tags      report
// @Router    /api/geo/reports/content-format [get]
// ContentFormatAttribution 内容类型归因
// GET /geo/reports/content-format?days=30
// 按内容类型（意图）分组统计提及率/引用率；样本量同行标注，低于
// ContentFormatAttributionSampleFloor 的分组读数不可下结论（SampleSizeHint 字段）。
func (c *ReportController) ContentFormatAttribution(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", strconv.Itoa(reporting.DefaultWindowDays(ctx.Request.Context()))))
	res, err := c.analyticsSvc.GetContentFormatAttribution(ctx.Request.Context(), days)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "内容类型归因分析失败: "+err.Error())
		return
	}
	response.Success(ctx, res, "ok")
}

// @Summary      SOV 趋势环比：当前窗口 vs 上一窗口
// @Tags      report
// @Router    /api/geo/sov/trend [get]
// ShareOfVoiceTrend SOV 趋势环比：当前窗口 vs 上一窗口// GET /geo/sov/trend?intent=&days=30
// current = 最近 days 天；previous = 其前一个 days 天（两窗口不重叠）
func (c *ReportController) ShareOfVoiceTrend(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", strconv.Itoa(reporting.DefaultWindowDays(ctx.Request.Context()))))
	if days <= 0 || days > 365 {
		days = reporting.DefaultWindowDays(ctx.Request.Context())
	}
	intent := ctx.Query("intent")

	now := time.Now()
	current, err := c.analyticsSvc.GetShareOfVoiceBetween(ctx.Request.Context(), intent, now.AddDate(0, 0, -days), now)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "SOV 趋势分析失败: "+err.Error())
		return
	}
	previous, err := c.analyticsSvc.GetShareOfVoiceBetween(ctx.Request.Context(), intent, now.AddDate(0, 0, -days*2), now.AddDate(0, 0, -days))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "SOV 趋势分析失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"current":  current,
		"previous": previous,
		"days":     days,
		"intent":   intent,
	}, "ok")
}

// @Summary      主动 UA 探测成功统计（不证明外部抓取或收录）
// @Tags      report
// @Router    /api/geo/crawler-stats [get]
// CrawlerStats 爬虫统计
// GET /geo/crawler-stats
func (c *ReportController) CrawlerStats(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	result, err := c.analyticsSvc.GetCrawlerStats(ctx.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrCrawlerTenantRequired) {
			response.Error(ctx, http.StatusUnauthorized, response.ErrUnauthorized)
			return
		}
		logger.Errorf("[GEO UA Probe] 读取统计失败: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "读取主动 UA 探测统计失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      不准确声明检测
// @Tags      report
// @Router    /api/geo/inaccurate-claims [post]
// InaccurateClaims 不准确声明检测
// POST /geo/inaccurate-claims
func (c *ReportController) InaccurateClaims(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	var body struct {
		BrandName string `json:"brand_name" binding:"required"`
	}
	if !response.BindJSON(ctx, &body) {
		return
	}
	result, err := c.analyticsSvc.DetectInaccurateClaims(ctx.Request.Context(), body.BrandName)
	if err != nil {
		respondGeoLLMError(ctx, err, "不准确声明检测")
		return
	}
	response.Success(ctx, result, "ok")
}

// @Summary      受理主动 UA 探测（不触发外部 AI 引擎抓取）
// @Tags      report
// @Router    /api/geo/crawler/run [post]
// RunCrawler 202 表示当前租户的主动 HTTP UA 探测已受理，结论尚未产生。
// POST /geo/crawler/run
func (c *ReportController) RunCrawler(ctx *gin.Context) {
	if err := c.startCrawler(ctx.Request.Context()); err != nil {
		if errors.Is(err, service.ErrCrawlerTenantRequired) {
			response.Error(ctx, http.StatusUnauthorized, response.ErrUnauthorized)
			return
		}
		logger.Errorf("[GEO UA Probe] 受理失败: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "主动 UA 探测受理失败")
		return
	}
	response.Accepted(ctx, map[string]any{
		"status": "accepted",
		"data_source": map[string]any{
			"kind": "active_ua_probe", "success_criteria": "http_2xx_3xx",
			"external_crawl_verified": false, "indexing_verified": false,
		},
	}, "主动 UA 探测已受理，不代表外部引擎抓取或收录")
}
