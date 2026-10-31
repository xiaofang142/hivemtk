package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type ChannelOverviewController struct {
	svc *service.ChannelOverviewService
}

// NewChannelOverviewController 创建概览控制器
func NewChannelOverviewController(svc *service.ChannelOverviewService) *ChannelOverviewController {
	return &ChannelOverviewController{svc: svc}
}

// @Summary      列出全部 13 个渠道的当前状态
// @Tags      channel-overview
// @Router    /api/channels/overview [get]
// Overview 列出所有 13 渠道的当前状态
// GET /api/channels/overview
func (ctrl *ChannelOverviewController) Overview(ctx *gin.Context) {
	response.Success(ctx, ctrl.svc.GetOverview(ctx.Request.Context()), "查询成功")
}

// BindChannel 绑定客户到某渠道（用于主动收集 OneID 信息）
// POST /api/channels/bind
func (ctrl *ChannelOverviewController) BindChannel(ctx *gin.Context) {
	var req dto.CustomerChannelBinding
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	data, err := ctrl.svc.BindChannel(ctx.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrChannelCustomerNotFound) {
			response.Error(ctx, http.StatusNotFound, "customer not found")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, data, "绑定成功")
}

// @Summary      列出客户的所有渠道绑定
// @Tags      channel-overview
// @Router    /api/channels/customer/{customer_id} [get]
// ListCustomerChannels 列出客户的所有渠道绑定
// GET /api/channels/customer/:customer_id
func (ctrl *ChannelOverviewController) ListCustomerChannels(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id required")
		return
	}

	data, err := ctrl.svc.ListCustomerChannels(ctx.Request.Context(), customerID)
	if err != nil {
		if errors.Is(err, service.ErrChannelCustomerNotFound) {
			response.Error(ctx, http.StatusNotFound, "customer not found")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, data, "查询成功")
}
