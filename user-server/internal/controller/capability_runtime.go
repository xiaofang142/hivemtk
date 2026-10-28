package controller

import (
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
