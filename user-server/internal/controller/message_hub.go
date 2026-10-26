package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// MessageHubController 消息中台控制器
type MessageHubController struct {
	svc *service.MessageHubService
}

// NewMessageHubController 创建消息中台控制器
func NewMessageHubController(svc *service.MessageHubService) *MessageHubController {
	return &MessageHubController{
		svc: svc,
	}
}

// @Summary      消息已存在(幂等)
// @Tags      message-hub
// @Router    /api/message-hub/push [post]
// Push 推送消息
func (c *MessageHubController) Push(ctx *gin.Context) {
	var req service.PushMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	msg, err := c.svc.Push(ctx.Request.Context(), &req)
	if err != nil {
		if err == service.ErrMessageHubIdempotent {
			response.Success(ctx, gin.H{"duplicate": true}, "消息已存在(幂等)")
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if msg != nil && msg.Direction == "inbound" {
		// 异步体继承请求租户：裸 context.Background() 在 on 模式下会让
		// ReopenOnInboundMessage / GetSession / 规则查询全部 fail-closed。
		bg := tenant.BackgroundWithTenant(ctx.Request.Context())
		go func(m *model.MessageHub) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			ctx2, cancel := context.WithTimeout(bg, 30*time.Second)
			defer cancel()
			chainSvc := service.NewSessionChainServiceFromGlobal()
			_, _ = chainSvc.ReopenOnInboundMessage(ctx2, m.ConversationID)
			sess, err := chainSvc.GetSession(ctx2, m.ConversationID)
			if err == nil && sess != nil {
				service.NewRuleEngineServiceFromGlobal().DispatchWithText(ctx2, service.RuleEventMessageInbound, m.ConversationID, m.Content, sess)
			}
		}(msg)
	}
	response.Success(ctx, msg, "推送成功")
}

// @Summary      批量推送完成
// @Tags      message-hub
// @Router    /api/message-hub/push-batch [post]
// PushBatch 批量推送
func (c *MessageHubController) PushBatch(ctx *gin.Context) {
	var reqs []service.PushMessageRequest
	if err := ctx.ShouldBindJSON(&reqs); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	results, errs := c.svc.PushBatch(ctx.Request.Context(), reqs)
	succeeded := 0
	for _, e := range errs {
		if e == nil {
			succeeded++
		}
	}
	// 字段名 success_count/failed_count，避免与信封级 success 布尔约定混淆
	resp := gin.H{
		"success_count": succeeded,
		"failed_count":  len(errs) - succeeded,
		"items":         results,
		"errors":        errs,
	}
	response.Success(ctx, resp, "批量推送完成")
}

// @Summary      列表
// @Tags      message-hub
// @Router    /api/message-hub/list [get]
// List 列表
func (c *MessageHubController) List(ctx *gin.Context) {
	_ = ctx
	q := service.ListQuery{

		Platform:       ctx.Query("platform"),
		AccountID:      ctx.Query("account_id"),
		ConversationID: ctx.Query("conversation_id"),
		SenderID:       ctx.Query("sender_id"),
		Direction:      ctx.Query("direction"),
		MsgType:        ctx.Query("msg_type"),
		Keyword:        ctx.Query("keyword"),
		OrderBy:        ctx.Query("order_by"),
	}
	if v := ctx.Query("is_read"); v != "" {
		b := v == "true" || v == "1"
		q.IsRead = &b
	}
	if v := ctx.Query("is_group"); v != "" {
		b := v == "true" || v == "1"
		q.IsGroup = &b
	}
	if v := ctx.Query("start_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.StartTime = &t
		}
	}
	if v := ctx.Query("end_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.EndTime = &t
		}
	}
	{
		p, ps, err := pagination.Parse(ctx)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		q.Page = p
		q.PageSize = ps
	}
	list, total, err := c.svc.List(ctx.Request.Context(), q)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(q.Page), int64(q.PageSize), total)
}

