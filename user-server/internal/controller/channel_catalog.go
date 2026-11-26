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

// ChannelOperationsController 统一渠道目录与租户级运营操作。
// 只做参数绑定/错误映射，业务口径全部下沉 Service。
type ChannelOperationsController struct {
	catalogSvc service.ChannelCatalogService
	manageSvc  service.ChannelAccountManageService
	authSvc    *service.AccountAuthorizationService
}

func NewChannelOperationsController() *ChannelOperationsController {
	return &ChannelOperationsController{
		catalogSvc: service.NewChannelCatalogService(),
		manageSvc:  service.NewChannelAccountManageService(),
		authSvc:    service.NewAccountAuthorizationService(),
	}
}

func NewChannelOperationsControllerWithDeps(catalogSvc service.ChannelCatalogService, manageSvc service.ChannelAccountManageService, authSvc *service.AccountAuthorizationService) *ChannelOperationsController {
	return &ChannelOperationsController{catalogSvc: catalogSvc, manageSvc: manageSvc, authSvc: authSvc}
}

// @Summary 统一渠道目录（已启用渠道 + 渠道下已启用账号）
// @Tags channel
// @Router /api/channels/catalog [get]
func (c *ChannelOperationsController) Catalog(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	rows, err := c.catalogSvc.Catalog(ctx.Request.Context(), tid)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询渠道目录失败")
		return
	}
	response.Success(ctx, rows, "ok")
}

// @Summary 租户级渠道开关
// @Tags channel
// @Router /api/channels/{id}/tenant-enabled [put]
func (c *ChannelOperationsController) SetTenantEnabled(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, ok := channelIDParam(ctx)
	if !ok {
		return
	}
	var in dto.ChannelToggleSave
	if err := ctx.ShouldBindJSON(&in); err != nil || in.Enabled == nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.catalogSvc.SetChannelEnabledForTenant(ctx.Request.Context(), tid, id, *in.Enabled); err != nil {
		if errors.Is(err, service.ErrChannelNotFound) || errors.Is(err, service.ErrPresetChannelNotFound) {
			response.Error(ctx, http.StatusNotFound, "渠道不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "更新渠道开关失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

func managedListQuery(ctx *gin.Context) dto.ChannelAccountListQuery {
	q := channelAccountListQuery(ctx)
	if v := ctx.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if v := ctx.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			q.Offset = n
		}
	}
	return q
}

// @Summary 管理面账号列表（含停用账号）
// @Tags channel-account
// @Router /api/channel-accounts/managed [get]
func (c *ChannelOperationsController) ManagedList(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	rows, total, err := c.manageSvc.ListManaged(ctx.Request.Context(), tid, managedListQuery(ctx))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询账号列表失败")
		return
	}
	response.SuccessWithList(ctx, rows, total)
}

func (c *ChannelOperationsController) ManagedGet(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	row, err := c.manageSvc.GetDetail(ctx.Request.Context(), tid, uint(id))
	if err != nil {
		if errors.Is(err, service.ErrChannelAccountNotFound) {
			response.Error(ctx, http.StatusNotFound, "渠道账号不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "查询账号详情失败")
		return
	}
	response.Success(ctx, row, "ok")
}

func (c *ChannelOperationsController) ManagedUpdate(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	var in dto.ChannelAccountManagedSaveDTO
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	err = c.manageSvc.UpdateManaged(ctx.Request.Context(), tid, uint(id), &service.ChannelAccountManagedSave{
		ChannelID: in.ChannelID, Name: in.Name, Remark: in.Remark,
		SelfIdentifyName: in.SelfIdentifyName, Type: in.Type,
	})
	if err != nil {
		channelAccountSaveError(ctx, err, "更新账号失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

func (c *ChannelOperationsController) ManagedSetStatus(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	var in dto.ChannelAccountStatusSave
	if err := ctx.ShouldBindJSON(&in); err != nil || in.Status == nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.manageSvc.SetStatus(ctx.Request.Context(), tid, uint(id), *in.Status); err != nil {
		channelAccountSaveError(ctx, err, "更新账号状态失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// authorizationUserID 授权端点里的 :id 是被授权员工（SystemUser.ID）。
func authorizationUserID(ctx *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的员工 ID")
		return 0, false
	}
	return uint(id), true
}

func accountAuthorizationError(ctx *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, service.ErrAccountAuthorizationInvalid),
		errors.Is(err, service.ErrAccountAuthorizationTenantMismatch):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrAccountAuthorizationStoreUnavailable):
		response.Error(ctx, http.StatusServiceUnavailable, err.Error())
	default:
		response.ErrorFromDB(ctx, err, fallback)
	}
}

// @Summary 账号授权可选账号（当前租户）
// @Tags channel-account
// @Router /api/channel-accounts/authorization-options [get]
func (c *ChannelOperationsController) AuthorizationAvailable(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	rows, err := c.authSvc.AvailableAccounts(ctx.Request.Context(), ctx.Query("keyword"))
	if err != nil {
		accountAuthorizationError(ctx, err, "查询可授权账号失败")
		return
	}
	response.Success(ctx, rows, "ok")
}

// @Summary 读取某员工的账号授权配对
// @Tags channel-account
// @Router /api/channel-accounts/{id}/authorizations [get]
func (c *ChannelOperationsController) AuthorizationList(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	userID, ok := authorizationUserID(ctx)
	if !ok {
		return
	}
	rows, err := c.authSvc.List(ctx.Request.Context(), tid, userID)
	if err != nil {
		accountAuthorizationError(ctx, err, "查询账号授权失败")
		return
	}
	response.Success(ctx, rows, "ok")
}

// @Summary 覆盖式保存某员工的账号授权
// @Tags channel-account
// @Router /api/channel-accounts/{id}/authorizations [post]
func (c *ChannelOperationsController) AuthorizationSave(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	userID, ok := authorizationUserID(ctx)
	if !ok {
		return
	}
	var in dto.ChannelAuthorizationSave
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	pairs := make([]service.AccountAuthorizationPair, 0, len(in.Accounts))
	for _, item := range in.Accounts {
		pairs = append(pairs, service.AccountAuthorizationPair{
			AccountID: item.AccountID, Industries: item.Industries, Role: item.Role,
		})
	}
	rows, err := c.authSvc.Save(ctx.Request.Context(), tid, userID, pairs)
	if err != nil {
		accountAuthorizationError(ctx, err, "保存账号授权失败")
		return
	}
	response.Success(ctx, rows, "保存成功")
}

// @Summary 撤销某员工的单条账号授权
// @Tags channel-account
// @Router /api/channel-accounts/{id}/authorizations/{account_id} [delete]
func (c *ChannelOperationsController) AuthorizationRevoke(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	userID, ok := authorizationUserID(ctx)
	if !ok {
		return
	}
	accountID, err := strconv.ParseUint(ctx.Param("account_id"), 10, 32)
	if err != nil || accountID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道账号 ID")
		return
	}
	if err := c.authSvc.Revoke(ctx.Request.Context(), tid, userID, uint(accountID)); err != nil {
		accountAuthorizationError(ctx, err, "撤销账号授权失败")
		return
	}
	response.Success(ctx, nil, "撤销成功")
}
