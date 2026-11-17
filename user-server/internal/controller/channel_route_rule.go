package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ChannelRouteRuleController 入站路由规则管理（specs/024 T305）。
//
// 租户来源只有一处：中间件写进 gin context 的 tenant_id（uint），
// 绝不从请求体读 tenant_id —— 那等于把租户隔离交给客户端声明。
type ChannelRouteRuleController struct {
	svc *service.ChannelRouteRuleService
}

func NewChannelRouteRuleController(svc *service.ChannelRouteRuleService) *ChannelRouteRuleController {
	return &ChannelRouteRuleController{svc: svc}
}

func (ctrl *ChannelRouteRuleController) tenantOf(ctx *gin.Context) string {
	raw, ok := ctx.Get(middleware.TenantContextKey)
	if !ok {
		return ""
	}
	id, _ := raw.(uint)
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(id), 10)
}

// List GET /api/channel-route-rules
func (ctrl *ChannelRouteRuleController) List(ctx *gin.Context) {
	tenant := ctrl.tenantOf(ctx)
	if tenant == "" {
		response.Error(ctx, http.StatusUnauthorized, "缺少租户归属，请重新登录")
		return
	}
	data, err := ctrl.svc.List(ctx.Request.Context(), tenant)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询路由规则失败")
		return
	}
	response.Success(ctx, data, "查询成功")
}

// Create POST /api/channel-route-rules
func (ctrl *ChannelRouteRuleController) Create(ctx *gin.Context) {
	tenant := ctrl.tenantOf(ctx)
	if tenant == "" {
		response.Error(ctx, http.StatusUnauthorized, "缺少租户归属，请重新登录")
		return
	}
	var req model.ChannelRouteRule
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型")
		return
	}
	if err := ctrl.svc.Create(ctx.Request.Context(), tenant, &req); err != nil {
		ctrl.writeErr(ctx, err, "创建路由规则失败")
		return
	}
	response.Success(ctx, req, "创建成功")
}

// Update PUT /api/channel-route-rules/:id
func (ctrl *ChannelRouteRuleController) Update(ctx *gin.Context) {
	tenant := ctrl.tenantOf(ctx)
	if tenant == "" {
		response.Error(ctx, http.StatusUnauthorized, "缺少租户归属，请重新登录")
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "规则 id 不合法")
		return
	}
	var req model.ChannelRouteRule
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型")
		return
	}
	req.ID = uint(id)
	if err := ctrl.svc.Update(ctx.Request.Context(), tenant, &req); err != nil {
		ctrl.writeErr(ctx, err, "更新路由规则失败")
		return
	}
	response.Success(ctx, req, "更新成功")
}

// Delete DELETE /api/channel-route-rules/:id
func (ctrl *ChannelRouteRuleController) Delete(ctx *gin.Context) {
	tenant := ctrl.tenantOf(ctx)
	if tenant == "" {
		response.Error(ctx, http.StatusUnauthorized, "缺少租户归属，请重新登录")
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "规则 id 不合法")
		return
	}
	if err := ctrl.svc.Delete(ctx.Request.Context(), tenant, uint(id)); err != nil {
		ctrl.writeErr(ctx, err, "删除路由规则失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}

func (ctrl *ChannelRouteRuleController) writeErr(ctx *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, service.ErrChannelRouteRuleNotFound):
		response.Error(ctx, http.StatusNotFound, "规则不存在")
	case errors.Is(err, service.ErrChannelRouteRuleInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	default:
		response.ErrorFromDB(ctx, err, fallback)
	}
}
