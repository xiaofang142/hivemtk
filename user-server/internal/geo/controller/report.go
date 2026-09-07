package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ReportController GEO 报表控制器。
type ReportController struct {
	svc          *service.ReportService
	analyticsSvc *service.GeoDecisionAnalyticsService
}

// NewReportController 构造报表控制器。
func NewReportController(svc *service.ReportService, analyticsSvc *service.GeoDecisionAnalyticsService) *ReportController {
	return &ReportController{svc: svc, analyticsSvc: analyticsSvc}
}

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

// GetAPICosts 获取 API 调用成本
// GET /geo/reports/api-costs
func (c *ReportController) GetAPICosts(ctx *gin.Context) {
	result, err := c.svc.GetAPICosts(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取 API 成本失败")
		return
	}
	response.Success(ctx, result, "获取 API 成本成功")
}

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

// ShareOfVoiceTrend SOV 趋势环比：当前窗口 vs 上一窗口
// GET /geo/sov/trend?intent=&days=30
// current = 最近 days 天；previous = 其前一个 days 天（两窗口不重叠）
func (c *ReportController) ShareOfVoiceTrend(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	if days <= 0 || days > 365 {
		days = 30
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

// CrawlerStats 爬虫统计
// GET /geo/crawler-stats
func (c *ReportController) CrawlerStats(ctx *gin.Context) {
	if c.analyticsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "analytics service 未初始化")
		return
	}
	result, err := c.analyticsSvc.GetCrawlerStats(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "爬虫统计失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

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
		response.Error(ctx, http.StatusInternalServerError, "不准确声明检测失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

// RunCrawler 手动触发竞品监控爬虫
// POST /geo/crawler/run
func (c *ReportController) RunCrawler(ctx *gin.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[GEO Crawler] panic: %v\n", r)
			}
		}()
		service.CrawlerMonitorCronSync()
	}()
	response.Success(ctx, map[string]string{"status": "started"}, "爬虫已启动")
}
