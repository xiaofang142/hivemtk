package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// WeComHealthController 企微账号健康度控制器
type WeComHealthController struct {
	svc   *service.WeComAccountHealthService
	integ *service.WeComIntegrationService
}

// NewWeComHealthController 创建企微健康度控制器
func NewWeComHealthController(svc *service.WeComAccountHealthService, integ *service.WeComIntegrationService) *WeComHealthController {
	return &WeComHealthController{
		svc:   svc,
		integ: integ,
	}
}

// @Summary      上报成功
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/{id} [post]
// ReportHealth 上报健康度
func (c *WeComHealthController) ReportHealth(ctx *gin.Context) {
	var req service.ReportHealthRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	// 路由是 /accounts/:id，前端也按 path 传 id、body 只带健康字段；
	// 修前 service 只读 body 的 account_id ⇒ 拼出来的请求恒报 invalid account_id（实测）。
	if req.AccountID == 0 {
		if id, err := strconv.ParseUint(ctx.Param("id"), 10, 32); err == nil {
			req.AccountID = uint(id)
		}
	}
	if req.AccountID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}

	rec, err := c.svc.ReportHealth(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, rec, "上报成功")
}

// @Summary      拉取企微账号最新健康度
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/{id} [get]
// GetLatestHealth 拉取最新健康度
func (c *WeComHealthController) GetLatestHealth(ctx *gin.Context) {
	accID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}
	rec, err := c.svc.GetLatestHealth(ctx.Request.Context(), uint(accID))
	if err != nil {
		response.NotFound(ctx, "健康度记录不存在")
		return
	}
	response.Success(ctx, rec, "查询成功")
}

// @Summary      健康度历史
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/{id}/history [get]
// ListHealthHistory 健康度历史
func (c *WeComHealthController) ListHealthHistory(ctx *gin.Context) {
	accID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	list, total, err := c.svc.ListHealthHistory(ctx.Request.Context(), uint(accID), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary      查询企微风险账号列表
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/risks [get]
// GetRiskAccounts 风险账号
func (c *WeComHealthController) GetRiskAccounts(ctx *gin.Context) {
	list, err := c.svc.GetRiskAccounts(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, list, "查询成功")
}

// @Summary      无可用账号，请先在系统中配置企业微信账号
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/select [get]
// SelectHealthyAccount 选最优账号
//
// 降级语义：service 内部已实现三步降级（健康账号 → 降级账号 → ErrWeComAccountNotFound）。
// 到 controller 时 err != nil 意味着系统完全没有可用账号，这是正常状态（未配置），
// 应返回 code=0, data=null 而不是 NOT_FOUND_1002。
func (c *WeComHealthController) SelectHealthyAccount(ctx *gin.Context) {
	acc, err := c.svc.SelectHealthyAccount(ctx.Request.Context())
	if err != nil {

		response.Success(ctx, nil, "无可用账号，请先在系统中配置企业微信账号")
		return
	}
	response.Success(ctx, acc, "查询成功")
}

// @Summary      查询企微健康度概览
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/summary [get]
// GetHealthSummary 健康度概览
func (c *WeComHealthController) GetHealthSummary(ctx *gin.Context) {
	summary, err := c.svc.GetHealthSummary(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, summary, "查询成功")
}

// @Summary      消耗成功
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/{id}/quota/consume [post]
// ConsumeQuota 消耗配额
func (c *WeComHealthController) ConsumeQuota(ctx *gin.Context) {
	accID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}
	var req struct {
		Count int `json:"count" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.ConsumeQuota(ctx.Request.Context(), uint(accID), req.Count); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"count": req.Count}, "消耗成功")
}

// @Summary      重置成功
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/quota/reset [post]
// ResetDailyQuota 重置日配额
func (c *WeComHealthController) ResetDailyQuota(ctx *gin.Context) {
	n, err := c.svc.ResetDailyQuota(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"affected": n}, "重置成功")
}

// @Summary      接入成功
// @Tags      we-com-health
// @Router    /api/wecom/messages/ingest [post]
// IngestMessage 接入企微消息
func (c *WeComHealthController) IngestMessage(ctx *gin.Context) {
	var req service.IngestRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	hubMsg, conv, err := c.integ.IngestMessage(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"hub": hubMsg, "conversation": conv}, "接入成功")
}

// @Summary      发送企业微信消息
// @Tags      we-com-health
// @Router    /api/wecom/messages/send [post]
// SendMessage 发送消息
func (c *WeComHealthController) SendMessage(ctx *gin.Context) {
	var req service.WeComSendRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	hubMsg, err := c.integ.SendMessage(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, hubMsg, "发送成功")
}

// @Summary      列出企微账号与最新健康度（出参脱敏）
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts [get]
// ListAccountsWithHealth 列出账号与最新健康度
func (c *WeComHealthController) ListAccountsWithHealth(ctx *gin.Context) {
	list, err := c.integ.ListAccountsWithHealth(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	// 出参脱敏：这条列表带着完整账号（含 corp_secret / access_token 等密钥）。
	maskAccountWithHealth(list)
	response.Success(ctx, list, "查询成功")
}

// @Summary      更新企微账号登录状态
// @Tags      we-com-health
// @Router    /api/wecom/health/accounts/{id}/status [post]
// UpdateAccountStatus 更新账号状态
func (c *WeComHealthController) UpdateAccountStatus(ctx *gin.Context) {
	accID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号ID")
		return
	}
	var req struct {
		LoginState string `json:"login_state" binding:"required"`
		Risk       string `json:"risk"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.integ.UpdateAccountStatus(ctx.Request.Context(), uint(accID), req.LoginState, req.Risk); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": accID, "login_state": req.LoginState}, "更新成功")
}
