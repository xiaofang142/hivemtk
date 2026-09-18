package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// RagRecallMonitorController 召回率监控控制器
type RagRecallMonitorController struct {
	svc        *service.RagRecallMonitorService
	metricsSvc *service.RagMetricsService
}

// NewRagRecallMonitorController 创建控制器
func NewRagRecallMonitorController(svc *service.RagRecallMonitorService, metricsSvc *service.RagMetricsService) *RagRecallMonitorController {
	return &RagRecallMonitorController{svc: svc, metricsSvc: metricsSvc}
}

// GetSnapshot godoc
// @Summary      获取最近一次召回率监控快照
// @Description  返回内存中最近一次定时采集的快照；若从未采集则返回空对象
// @Tags         RAG Recall
// @Produce      json
// @Success      200  {object}  service.RagRecallMetricsSummary
// @Router       /api/rag/recall/snapshot [get]
func (c *RagRecallMonitorController) GetSnapshot(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "召回率监控服务未初始化")
		return
	}
	snap, at := c.svc.GetLatestSnapshot(context.Background())
	if snap == nil {
		response.Success(ctx, gin.H{
			"summary":   nil,
			"captured":  nil,
			"available": false,
		}, "尚无监控数据，请先启动采集或等待定时任务")
		return
	}
	response.Success(ctx, gin.H{
		"summary":   snap,
		"captured":  at,
		"available": true,
	}, "ok")
}

// ListSnapshots godoc
// @Summary      列出最近 N 条监控快照
// @Description  按时间窗口倒序返回历史快照（默认 50，上限 1000）
// @Tags         RAG Recall
// @Produce      json
// @Param        limit  query  int  false  "返回条数（默认 50，最大 1000）"
// @Success      200    {array}  map[string]any
// @Router       /api/rag/recall/snapshots [get]
func (c *RagRecallMonitorController) ListSnapshots(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "召回率监控服务未初始化")
		return
	}
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	rows, err := c.svc.ListSnapshots(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询监控快照失败："+err.Error())
		return
	}
	response.SuccessWithPage(ctx, rows, 1, int64(limit), int64(len(rows)))
}

// CollectRequest 手动触发采集的请求体
type CollectRequest struct {
	WindowSeconds int `json:"window_seconds"`
}

// Collect godoc
// @Summary      手动触发一次召回率指标采集
// @Description  按指定时间窗口（默认 1 小时）聚合 rag_query_logs 并写入监控快照表
// @Tags         RAG Recall
// @Accept       json
// @Produce      json
// @Param        body  body      CollectRequest  false  "采集参数"
// @Success      200   {object}  service.RagRecallMetricsSummary
// @Router       /api/rag/recall/collect [post]
func (c *RagRecallMonitorController) Collect(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "召回率监控服务未初始化")
		return
	}
	var req CollectRequest
	_ = ctx.ShouldBindJSON(&req)

	window := time.Duration(req.WindowSeconds) * time.Second
	if window <= 0 {
		window = service.RagRecallMonitorDefaultWindow
	}
	end := time.Now()
	start := end.Add(-window)

	bgCtx, cancel := context.WithTimeout(ctx.Request.Context(), 30*time.Second)
	defer cancel()

	summary, err := c.svc.CollectAndStore(bgCtx, start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, "采集失败："+err.Error())
		return
	}
	response.Success(ctx, summary, "ok")
}

// Start godoc
// @Summary      启动后台定时采集
// @Tags         RAG Recall
// @Produce      json
// @Success      200  {object}  map[string]any
// @Router       /api/rag/recall/start [post]
func (c *RagRecallMonitorController) Start(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "召回率监控服务未初始化")
		return
	}
	c.svc.Start(context.Background())
	response.Success(ctx, gin.H{"started": true, "interval": service.RagRecallMonitorDefaultInterval.String()}, "已启动")
}

// Stop godoc
// @Summary      停止后台定时采集
// @Tags         RAG Recall
// @Produce      json
// @Success      200  {object}  map[string]any
// @Router       /api/rag/recall/stop [post]
func (c *RagRecallMonitorController) Stop(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "召回率监控服务未初始化")
		return
	}
	c.svc.Stop(context.Background())
	response.Success(ctx, gin.H{"started": false}, "已停止")
}

// GetRecallMetrics godoc
// @Summary      查询时间窗口内的召回指标聚合
// @Description  基于 rag_query_logs 计算 recall/precision/latency/P99/零命中/低召回统计
// @Tags         RAG Recall
// @Produce      json
// @Param        start  query  string  true   "开始时间 RFC3339"
// @Param        end    query  string  true   "结束时间 RFC3339"
// @Success      200    {object}  service.RecallMetrics
// @Router       /api/rag/recall/metrics [get]
func (c *RagRecallMonitorController) GetRecallMetrics(ctx *gin.Context) {
	if c.metricsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "RAG 指标服务未初始化")
		return
	}
	startStr := ctx.Query("start")
	endStr := ctx.Query("end")
	if startStr == "" || endStr == "" {
		response.Error(ctx, http.StatusBadRequest, "start/end 参数必填 (RFC3339)")
		return
	}
	start, err1 := time.Parse(time.RFC3339, startStr)
	end, err2 := time.Parse(time.RFC3339, endStr)
	if err1 != nil || err2 != nil {
		response.Error(ctx, http.StatusBadRequest, "时间格式错误，需 RFC3339")
		return
	}
	metrics, err := c.metricsSvc.GetRecallMetrics(ctx.Request.Context(), start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询召回指标失败："+err.Error())
		return
	}
	response.Success(ctx, metrics, "ok")
}

// GetLowRecallQueries godoc
// @Summary      查询召回率低于阈值的样本
// @Description  用于调优分析，按创建时间倒序返回低召回查询
// @Tags         RAG Recall
// @Produce      json
// @Param        threshold  query  number  false  "召回率阈值（默认 0.3）"
// @Param        limit      query  int    false  "返回条数（默认 50，最大 200）"
// @Success      200        {array}   service.LowRecallQuery
// @Router       /api/rag/recall/low-recall [get]
func (c *RagRecallMonitorController) GetLowRecallQueries(ctx *gin.Context) {
	if c.metricsSvc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "RAG 指标服务未初始化")
		return
	}
	threshold, _ := strconv.ParseFloat(ctx.DefaultQuery("threshold", "0.3"), 64)
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := c.metricsSvc.GetLowRecallQueries(ctx.Request.Context(), threshold, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询低召回样本失败："+err.Error())
		return
	}
	response.Success(ctx, rows, "ok")
}

// RegisterRoutes 注册路由
func (c *RagRecallMonitorController) RegisterRoutes(auth *gin.RouterGroup) {
	group := auth.Group("/rag/recall")
	{
		group.GET("/snapshot", c.GetSnapshot)
		group.GET("/snapshots", c.ListSnapshots)
		group.POST("/collect", c.Collect)
		group.POST("/start", c.Start)
		group.POST("/stop", c.Stop)
		group.GET("/metrics", c.GetRecallMetrics)
		group.GET("/low-recall", c.GetLowRecallQueries)
	}
}
