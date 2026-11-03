package controller

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// @Summary 坐席接管 AI 会话
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/takeover [post]
// Takeover 坐席接管 AI 会话
//
//	POST /api/customer-sessions/:id/takeover
//	body: {"reason": "AI 答非所问"}
func (c *CustomerSessionController) Takeover(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = ctx.ShouldBindJSON(&req)

	agentID := getUserIDFromContext(ctx)
	if agentID == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未登录或无权操作", "missing user_id")
		return
	}
	// 数据隔离：接管前先过可见性检查（未分配池/自己名下可接管，他人名下 403）
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	if err := c.sessionService.TakeoverByAgent(ctx.Request.Context(), &service.TakeoverRequest{
		SessionID: uint(id),
		AgentID:   agentID,
		Reason:    req.Reason,
	}); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"session_id":   id,
		"handler_type": "human",
	}, "接管成功")
}

// @Summary 坐席释放会话回 AI
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/release [post]
// Release 坐席释放会话回 AI
//
//	POST /api/customer-sessions/:id/release
func (c *CustomerSessionController) Release(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	agentID := getUserIDFromContext(ctx)
	if agentID == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未登录或无权操作", "missing user_id")
		return
	}
	// 数据隔离：释放前先过可见性检查
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	if err := c.sessionService.ReleaseToAI(ctx.Request.Context(), &service.ReleaseToAIRequest{
		SessionID: uint(id),
		AgentID:   agentID,
	}); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"session_id":   id,
		"handler_type": "ai",
	}, "释放成功")
}

// @Summary 统一 AI/人工切换接口
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/switch-handler [post]
// SwitchHandler 统一 AI/人工切换接口
//
//	POST /api/customer-sessions/:id/switch-handler
//	body: {"handler_type": "human" | "ai", "reason": "..."}
//
// 传 handler_type=human → 等价 Takeover；handler_type=ai → 等价 Release。
// 前端只需要一个按钮调一个接口，根据当前 handler_type 反向选择目标。
func (c *CustomerSessionController) SwitchHandler(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	var req struct {
		HandlerType string `json:"handler_type" binding:"required,oneof=ai human"`
		Reason      string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	agentID := getUserIDFromContext(ctx)
	if req.HandlerType == "human" && agentID == 0 {
		response.Error(ctx, http.StatusUnauthorized, "切人工时必须登录", "missing user_id")
		return
	}
	// 数据隔离：切换前先过可见性检查
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	if err := c.sessionService.SwitchHandler(ctx.Request.Context(), &service.SwitchHandlerRequest{
		SessionID:   uint(id),
		AgentID:     agentID,
		HandlerType: model.HandlerType(req.HandlerType),
		Reason:      req.Reason,
	}); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"session_id":   id,
		"handler_type": req.HandlerType,
	}, "切换成功")
}

// @Summary 坐席人工回复
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/reply [post]
// Reply 坐席人工回复访客
//
//	POST /api/customer-sessions/:id/reply
//	body: {"content": "..."}
//
// T1 线上验收补的口（BUG-1）：统一收件箱此前没有任何人工回复入口，
// SmartCSOrchestrator.AgentReply 有实现但从未挂路由。这里复用
// CustomerSessionService.SendMessage 的 agent 通道——它已经把
// 计数、首次人工回复时间、坐席日报与访客端 WebSocket 实时推送都做齐了，
// 不再造第二条发送路径。会话还在 AI 手上时先自动接管（与前端「回复即接管」
// 的直觉一致），再落人工回复。
func (c *CustomerSessionController) Reply(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：content 必填")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		response.Error(ctx, http.StatusBadRequest, "回复内容不能为空")
		return
	}
	if len([]rune(content)) > 5000 {
		response.Error(ctx, http.StatusBadRequest, "回复内容过长，最多 5000 字符")
		return
	}
	agentID := getUserIDFromContext(ctx)
	if agentID == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未登录或无权操作", "missing user_id")
		return
	}
	session, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx))
	if err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	// 仍在 AI/待处理态时先接管为人工，避免「人已经回了、会话还标 AI 处理中」的状态漂移。
	if session.HandlerType != model.HandlerTypeHuman {
		if err := c.sessionService.TakeoverByAgent(ctx.Request.Context(), &service.TakeoverRequest{
			SessionID: session.ID,
			AgentID:   agentID,
			Reason:    "agent_reply",
		}); err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
	}
	msg, err := c.sessionService.SendMessage(ctx.Request.Context(), &service.SendMessageRequest{
		SessionID:  session.SessionID,
		Content:    content,
		SenderType: "agent",
		SenderID:   strconv.FormatUint(uint64(agentID), 10),
		SenderName: ctx.GetString("username"),
	})
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"session_id": session.ID,
		"message":    msg,
	}, "回复成功")
}
