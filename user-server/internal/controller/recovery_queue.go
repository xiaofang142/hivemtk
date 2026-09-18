package controller

import (
	"context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RecoveryQueueController 流失挽回队列控制器
type RecoveryQueueController struct {
	svc *service.RecoveryQueueService
}

// NewRecoveryQueueController 创建控制器
func NewRecoveryQueueController() *RecoveryQueueController {
	return &RecoveryQueueController{svc: service.NewRecoveryQueueService()}
}

// Enqueue 手动入队
// @Summary 手动入队
// @Tags 挽回队列
// @Accept json
// @Produce json
// @Param request body dto.RecoveryEnqueueRequest true "入队参数"
// @Success 200 {object} object{data=dto.RecoveryQueueResponse}
// @Router /api/recovery-queue/enqueue [post]
func (c *RecoveryQueueController) Enqueue(ctx *gin.Context) {
	var req dto.RecoveryEnqueueRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	item, err := c.svc.Enqueue(context.Background(), req.CustomerID, req.UnifiedID, req.Account, req.Reason, req.Strategy, req.Priority)
	if err != nil {
		response.ErrorFromDB(ctx, err, "入队失败: "+err.Error())
		return
	}
	response.Success(ctx, service.FromRecoveryQueueModel(item), "ok")
}

// MarkAttempt 记录触达尝试
// @Summary 记录触达尝试
// @Tags 挽回队列
// @Accept json
// @Produce json
// @Param id path int true "队列 ID"
// @Param request body dto.RecoveryMarkAttemptRequest true "尝试参数"
// @Success 200 {object} object{message=string}
// @Router /api/recovery-queue/{id}/attempt [post]
func (c *RecoveryQueueController) MarkAttempt(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req dto.RecoveryMarkAttemptRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	delay := time.Duration(req.NextDelay) * time.Second
	if err := c.svc.MarkAttempt(ctx.Request.Context(), id, req.Channel, req.Result, req.Stage, delay); err != nil {
		response.ErrorFromDB(ctx, err, "记录失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "ok")
}

// MarkRecovered 标记挽回成功
// @Summary 标记挽回成功
// @Tags 挽回队列
// @Accept json
// @Produce json
// @Param id path int true "队列 ID"
// @Param request body dto.RecoveryMarkRecoveredRequest true "挽回金额"
// @Success 200 {object} object{message=string}
// @Router /api/recovery-queue/{id}/recovered [post]
func (c *RecoveryQueueController) MarkRecovered(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req dto.RecoveryMarkRecoveredRequest
	_ = ctx.ShouldBindJSON(&req)
	if err := c.svc.MarkRecovered(ctx.Request.Context(), id, req.RecoveryValue); err != nil {
		response.ErrorFromDB(ctx, err, "标记失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "ok")
}

// Cancel 取消
// @Summary 取消入队
// @Tags 挽回队列
// @Param id path int true "队列 ID"
// @Success 200 {object} object{message=string}
// @Router /api/recovery-queue/{id}/cancel [post]
func (c *RecoveryQueueController) Cancel(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	if err := c.svc.Cancel(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, "取消失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "ok")
}

// ListByStage 按阶段分页
// @Summary 按阶段分页
// @Tags 挽回队列
// @Param stage query string false "queued/running/succeed/failed/cancelled"
// @Param page query int false "页码"
// @Param page_size query int false "每页"
// @Success 200 {object} object{data=dto.RecoveryQueueListResponse}
// @Router /api/recovery-queue/list [get]
func (c *RecoveryQueueController) ListByStage(ctx *gin.Context) {
	stage := ctx.Query("stage")
	page := parsePositiveInt(ctx.Query("page"), 1, 10000)
	pageSize := parsePositiveInt(ctx.Query("page_size"), 20, 200)
	list, total, err := c.svc.ListByStage(ctx.Request.Context(), stage, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	resp := &dto.RecoveryQueueListResponse{
		List:     make([]*dto.RecoveryQueueResponse, 0, len(list)),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, item := range list {
		resp.List = append(resp.List, service.FromRecoveryQueueModel(item))
	}
	response.Success(ctx, resp, "ok")
}

// Distribution 阶段分布
// @Summary 阶段分布
// @Tags 挽回队列
// @Success 200 {object} object{data=dto.RecoveryDistributionResponse}
// @Router /api/recovery-queue/distribution [get]
func (c *RecoveryQueueController) Distribution(ctx *gin.Context) {
	dist, err := c.svc.Distribution(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	total := int64(0)
	for _, v := range dist {
		total += v
	}
	response.Success(ctx, dto.RecoveryDistributionResponse{Distribution: dist, Total: total}, "ok")
}

// ListReadyForAttempt 列出可触达任务
// @Summary 列出可触达任务
// @Tags 挽回队列
// @Param limit query int false "上限"
// @Success 200 {object} object{data=[]dto.RecoveryQueueResponse}
// @Router /api/recovery-queue/ready [get]
func (c *RecoveryQueueController) ListReadyForAttempt(ctx *gin.Context) {
	limit := parsePositiveInt(ctx.Query("limit"), 50, 500)
	list, err := c.svc.ListReadyForAttempt(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	resp := make([]*dto.RecoveryQueueResponse, 0, len(list))
	for _, item := range list {
		resp = append(resp, service.FromRecoveryQueueModel(item))
	}
	response.Success(ctx, resp, "ok")
}
