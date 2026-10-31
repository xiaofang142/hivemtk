package controller

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// @Summary 拉黑当前会话对应的访客
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/blacklist [post]
// Blacklist 拉黑当前会话对应的访客
//
//	POST /api/customer-sessions/:id/blacklist
//	body: {"reason": "辱骂客服", "ttl_hours": 0}
func (c *CustomerSessionController) Blacklist(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	var req struct {
		Reason   string `json:"reason"`
		TTLHours int    `json:"ttl_hours"`
	}
	_ = ctx.ShouldBindJSON(&req)

	agentID := getUserIDFromContext(ctx)
	operatorName := ""
	if name, ok := ctx.Get("username"); ok {
		if s, ok := name.(string); ok {
			operatorName = s
		}
	}

	if err := c.sessionService.BlacklistUser(ctx.Request.Context(), &service.BlacklistRequest{
		SessionID:    uint(id),
		Reason:       req.Reason,
		OperatorID:   agentID,
		OperatorName: operatorName,
		TTLHours:     req.TTLHours,
	}); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"session_id":  id,
		"blacklisted": true,
	}, "拉黑成功")
}

// @Summary 解除拉黑
// @Tags      customer-session
// @Router    /api/customer-sessions/blacklist/remove [post]
// Unblacklist 解除拉黑
//
//	POST /api/customer-sessions/blacklist/remove
//	body: {"user_id": "u_123", "platform": "web"}
//
// 鉴权：要求登录态（JWT 中间件已保证），但操作者必须存在（agentID > 0），
// 避免未登录态/匿名 token 误调。
func (c *CustomerSessionController) Unblacklist(ctx *gin.Context) {
	var req struct {
		UserID   string         `json:"user_id" binding:"required"`
		Platform model.Platform `json:"platform"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			"请求参数错误：user_id 必填，platform 可选（默认 web）")
		return
	}
	if getUserIDFromContext(ctx) == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未登录或无权操作", "missing user_id")
		return
	}
	if err := c.sessionService.UnblacklistUser(ctx.Request.Context(), req.UserID, req.Platform); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, nil, "解除拉黑成功")
}

// @Summary      判断访客是否在会话黑名单
// @Tags      customer-session
// @Router    /api/customer-sessions/blacklist/check [get]
// IsUserBlacklisted 判断访客是否在黑名单
//
//	GET /api/customer-sessions/blacklist/check?user_id=u_123&platform=web
func (c *CustomerSessionController) IsUserBlacklisted(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "user_id 必填")
		return
	}
	platform := model.Platform(ctx.DefaultQuery("platform", "web"))
	ok, err := c.sessionService.IsUserBlacklisted(ctx.Request.Context(), userID, platform)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询黑名单失败")
		return
	}
	response.Success(ctx, gin.H{"blacklisted": ok}, "查询成功")
}

// @Summary      分页查询生效中的会话黑名单
// @Tags      customer-session
// @Router    /api/customer-sessions/blacklist [get]
// ListActiveBlacklist 分页查询生效中的黑名单
//
//	GET /api/customer-sessions/blacklist?page=1&page_size=20
func (c *CustomerSessionController) ListActiveBlacklist(ctx *gin.Context) {
	page, pageSize := sessionPaging(ctx, 20, 100)
	rows, total, err := c.sessionService.ListActiveBlacklist(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询黑名单失败")
		return
	}
	response.Success(ctx, gin.H{
		"list":      rows,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}
