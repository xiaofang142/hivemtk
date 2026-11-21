package controller

import (
	"net/http"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CapabilityRuntimeController D01：能力运行视图只读接口
type CapabilityRuntimeController struct {
	svc *service.CapabilityRuntimeService
}

// NewCapabilityRuntimeController 构造
func NewCapabilityRuntimeController(svc *service.CapabilityRuntimeService) *CapabilityRuntimeController {
	return &CapabilityRuntimeController{svc: svc}
}

// List 全部登记能力的运行视图（implemented/configured/ready/reason_code 分层）
// GET /api/capabilities
func (ctrl *CapabilityRuntimeController) List(ctx *gin.Context) {
	response.Success(ctx, ctrl.svc.Describe(ctx.Request.Context()), "查询成功")
}

// ListCells 返回租户管理员可查询的注册表精确格，不读取店铺证据。
func (ctrl *CapabilityRuntimeController) ListCells(ctx *gin.Context) {
	response.Success(ctx, ctrl.svc.ListCellOptions(), "查询成功")
}

// ResolveCell 只读查询精确店铺动作格；当前无权威店铺证据源时明确返回阻塞原因。
func (ctrl *CapabilityRuntimeController) ResolveCell(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
	var req dto.CapabilityCellResolveRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return
	}
	result, err := ctrl.svc.ResolveCellSummary(ctx.Request.Context(), req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return
	}
	response.Success(ctx, result, "查询成功")
}
