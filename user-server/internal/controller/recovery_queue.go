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
//
// 路由表里只有三条读接口（GET /api/recovery-queue/list、/distribution、/ready），
// 下面四个写方法一条都没注册（用 gin 的 RouteInfo 现跑路由表核对过）。队列的真实数据面是这样：
//   - 入队：CustomerRFMService.enqueueRecovery（internal/service/customer_rfm.go，
//     由 ComputeForCustomer 与 computeForCustomerLoaded 调用）直接走 repository.Create，
//     绕过了本控制器与 service.Enqueue，所以队列有数据、但不是从 service 的入队口径来的；
//   - 推进：worker 只用 service.MarkAttempt 与 service.DeferAttempt
//     （internal/service/recovery_queue_worker.go），
//     失败耗尽次数置 failed、命中免打扰置 cancelled，都由 worker 传 stage 完成；
//   - 缺口：全仓没有任何代码把队列项写成 succeed——RecoveryStageSucceed 只出现在
//     service.MarkRecovered 里，而它的唯一调用方就是下面那个没有路由的方法。
//     客户回流后这条记录不会收敛成"已挽回"，只会一路走到 failed。
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
// 未接线：路由表里没有这条 POST；且 service.Enqueue 也没有任何生产调用方——
// 现在的入队来自 RFM 计算里对 repository 的直接写入，带文案/模板/渠道偏好的完整入队口径无人使用。
func (c *RecoveryQueueController) Enqueue(ctx *gin.Context) {
	var req dto.RecoveryEnqueueRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	item, err := c.svc.Enqueue(context.Background(), &service.RecoveryEnqueueInput{
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
// 未接线：路由表里没有这个 POST。这里的活由恢复队列 worker 自己推进
// （internal/service/recovery_queue_worker.go 调 service.MarkAttempt 记尝试并推 stage），无 HTTP 出口。
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
// 未接线：路由表里没有这个 POST，且 service.MarkRecovered 也没有任何调用方——
// 它是全仓唯一把队列项写成 succeed 的地方（RecoveryStageSucceed 只出现在那个方法里），
// 所以客户回流后这条记录没有收敛出口，只会走到 queued/running/failed/cancelled。
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
// 未接线：路由表里的取消接口属于跟进/SOP 执行（POST /api/followups/:id/cancel 等，另一个服务）；
// 本方法的 service.Cancel 也没有调用方。队列项确实会被置成 cancelled，但那是 worker 命中
// 免打扰时自己传 stage 走 MarkAttempt 做的（internal/service/recovery_queue_worker.go），
// 不是从这个入口。
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
