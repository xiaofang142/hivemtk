package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// channelTenantID 渠道域 Controller 的统一租户上下文判据（缺租户一律拒绝）。
func channelTenantID(ctx *gin.Context) (uint, bool) {
	tid, ok := tenant.FromContext(ctx.Request.Context())
	if !ok || tid == 0 {
		response.Error(ctx, http.StatusForbidden, "缺少租户上下文")
		return 0, false
	}
	return tid, true
}

func channelIDParam(ctx *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道 ID")
		return 0, false
	}
	return uint(id), true
}

// ChannelController 渠道主数据管理（只回答"渠道本身是什么"）。
type ChannelController struct {
	svc       service.ChannelMasterService
	presetSvc service.ChannelService
}

// NewChannelController 创建渠道控制器。
func NewChannelController(svc service.ChannelMasterService) *ChannelController {
	return &ChannelController{svc: svc, presetSvc: service.NewChannelService()}
}

// NewChannelControllerWithPresets 用显式预设渠道服务构造（装配点/测试用）。
func NewChannelControllerWithPresets(svc service.ChannelMasterService, preset service.ChannelService) *ChannelController {
	return &ChannelController{svc: svc, presetSvc: preset}
}

// @Summary 渠道列表
// @Tags channel
// @Router /api/channels [get]
func (c *ChannelController) List(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	rows, err := c.svc.List(ctx.Request.Context(), tid, dto.ChannelListQuery{Keyword: ctx.Query("keyword")})
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询渠道失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

// @Summary 渠道下拉（预设渠道表驱动，仅启用中）
// @Tags channel
// @Router /api/channels/options [get]
func (c *ChannelController) Options(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	var rows []dto.ChannelOption
	var err error
	if c.presetSvc != nil {
		rows, err = c.presetSvc.EnabledOptions(ctx.Request.Context(), tid)
	} else {
		on := true
		rows, err = c.svc.List(ctx.Request.Context(), tid, dto.ChannelListQuery{Enabled: &on})
	}
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询渠道下拉失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

func (c *ChannelController) Get(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, ok := channelIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), tid, id)
	if err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			response.Error(ctx, http.StatusNotFound, "渠道不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "查询渠道失败")
		return
	}
	response.Success(ctx, row, "ok")
}

func (c *ChannelController) Create(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	var in dto.ChannelSave
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	row, err := c.svc.Create(ctx.Request.Context(), tid, &in)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建渠道失败")
		return
	}
	response.Success(ctx, row, "创建成功")
}

func (c *ChannelController) Update(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, ok := channelIDParam(ctx)
	if !ok {
		return
	}
	var in dto.ChannelSave
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.Update(ctx.Request.Context(), tid, id, &in); err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			response.Error(ctx, http.StatusNotFound, "渠道不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "更新渠道失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

func (c *ChannelController) Delete(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, ok := channelIDParam(ctx)
	if !ok {
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), tid, id); err != nil {
		response.ErrorFromDB(ctx, err, "删除渠道失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
