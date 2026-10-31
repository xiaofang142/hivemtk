package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func reachHTTPStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrReachPipelineNotFound),
		errors.Is(err, service.ErrReachJobNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrReachInvalidChannel),
		errors.Is(err, service.ErrReachInvalidSteps),
		errors.Is(err, service.ErrReachInvalidPayload),
		errors.Is(err, service.ErrReachJobNotPending),
		errors.Is(err, service.ErrReachRateLimited):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

type ReachPipelineController struct {
	svc *service.ReachPipelineService
}

// NewReachPipelineController 创建触达 Pipeline 控制器
func NewReachPipelineController(svc *service.ReachPipelineService) *ReachPipelineController {
	return &ReachPipelineController{svc: svc}
}

// @Summary      创建触达管道
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines [post]
// CreatePipeline 创建 Pipeline
func (c *ReachPipelineController) CreatePipeline(ctx *gin.Context) {
	var req service.CreatePipelineRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	if req.RetryPolicy.MaxRetries == 0 {
		req.RetryPolicy = service.DefaultRetryPolicy()
	}
	if req.RateLimit.QPS == 0 {
		req.RateLimit = service.DefaultRateLimit()
	}
	pipe, err := c.svc.CreatePipeline(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, pipe, "创建成功")
}

// @Summary      更新触达管道
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id} [put]
// UpdatePipeline 更新
func (c *ReachPipelineController) UpdatePipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	var req service.CreatePipelineRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	pipe, err := c.svc.UpdatePipeline(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, pipe, "更新成功")
}

// @Summary      查询触达管道详情
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id} [get]
// GetPipeline 详情
func (c *ReachPipelineController) GetPipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	pipe, err := c.svc.GetPipeline(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "Pipeline 不存在")
		return
	}
	response.Success(ctx, pipe, "查询成功")
}

// @Summary      列表
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines [get]
// ListPipelines 列表
func (c *ReachPipelineController) ListPipelines(ctx *gin.Context) {
	channel := ctx.Query("channel")
	status := ctx.Query("status")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.ListPipelines(ctx.Request.Context(), channel, status, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary      删除触达管道
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id} [delete]
// DeletePipeline 删除
func (c *ReachPipelineController) DeletePipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.DeletePipeline(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

// @Summary 暂停
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id}/pause [post]
// PausePipeline 暂停
func (c *ReachPipelineController) PausePipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.PausePipeline(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"id": id}, "暂停成功")
}

// @Summary 恢复
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id}/resume [post]
// ResumePipeline 恢复
func (c *ReachPipelineController) ResumePipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.ResumePipeline(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"id": id}, "恢复成功")
}

// @Summary 归档
// @Tags      reach-pipeline
// @Router    /api/reach/pipelines/{id}/archive [post]
// ArchivePipeline 归档
func (c *ReachPipelineController) ArchivePipeline(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.ArchivePipeline(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"id": id}, "归档成功")
}

// @Summary 入队任务
// @Tags      reach-pipeline
// @Router    /api/reach/jobs [post]
// EnqueueJob 入队任务
func (c *ReachPipelineController) EnqueueJob(ctx *gin.Context) {
	var req service.EnqueueJobRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	if req.RunAt == nil {
		now := time.Now()
		req.RunAt = &now
	}
	job, err := c.svc.EnqueueJob(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, job, "入队成功")
}

// @Summary      查询触达任务详情
// @Tags      reach-pipeline
// @Router    /api/reach/jobs/{id} [get]
// GetJob 任务详情
func (c *ReachPipelineController) GetJob(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	job, err := c.svc.GetJob(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "任务不存在")
		return
	}
	response.Success(ctx, job, "查询成功")
}

// @Summary      任务列表
// @Tags      reach-pipeline
// @Router    /api/reach/jobs [get]
// ListJobs 任务列表
func (c *ReachPipelineController) ListJobs(ctx *gin.Context) {
	channel := ctx.Query("channel")
	state := ctx.Query("state")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.ListJobs(ctx.Request.Context(), channel, state, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary 取消任务
// @Tags      reach-pipeline
// @Router    /api/reach/jobs/{id}/cancel [post]
// CancelJob 取消任务
func (c *ReachPipelineController) CancelJob(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.CancelJob(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "取消成功")
}

// @Summary 重试任务
// @Tags      reach-pipeline
// @Router    /api/reach/jobs/{id}/retry [post]
// RetryJob 重试任务
func (c *ReachPipelineController) RetryJob(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.RetryJob(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "重试成功")
}

// @Summary      任务被限流
// @Tags      reach-pipeline
// @Router    /api/reach/jobs/{id}/execute [post]
// ExecuteJob 立即执行
func (c *ReachPipelineController) ExecuteJob(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	job, err := c.svc.ExecuteJob(ctx.Request.Context(), uint(id))
	if err != nil {
		if err == service.ErrReachRateLimited && job != nil {
			response.Success(ctx, job, "任务被限流")
			return
		}
		response.Error(ctx, reachHTTPStatus(err), err.Error())
		return
	}
	response.Success(ctx, job, "执行成功")
}

// @Summary      触达统计
// @Tags      reach-pipeline
// @Router    /api/reach/stats [get]
// Stats 统计
func (c *ReachPipelineController) Stats(ctx *gin.Context) {
	stats, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, stats, "查询成功")
}

// @Summary 重置限流
// @Tags      reach-pipeline
// @Router    /api/reach/rate-limit/reset [post]
// ResetRateLimit 重置限流
func (c *ReachPipelineController) ResetRateLimit(ctx *gin.Context) {
	channel := ctx.Query("channel")
	if channel == "" {
		response.Error(ctx, http.StatusBadRequest, "channel 必填")
		return
	}
	c.svc.ResetRateLimit(ctx.Request.Context(), channel)
	response.Success(ctx, gin.H{"channel": channel}, "重置成功")
}

// @Summary GET /api/reach/jobs/with-experiment?experiment_id=xxx
// @Tags      reach-pipeline
// @Router    /api/reach/jobs/with-experiment [get]
// ListJobsWithExperiment GET /api/reach/jobs/with-experiment?experiment_id=xxx
func (c *ReachPipelineController) ListJobsWithExperiment(ctx *gin.Context) {
	experimentID := ctx.Query("experiment_id")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	list, total, err := c.svc.ListJobsByExperiment(ctx.Request.Context(), experimentID, page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "ok")
}
