package controller

// 收件箱人工回复口（2026-11-04 T1 线上验收 BUG-1 的收件箱侧入口）。
//
// 统一收件箱此前没有任何人工回复 UI/API：访客消息进得来，坐席的回复出不去
// （SmartCSOrchestrator.AgentReply 有实现但从未挂路由，message-hub push 对
// web_embed 也无人认领）。本口与 /api/customer-sessions/:id/reply 同一落点——
// 都走 CustomerSessionService.SendMessage 的 agent 通道（计数、首次人工回复
// 时间、坐席日报、访客 WebSocket 实时推送全在那一处）；区别只在寻址方式：
// 这里按收件箱会话主键（前端 currentConv.id）寻址，前端不用再换算 chat 主键。
//
// 数据隔离：收件箱侧 GetByIDScoped 过 assigned_to 口径，chat 会话侧再用
// GetSessionByIDStringScoped 复核一次（两层 scope 与 lock-human 同款）。

import (
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// Reply 收件箱人工回复
//
//	POST /api/inbox/conversations/:id/reply
//	body: {"content": "..."}
func (c *InboxController) Reply(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id == 0 {
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

	conv, err := c.svc.GetByIDScoped(ctx.Request.Context(), uint(id), inboxScopeFromCtx(ctx))
	if err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	if conv.ConversationID == "" {
		response.Error(ctx, http.StatusBadRequest, "该会话未关联客服会话，无法回复")
		return
	}

	sessionSvc := service.NewCustomerSessionService()
	session, err := sessionSvc.GetSessionByIDStringScoped(ctx.Request.Context(), conv.ConversationID, sessionScopeFromCtx(ctx))
	if err != nil {
		writeSessionScopedError(ctx, err, "客服会话不存在")
		return
	}
	// 仍在 AI/待处理态时先接管为人工，避免「人已经回了、会话还标 AI 处理中」的漂移。
	if session.HandlerType != model.HandlerTypeHuman {
		if err := sessionSvc.TakeoverByAgent(ctx.Request.Context(), &service.TakeoverRequest{
			SessionID: session.ID,
			AgentID:   agentID,
			Reason:    "inbox_reply",
		}); err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
	}
	msg, err := sessionSvc.SendMessage(ctx.Request.Context(), &service.SendMessageRequest{
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
		"conversation_id": conv.ID,
		"session_id":      session.ID,
		"message":         msg,
	}, "回复成功")
}
