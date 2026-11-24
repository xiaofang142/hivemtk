package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ChannelAccountController 渠道账号管理。
// 列表/下拉一律走 ChannelAccountService.ListOptions（FR-029 唯一入口）。
type ChannelAccountController struct {
	svc service.ChannelAccountService
}

// NewChannelAccountController 创建渠道账号控制器。
func NewChannelAccountController(svc service.ChannelAccountService) *ChannelAccountController {
	return &ChannelAccountController{svc: svc}
}

// channelAccountSaveError 把渠道账号业务错误映射为 HTTP 状态（参数类 400 / 不存在 404）。
func channelAccountSaveError(ctx *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrChannelAccountInvalid) || errors.Is(err, service.ErrChannelAccountDataScopeInvalid) {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return true
	}
	if errors.Is(err, service.ErrChannelAccountNotFound) || errors.Is(err, service.ErrChannelNotFound) {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return true
	}
	response.ErrorFromDB(ctx, err, fallback)
	return true
}

func channelAccountListQuery(ctx *gin.Context) dto.ChannelAccountListQuery {
	q := dto.ChannelAccountListQuery{Keyword: ctx.Query("keyword")}
	if v := ctx.Query("channel_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			q.ChannelID = uint(id)
		}
	}
	if v := ctx.Query("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Status = &n
		}
	}
	return q
}

// @Summary 渠道账号列表
// @Tags channel-account
// @Router /api/channel-accounts [get]
func (c *ChannelAccountController) List(ctx *gin.Context) {
	c.Options(ctx)
}

// @Summary 渠道账号下拉（统一选择入口）
// @Tags channel-account
// @Router /api/channel-accounts/options [get]
func (c *ChannelAccountController) Options(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	rows, err := c.svc.ListOptions(ctx.Request.Context(), tid, channelAccountListQuery(ctx))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询渠道账号失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

func (c *ChannelAccountController) Get(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	row, err := c.svc.GetDetail(ctx.Request.Context(), tid, uint(id))
	if err != nil {
		if errors.Is(err, service.ErrChannelAccountNotFound) {
			response.Error(ctx, http.StatusNotFound, "渠道账号不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "查询渠道账号失败")
		return
	}
	response.Success(ctx, row, "ok")
}

func (c *ChannelAccountController) Create(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	var in dto.ChannelAccountSave
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	row, err := c.svc.Create(ctx.Request.Context(), tid, &in)
	if err != nil {
		channelAccountSaveError(ctx, err, "创建渠道账号失败")
		return
	}
	response.Success(ctx, row, "创建成功")
}

func (c *ChannelAccountController) Update(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	var in dto.ChannelAccountSave
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.Update(ctx.Request.Context(), tid, uint(id), &in); err != nil {
		channelAccountSaveError(ctx, err, "更新渠道账号失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

func (c *ChannelAccountController) Delete(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), tid, uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "删除渠道账号失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
