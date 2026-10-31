package controller

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// UnifiedMessageController 统一消息控制器
type UnifiedMessageController struct {
	messageService *service.UnifiedMessageService
}

// NewUnifiedMessageController 创建统一消息控制器实例
func NewUnifiedMessageController() *UnifiedMessageController {
	return &UnifiedMessageController{
		messageService: service.NewUnifiedMessageService(),
	}
}

// @Summary      分页查询统一消息列表（按平台筛选）
// @Tags      unified-message
// @Router    /api/messages [get]
// GetMessages 获取消息列表
func (c *UnifiedMessageController) GetMessages(ctx *gin.Context) {

	platform := ctx.Query("platform")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	messages, total, err := c.messageService.GetMessages(ctx.Request.Context(), platform, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, gin.H{
		"list":      messages,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      按 ID 获取消息详情
// @Tags      unified-message
// @Router    /api/messages/{id} [get]
// GetMessageByID 获取消息详情
func (c *UnifiedMessageController) GetMessageByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的消息ID")
		return
	}

	msg, err := c.messageService.GetMessageByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, msg, "获取成功")
}

// PlatformAccountController 平台账号控制器
type PlatformAccountController struct {
	accountService *service.PlatformAccountService
}

// NewPlatformAccountController 创建平台账号控制器实例
func NewPlatformAccountController() *PlatformAccountController {
	return &PlatformAccountController{
		accountService: service.NewPlatformAccountService(),
	}
}

// @Summary      获取平台账号列表
// @Tags      platform-account
// @Router    /api/platform-accounts [get]
// GetAccounts 获取平台账号列表
func (c *PlatformAccountController) GetAccounts(ctx *gin.Context) {

	// platform/account/status 是前端列表页的筛选参数，此前被整体忽略（见 service 注释）
	filter := service.PlatformAccountFilter{
		Platform: ctx.Query("platform"),
		Account:  ctx.Query("account"),
	}
	if st := ctx.Query("status"); st != "" {
		if n, err := strconv.Atoi(st); err == nil {
			filter.Status = &n
		}
	}
	accounts, err := c.accountService.GetAccountsFiltered(ctx.Request.Context(), filter)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, accounts, "获取成功")
}

// @Summary      按 ID 获取平台账号详情
// @Tags      platform-account
// @Router    /api/platform-accounts/{id} [get]
// GetAccountByID 获取平台账号详情
func (c *PlatformAccountController) GetAccountByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	account, err := c.accountService.GetAccountByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取平台账号") {
		return
	}

	response.Success(ctx, account, "获取成功")
}

// @Summary      创建平台账号
// @Tags      platform-account
// @Router    /api/platform-accounts [post]
// CreateAccount 创建平台账号
func (c *PlatformAccountController) CreateAccount(ctx *gin.Context) {

	var req service.CreatePlatformAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	account, err := c.accountService.CreateAccount(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, account, "创建成功")
}

// @Summary      更新平台账号
// @Tags      platform-account
// @Router    /api/platform-accounts/{id} [put]
// UpdateAccount 更新平台账号
func (c *PlatformAccountController) UpdateAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	var req service.UpdatePlatformAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	account, err := c.accountService.UpdateAccount(ctx.Request.Context(), uint(id), &req)
	if HandleDBError(ctx, err, "更新平台账号") {
		return
	}

	response.Success(ctx, account, "更新成功")
}

// @Summary      删除平台账号
// @Tags      platform-account
// @Router    /api/platform-accounts/{id} [delete]
// DeleteAccount 删除平台账号
func (c *PlatformAccountController) DeleteAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	if HandleDBError(ctx, c.accountService.DeleteAccount(ctx.Request.Context(), uint(id)), "删除平台账号") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary 登录平台账号
// @Tags      platform-account
// @Router    /api/platform-accounts/{id}/login [post]
// LoginAccount 登录平台账号
func (c *PlatformAccountController) LoginAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	var req service.PlatformLoginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	account, err := c.accountService.Login(ctx.Request.Context(), uint(id), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, account, "登录成功")
}

// @Summary 检查登录状态
// @Tags      platform-account
// @Router    /api/platform-accounts/{id}/status [get]
// CheckLoginStatus 检查登录状态
func (c *PlatformAccountController) CheckLoginStatus(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	status, err := c.accountService.CheckLoginStatus(ctx.Request.Context(), uint(id))
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, gin.H{
		"is_logged_in": status,
	}, "检查成功")
}

// @Summary      获取支持的平台静态清单
// @Tags      platform-account
// @Router    /api/platform-accounts/platforms [get]
// GetSupportedPlatforms 获取支持的平台列表
//
// 注：抖音/快手/小红书/咸鱼/tiktok 原为 CDP 无头浏览器自动回复通道（internal/platform
// 的 BrowserAdapter），该实现已删除。这些平台现由独立的桥接模块对接，此处仅返回平台
// 静态清单用于前端展示，不再依赖已删除的 platform 适配器注册表。
func (c *PlatformAccountController) GetSupportedPlatforms(ctx *gin.Context) {
	platforms := []model.Platform{
		model.PlatformDouyin,
		model.PlatformKuaishou,
		model.PlatformXiaohongshu,
		model.PlatformXianyu,
		model.PlatformTiktok,
	}

	result := make([]map[string]string, 0, len(platforms))
	platformNames := map[string]string{
		"douyin":      "抖音",
		"kuaishou":    "快手",
		"xiaohongshu": "小红书",
		"xianyu":      "闲鱼",
		"tiktok":      "TikTok",
	}

	for _, p := range platforms {
		result = append(result, map[string]string{
			"code": string(p),
			"name": platformNames[string(p)],
		})
	}

	response.Success(ctx, result, "获取成功")
}
