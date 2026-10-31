package controller

import (
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// WeComController 企业微信控制器
type WeComController struct {
	wecomService *service.WeComService
}

// NewWeComController 创建企业微信控制器实例
//
// 测试场景下传入 nil 时自动构造默认服务（依赖 dbUtil.GetDB()，
// 由 testutil.NewTestDB + db.SetTestDB 设置全局 DB）。
func NewWeComController(wecomService *service.WeComService) *WeComController {
	if wecomService == nil {
		wecomService = service.NewWeComService()
	}
	return &WeComController{
		wecomService: wecomService,
	}
}

// @Summary      创建企业微信账号（出参脱敏）
// @Tags      we-com
// @Router    /api/wecom/accounts [post]

// respondWeComError 把企微渠道错误与本地错误分开。
//
// 企微 API 的 errcode/errmsg 是**上游**失败（凭据错/频率限制/接口下线），
// 修前一律走 ErrorFromDB 落 500「内部错误」，运维看不出该改凭据还是查本机；
// 统一 502 + 渠道原文，与 QQ/TG 的 502 口径一致。
func respondWeComError(ctx *gin.Context, err error, fallback string) {
	if err == nil {
		return
	}
	if service.AsChannelError(err) != nil {
		response.ErrorWithCode(ctx, utils.ErrorCodeUpstreamFailure, http.StatusBadGateway, err.Error())
		return
	}
	response.ErrorFromDB(ctx, err, fallback, err.Error())
}

// CreateAccount 创建企业微信账号
func (c *WeComController) CreateAccount(ctx *gin.Context) {

	var req service.CreateAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	account, err := c.wecomService.CreateAccount(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	// 出参脱敏：账号带着 corp_secret / agent_secret / access_token / callback_token /
	// encoding_aes_key，直接序列化就是把商户的企业微信应用密钥发给了前端。
	maskWeComAccount(account)
	response.Success(ctx, account, "创建成功")
}

// @Summary      获取企业微信账号列表
// @Tags      we-com
// @Router    /api/wecom/accounts [get]
// GetAccountList 获取企业微信账号列表
func (c *WeComController) GetAccountList(ctx *gin.Context) {

	accounts, err := c.wecomService.GetAccountList(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	maskWeComAccounts(accounts)
	response.Success(ctx, accounts, "获取成功")
}

// @Summary      获取企业微信账号详情
// @Tags      we-com
// @Router    /api/wecom/accounts/{id} [get]
// GetAccountByID 获取企业微信账号详情
func (c *WeComController) GetAccountByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	maskWeComAccount(account)
	response.Success(ctx, account, "获取成功")
}

// @Summary      更新企业微信账号
// @Tags      we-com
// @Router    /api/wecom/accounts/{id} [put]
// UpdateAccount 更新企业微信账号
func (c *WeComController) UpdateAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	var req service.CreateAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	account, err := c.wecomService.UpdateAccount(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	maskWeComAccount(account)
	response.Success(ctx, account, "更新成功")
}

// @Summary      删除企业微信账号
// @Tags      we-com
// @Router    /api/wecom/accounts/{id} [delete]
// DeleteAccount 删除企业微信账号
func (c *WeComController) DeleteAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	if err := c.wecomService.DeleteAccount(ctx.Request.Context(), uint(id)); err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary      同步企业微信客户
// @Tags      we-com
// @Router    /api/wecom/accounts/{id}/sync-customers [post]
// SyncCustomers 同步企业微信客户
func (c *WeComController) SyncCustomers(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.wecomService.SyncCustomers(ctx.Request.Context(), account)
	if err != nil {
		respondWeComError(ctx, err, "同步客户失败")
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}

// @Summary      获取企业微信客户列表
// @Tags      we-com
// @Router    /api/wecom/customers [get]
// GetCustomerList 获取企业微信客户列表
func (c *WeComController) GetCustomerList(ctx *gin.Context) {

	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	customers, total, err := c.wecomService.GetCustomerList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, gin.H{
		"list":      customers,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      同步企业微信客户群
// @Tags      we-com
// @Router    /api/wecom/accounts/{id}/sync-groups [post]
// SyncGroups 同步企业微信客户群
func (c *WeComController) SyncGroups(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.wecomService.SyncGroups(ctx.Request.Context(), account)
	if err != nil {
		respondWeComError(ctx, err, "同步群失败")
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}

// @Summary      获取企业微信客户群列表
// @Tags      we-com
// @Router    /api/wecom/groups [get]
// GetGroupList 获取企业微信客户群列表
func (c *WeComController) GetGroupList(ctx *gin.Context) {

	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	groups, total, err := c.wecomService.GetGroupList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, gin.H{
		"list":      groups,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      发送企业微信消息
// @Tags      we-com
// @Router    /api/wecom/accounts/{id}/send-message [post]
// SendMessage 发送企业微信消息
func (c *WeComController) SendMessage(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	var req service.WeComSendMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	msgID, err := c.wecomService.SendMessage(ctx.Request.Context(), account, &req)
	if err != nil {
		respondWeComError(ctx, err, "发送失败")
		return
	}

	response.Success(ctx, gin.H{"msg_id": msgID}, "发送成功")
}

// @Summary 刷新 access_token（清空本地缓存，强制向企业微信重新换取）
// @Tags      we-com
// @Router    /api/wecom/accounts/{id}/refresh [post]
// RefreshAccount 刷新 access_token（清空本地缓存，强制向企业微信重新换取）
func (c *WeComController) RefreshAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	account.AccessToken = ""
	account.TokenExpires = time.Time{}
	if _, err := c.wecomService.GetAccessToken(ctx.Request.Context(), account); err != nil {
		respondWeComError(ctx, err, "刷新失败")
		return
	}

	account, err = c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "刷新后回查账号失败："+err.Error())
		return
	}
	maskWeComAccount(account)
	response.Success(ctx, account, "刷新成功")
}

// @Summary      获取企业微信消息列表
// @Tags      we-com
// @Router    /api/wecom/messages [get]
// GetMessageList 获取企业微信消息列表
func (c *WeComController) GetMessageList(ctx *gin.Context) {

	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	messages, total, err := c.wecomService.GetMessageList(ctx.Request.Context(), page, pageSize)
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

// @Summary      获取企业微信标签列表
// @Tags      we-com
// @Router    /api/wecom/tags [get]
// GetTagList 获取企业微信标签列表
func (c *WeComController) GetTagList(ctx *gin.Context) {

	tags, err := c.wecomService.GetTagList(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, tags, "获取成功")
}

// @Summary      同步企业微信标签
// @Tags      we-com
// @Router    /api/wecom/accounts/{id}/sync-tags [post]
// SyncTags 同步企业微信标签
func (c *WeComController) SyncTags(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.wecomService.GetAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.wecomService.SyncTags(ctx.Request.Context(), account)
	if err != nil {
		respondWeComError(ctx, err, "同步标签失败")
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}
