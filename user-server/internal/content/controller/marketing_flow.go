package controller

import (
	"errors"
	"strings"

	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// handleFlowWriteError 分流营销流程写口的错误：
//   - 入参/业务校验 → 400，并把原文带给调用方（A15）
//   - 无权限 → 403（contract 测试要求）
//   - 其他（含租户 fail-closed）→ 500 原文，保留既有契约
func handleFlowWriteError(ctx *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case errors.Is(err, utils.ErrInvalidInput):
		response.Error(ctx, http.StatusBadRequest, msg)
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "无权限"):
		response.Error(ctx, http.StatusForbidden, msg)
	case errhttp.IsNotFoundError(err):
		response.NotFound(ctx, msg)
	default:
		// 内部错误（含租户 fail-closed）只回兜底文案，不回显表名/租户细节
		response.Error(ctx, http.StatusInternalServerError, fallback)
	}
	return true
}

// flowCaller 取「身份 + 是否管理员」。
//
// 身份类型必须过 convertToUint：用户侧 JWT 中间件存 uint，团队侧存 float64/string，
// 改前这里是 `userID.(uint)`——团队身份进来直接 panic，无身份时 nil.(uint) 同样 panic，
// 两者都表现为 500 而不是鉴权/权限失败。
func flowCaller(ctx *gin.Context) (uint, bool, bool) {
	uid, ok := callerUserID(ctx)
	role, _ := ctx.Get("role")
	admin, _ := role.(string)
	isAdmin := admin == "admin" || admin == "super_admin"
	return uid, ok, isAdmin && ok
}

// MarketingFlowController 营销流程控制器
type MarketingFlowController struct {
	flowService *service.MarketingFlowService
}

// NewMarketingFlowController 创建营销流程控制器实例
func NewMarketingFlowController() *MarketingFlowController {
	return &MarketingFlowController{
		flowService: service.NewMarketingFlowService(db.GetDB()),
	}
}

// CreateFlow 创建流程
func (c *MarketingFlowController) CreateFlow(ctx *gin.Context) {
	userID, ok, _ := flowCaller(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	var req service.CreateFlowRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	flow, err := c.flowService.CreateFlow(ctx.Request.Context(), userID, &req)
	// 创建失败大多是**入参/业务规则**（trigger_type 非法、flow_data 结构不对），
	// 用 HandleDBError 会统一压成 400 INVALID_PARAM + 一句「创建营销流程失败」，
	// 把可行动的原因吃掉（A15）。但租户 fail-closed / 无权访问又必须保持 500 / 403
	// （既有契约测试钉住），所以这里按错误类别分流，而不是一刀切。
	if handleFlowWriteError(ctx, err, "创建营销流程失败") {
		return
	}

	response.Success(ctx, flow, "创建成功")
}

// GetFlowList 获取流程列表
func (c *MarketingFlowController) GetFlowList(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	flows, total, err := c.flowService.GetFlowList(ctx.Request.Context(), page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取营销流程列表") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      flows,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetFlowByID 获取流程详情
func (c *MarketingFlowController) GetFlowByID(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	flow, err := c.flowService.GetFlowByID(ctx.Request.Context(), id)
	if errhttp.HandleDBError(ctx, err, "获取营销流程") {
		return
	}

	response.Success(ctx, flow, "获取成功")
}

// UpdateFlow 更新流程
func (c *MarketingFlowController) UpdateFlow(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	var req service.UpdateFlowRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	userID, _, isAdmin := flowCaller(ctx)
	flow, err := c.flowService.UpdateFlow(ctx.Request.Context(), id, userID, isAdmin, &req)
	if handleFlowWriteError(ctx, err, "更新营销流程失败") {
		return
	}

	response.Success(ctx, flow, "更新成功")
}

// DeleteFlow 删除流程
func (c *MarketingFlowController) DeleteFlow(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	userID, _, isAdmin := flowCaller(ctx)
	if errhttp.HandleDBError(ctx, c.flowService.DeleteFlow(ctx.Request.Context(), id, userID, isAdmin), "删除营销流程") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// ActivateFlow 激活流程
func (c *MarketingFlowController) ActivateFlow(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	userID, _, isAdmin := flowCaller(ctx)
	if errhttp.HandleDBError(ctx, c.flowService.ActivateFlow(ctx.Request.Context(), id, userID, isAdmin), "激活营销流程") {
		return
	}

	response.Success(ctx, nil, "激活成功")
}

// PauseFlow 暂停流程
func (c *MarketingFlowController) PauseFlow(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	userID, _, isAdmin := flowCaller(ctx)
	if errhttp.HandleDBError(ctx, c.flowService.PauseFlow(ctx.Request.Context(), id, userID, isAdmin), "暂停营销流程") {
		return
	}

	response.Success(ctx, nil, "暂停成功")
}

// StopFlow 停止流程
func (c *MarketingFlowController) StopFlow(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	userID, _, isAdmin := flowCaller(ctx)
	if errhttp.HandleDBError(ctx, c.flowService.StopFlow(ctx.Request.Context(), id, userID, isAdmin), "停止营销流程") {
		return
	}

	response.Success(ctx, nil, "停止成功")
}

// GetExecutionList 获取执行记录列表
func (c *MarketingFlowController) GetExecutionList(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	executions, total, err := c.flowService.GetExecutionList(ctx.Request.Context(), id, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取执行记录") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      executions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetExecutionStats 获取执行统计
func (c *MarketingFlowController) GetExecutionStats(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	stats, err := c.flowService.GetExecutionStats(ctx.Request.Context(), id)
	if errhttp.HandleDBError(ctx, err, "获取执行统计") {
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// TriggerRequest 触发营销流程请求体
type TriggerRequest struct {
	TriggerID string         `json:"trigger_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Trigger 触发营销流程（手动触发 / 测试触发）
// POST /marketing-flows/:id/trigger
func (c *MarketingFlowController) Trigger(ctx *gin.Context) {
	id, ok := flowIDParam(ctx)
	if !ok {
		return
	}

	var req TriggerRequest
	_ = ctx.ShouldBindJSON(&req)

	uid, _, _ := flowCaller(ctx)
	uidStr := strconv.FormatUint(uint64(uid), 10)

	triggerID := req.TriggerID
	if triggerID == "" {
		triggerID = "manual-" + uidStr
	}

	execution, err := c.flowService.TriggerFlowByID(ctx.Request.Context(), id, triggerID, uidStr, req.Data)
	if errhttp.HandleDBError(ctx, err, "触发营销流程") {
		return
	}

	response.Success(ctx, execution, "流程已触发")
}

func flowIDParam(ctx *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的流程 ID")
		return 0, false
	}
	return uint(id), true
}
