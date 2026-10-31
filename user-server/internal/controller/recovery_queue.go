package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// RecoveryQueueController 流失挽回队列控制器
//
// 队列的数据面分三条腿：
//   - 入队：RFM 计算（internal/service/customer_rfm.go 的 enqueueRecovery）直接走
//     repository.Create；POST /api/recovery-queue/enqueue 则提供带文案/模板/渠道偏好的
//     完整入队口径，供运营手工补录；
//   - 推进：worker（internal/service/recovery_queue_worker.go）用 service.MarkAttempt /
//     DeferAttempt 自己记尝试并推进 stage；POST /:id/attempt 只给人工补记/纠错用；
//   - 收敛：客户实际回流后由管理员 POST /:id/recovered 落 succeed —— 这是全仓唯一
//     写 succeed 的入口，repository.MarkRecovered 会同时写 recovered_at/recovery_value；
//     退订/放弃走 POST /:id/cancel。
//
// 四条写方法统一挂在 middleware.AdminAuthMiddleware() 之后（见
// internal/router/content_routes.go 的 setupRecoveryQueueRoutes）：它们动的是
// "要不要继续给流失客户发消息"的队列状态，不该是所有登录态都点得动的。
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
// 路由：POST /api/recovery-queue/enqueue（admin）。RFM 自动入队另走 repository.Create，
// 这条给的是带文案/模板/渠道偏好的完整口径。
func (c *RecoveryQueueController) Enqueue(ctx *gin.Context) {
	var req dto.RecoveryEnqueueRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	item, err := c.svc.Enqueue(ctx.Request.Context(), &service.RecoveryEnqueueInput{
		CustomerID:        req.CustomerID,
		UnifiedID:         req.UnifiedID,
		Account:           req.Account,
		Reason:            req.Reason,
		Strategy:          req.Strategy,
		Priority:          req.Priority,
		Content:           req.Content,
		Subject:           req.Subject,
		TemplateID:        req.TemplateID,
		Params:            req.Params,
		PreferredChannels: req.PreferredChannels,
		MaxAttempts:       req.MaxAttempts,
	})
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
// 路由：POST /api/recovery-queue/:id/attempt（admin）。worker 内部仍直接调 service.MarkAttempt；
// 这条给人工补记/纠错一个出口。stage 只收 queued/running/failed/cancelled，
// succeed 必须走 /recovered（要同时写 recovered_at/recovery_value）。
func (c *RecoveryQueueController) MarkAttempt(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req dto.RecoveryMarkAttemptRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
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
// @Summary POST /api/recovery-queue/{id}/recovered
// @Tags 挽回队列
// @Accept json
// @Produce json
// @Param id path int true "队列 ID"
// @Param request body dto.RecoveryMarkRecoveredRequest true "挽回金额"
// @Success 200 {object} object{message=string}
// @Router /api/recovery-queue/{id}/recovered [post]
// 路由：POST /api/recovery-queue/:id/recovered（admin）。全仓唯一把队列项写成 succeed 的入口：
// 客户实际回流后由管理员确认，repository.MarkRecovered 同一笔写 recovered_at/recovery_value。
// 合法迁移见 service.MarkRecovered（queued/running/failed → succeed；cancelled 拒绝）。
func (c *RecoveryQueueController) MarkRecovered(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req dto.RecoveryMarkRecoveredRequest
	// 空 body 合法（只标记成功、不记金额）；有 body 但 JSON 坏了要报 400，
	// 不能静默当成 recovery_value=0 落库。
	if err := ctx.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
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
// 路由：POST /api/recovery-queue/:id/cancel（admin）。worker 命中免打扰时也会把它置
// cancelled（走 MarkAttempt 传 stage），两条路共用同一终态；service.Cancel 只收
// queued/running，failed/succeed 是终态不许再取消。
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