// @Summary      按 ID 查询统一消息详情
// @Tags      message-hub
// @Router    /api/message-hub/{id} [get]
// GetByID 详情
func (c *MessageHubController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	msg, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	if msg == nil {
		response.NotFound(ctx, "消息不存在")
		return
	}
	response.Success(ctx, msg, "获取成功")
}

// @Summary      撤回已发出的消息
// @Tags      message-hub
// @Router    /api/message-hub/{id}/recall [post]
// Recall 撤回一条出站消息（平台撤回 + 本地置终态，重复调用幂等）。
//
// 状态码口径（与 service 的分类一一对应，不合并成一句"失败"）：
//   - 200：撤掉了；或这一行本来就已经是终态（already_recalled=true）
//   - 400：入站行不是撤回对象（客户发来的消息我们没有平台凭据去删）
//   - 404：这一行不存在
//   - 409：这个渠道没有主动撤回通道（WA / 桥接渠道）或已过平台窗口 —— 等也没用
//   - 502：出网失败（网络/平台业务码拒绝/凭据缺失）—— 可能再试一次就行
//   - 503：撤回端口未装配
func (c *MessageHubController) Recall(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	res, err := c.svc.Recall(ctx.Request.Context(), uint(id))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMessageHubRowNotFound):
			response.NotFound(ctx, "消息不存在")
		case errors.Is(err, service.ErrMessageHubRecallInbound):
			response.Error(ctx, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrMessageHubRecallNotSupported),
			errors.Is(err, service.ErrMessageHubRecallWindowExpired):
			// 409 而不是 400：请求本身没写错，是"这个对象/这个时点做不到"。
			// 前端据此显示可解释文案（换人工去平台里删 / 窗口已过），
			// 而不是笼统的"参数错误"。
			response.Error(ctx, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrMessageHubRecallUnavailable):
			response.Error(ctx, http.StatusServiceUnavailable, err.Error())
		default:
			response.ErrorFromDB(ctx, err, err.Error())
		}
		return
	}
	response.Success(ctx, res, "撤回成功")
}

// @Summary      标记已读成功
// @Tags      message-hub
// @Router    /api/message-hub/{id}/read [post]
// MarkRead 标记已读
func (c *MessageHubController) MarkRead(ctx *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if err := c.svc.MarkRead(ctx.Request.Context(), req.IDs); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"updated": len(req.IDs)}, "标记已读成功")
}

// @Summary      统计成功
// @Tags      message-hub
// @Router    /api/message-hub/stats [get]
// Stats 统计
func (c *MessageHubController) Stats(ctx *gin.Context) {
	var start, end *time.Time
	if v := ctx.Query("start_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = &t
		}
	}
	if v := ctx.Query("end_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = &t
		}
	}
	stats, err := c.svc.GetStats(ctx.Request.Context(), start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, stats, "统计成功")
}

// @Summary      列出消息中心支持的平台
// @Tags      message-hub
// @Router    /api/message-hub/platforms [get]
// Platforms 列出支持平台
func (c *MessageHubController) Platforms(ctx *gin.Context) {
	response.Success(ctx, gin.H{
		"platforms": service.ListPlatforms(),
		"msg_types": service.ListMsgTypes(),
	}, "获取成功")
}

// @Summary      消息已存在(幂等)
// @Tags      message-hub
// @Router    /api/message-hub/push-from-channel [post]
// PushFromChannel 从渠道原始消息推送
func (c *MessageHubController) PushFromChannel(ctx *gin.Context) {
	var raw service.RawChannelMessage
	if err := ctx.ShouldBindJSON(&raw); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	req := c.svc.ConvertFromChannel(ctx.Request.Context(), &raw)
	msg, err := c.svc.Push(ctx.Request.Context(), req)
	if err != nil {
		if err == service.ErrMessageHubIdempotent {
			response.Success(ctx, gin.H{"duplicate": true}, "消息已存在(幂等)")
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, msg, "推送成功")
}
