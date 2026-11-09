// order_change_request.go TB-05：改单申请单 HTTP 面（管理端，AdminAuth 闸）。
// 五层归属：Handler（参数绑定+调 Service+统一响应出口），不含业务判断。
package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// OrderChangeController 改单申请单控制器
type OrderChangeController struct {
	svc *service.OrderChangeService
}

func NewOrderChangeController(svc *service.OrderChangeService) *OrderChangeController {
	return &OrderChangeController{svc: svc}
}

func (c *OrderChangeController) ready(ctx *gin.Context) bool {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "改单申请单服务未装配（InitOrderChangeRuntime 未执行）")
		return false
	}
	return true
}

func (c *OrderChangeController) operator(ctx *gin.Context) string {
	if v, ok := ctx.Get("username"); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v, ok := ctx.Get("user_id"); ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// List GET /api/order-change-requests?status=&page=
func (c *OrderChangeController) List(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	list, total, err := c.svc.List(ctx.Request.Context(), ctx.Query("status"), page, pageSize)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取改单申请列表失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Create POST /api/order-change-requests
func (c *OrderChangeController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var in service.OrderChangeCreateInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：platform/order_id/change_type/payload 为必填")
		return
	}
	rec, err := c.svc.Create(ctx.Request.Context(), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "创建改单申请失败")
		return
	}
	response.Success(ctx, rec, "创建成功")
}

// Get GET /api/order-change-requests/:id
func (c *OrderChangeController) Get(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	rec, err := c.svc.Get(ctx.Request.Context(), uint(id))
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取改单申请失败")
		return
	}
	response.Success(ctx, rec, "获取成功")
}

// Approve POST /api/order-change-requests/:id/approve
func (c *OrderChangeController) Approve(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	rec, err := c.svc.Approve(ctx.Request.Context(), uint(id), c.operator(ctx))
	if err != nil {
		replyCustomIntegrationError(ctx, err, "审批失败")
		return
	}
	response.Success(ctx, rec, "审批通过")
}

// Reject POST /api/order-change-requests/:id/reject
func (c *OrderChangeController) Reject(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = ctx.ShouldBindJSON(&body)
	rec, err := c.svc.Reject(ctx.Request.Context(), uint(id), c.operator(ctx), body.Reason)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "拒绝失败")
		return
	}
	response.Success(ctx, rec, "已拒绝")
}

// Execute POST /api/order-change-requests/:id/execute（按冻结的能力路由分派）
func (c *OrderChangeController) Execute(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	rec, err := c.svc.Execute(ctx.Request.Context(), uint(id), c.operator(ctx))
	if err != nil {
		replyCustomIntegrationError(ctx, err, "执行失败")
		return
	}
	response.Success(ctx, rec, "执行完成")
}

// Cancel POST /api/order-change-requests/:id/cancel
func (c *OrderChangeController) Cancel(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	rec, err := c.svc.Cancel(ctx.Request.Context(), uint(id), c.operator(ctx))
	if err != nil {
		replyCustomIntegrationError(ctx, err, "取消失败")
		return
	}
	response.Success(ctx, rec, "已取消")
}
