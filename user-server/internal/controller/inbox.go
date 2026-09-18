package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// InboxController 统一收件箱控制器
type InboxController struct {
	svc *service.InboxService
}

// NewInboxController 创建统一收件箱控制器
func NewInboxController(svc *service.InboxService) *InboxController {
	return &InboxController{svc: svc}
}

// List 列表
func (c *InboxController) List(ctx *gin.Context) {
	q := service.InboxQuery{
		Platform:   ctx.Query("platform"),
		AccountID:  ctx.Query("account_id"),
		CustomerID: ctx.Query("customer_id"),
		Keyword:    ctx.Query("keyword"),
		Status:     ctx.Query("status"),
		AssignedTo: ctx.Query("assigned_to"),
		OrderBy:    ctx.Query("order_by"),
	}
	if v := ctx.Query("assigned_sop"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			q.AssignedSOP = uint(n)
		}
	}
	if v := ctx.Query("pinned"); v != "" {
		b := v == "true" || v == "1"
		q.Pinned = &b
	}
	if v := ctx.Query("starred"); v != "" {
		b := v == "true" || v == "1"
		q.Starred = &b
	}
	if v := ctx.Query("muted"); v != "" {
		b := v == "true" || v == "1"
		q.Muted = &b
	}
	q.Page, _ = strconv.Atoi(ctx.DefaultQuery("page", "1"))
	q.PageSize, _ = strconv.Atoi(ctx.DefaultQuery("page_size", "20"))

	list, total, err := c.svc.List(ctx.Request.Context(), q)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(q.Page), int64(q.PageSize), total)
}

// GetByID 详情
func (c *InboxController) GetByID(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	conv, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "会话不存在")
		return
	}
	response.Success(ctx, conv, "获取成功")
}

// MarkRead 标记已读
func (c *InboxController) MarkRead(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	if err := c.svc.MarkRead(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "标记已读成功")
}

// Pin 置顶
func (c *InboxController) Pin(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.Pin(ctx.Request.Context(), uint(id), req.Pinned); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"pinned": req.Pinned}, "操作成功")
}

// Star 标星
func (c *InboxController) Star(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req struct {
		Starred bool `json:"starred"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.Star(ctx.Request.Context(), uint(id), req.Starred); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"starred": req.Starred}, "操作成功")
}

// Mute 静音
func (c *InboxController) Mute(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req struct {
		Muted bool `json:"muted"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.Mute(ctx.Request.Context(), uint(id), req.Muted); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"muted": req.Muted}, "操作成功")
}

// AddTag 添加标签
func (c *InboxController) AddTag(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	var req struct {
		Tag string `json:"tag" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := c.svc.AddTag(ctx.Request.Context(), uint(id), req.Tag); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"tag": req.Tag}, "添加成功")
}

// RemoveTag 移除标签
func (c *InboxController) RemoveTag(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	tag := ctx.Param("tag")
	if tag == "" {
		response.Error(ctx, http.StatusBadRequest, "tag不能为空")
		return
	}
	if err := c.svc.RemoveTag(ctx.Request.Context(), uint(id), tag); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"tag": tag}, "移除成功")
}

// Assign 分配
func (c *InboxController) Assign(ctx *gin.Context) {
	var req struct {
		ConversationID uint   `json:"conversation_id" binding:"required"`
		Action         string `json:"action" binding:"required"`
		ToType         string `json:"to_type"`
		ToUserID       string `json:"to_user_id"`
		ToSOPID        uint   `json:"to_sop_id"`
		Remark         string `json:"remark"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	operatorID := ""
	if v, ok := ctx.Get("user_id"); ok {
		operatorID, _ = v.(string)
	}
	h, err := c.svc.Assign(ctx.Request.Context(), service.InboxAssignRequest{
		ConversationID: req.ConversationID,
		Action:         req.Action,
		ToType:         req.ToType,
		ToUserID:       req.ToUserID,
		ToSOPID:        req.ToSOPID,
		OperatorID:     operatorID,
		Remark:         req.Remark,
	})
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, h, "操作成功")
}

// AutoAssign 自动分配
func (c *InboxController) AutoAssign(ctx *gin.Context) {
	var req struct {
		ConversationID uint     `json:"conversation_id" binding:"required"`
		Candidates     []string `json:"candidates" binding:"required"`
		Mode           string   `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	operatorID := ""
	if v, ok := ctx.Get("user_id"); ok {
		operatorID, _ = v.(string)
	}
	var h any
	var err error

	switch mode, modeErr := service.ResolveAutoAssignMode(req.Mode); {
	case modeErr != nil:
		response.Error(ctx, http.StatusBadRequest, modeErr.Error())
		return
	case mode == service.StrategyRoundRobin:
		h, err = c.svc.RoundRobinAssign(ctx.Request.Context(), req.ConversationID, req.Candidates, operatorID)
	default:
		h, err = c.svc.AutoAssign(ctx.Request.Context(), req.ConversationID, req.Candidates, operatorID)
	}
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, h, "分配成功")
}

// Stats 统计
func (c *InboxController) Stats(ctx *gin.Context) {
	stats, err := c.svc.GetStats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, stats, "统计成功")
}

// Reconcile 收件箱对账 / 治理
//   - mode=unread（默认）：以 message_hub 最后一条消息为事实源，重算全部会话未读/状态（修正历史“AI 已处理却仍显示未读”）。
//   - mode=overdue：将“超时未响应”的会话转给在线人工坐席处理（否则默认由 AI 处理）。
//   - mode=backfill：修正 NULL/空 conversation_id 历史脏数据，并为缺失的会话补建 inbox_conversations（消除 sync_gap 缺口）。
func (c *InboxController) Reconcile(ctx *gin.Context) {
	mode := ctx.DefaultQuery("mode", service.ReconcileModeUnread)
	if mode != service.ReconcileModeUnread && mode != service.ReconcileModeOverdue && mode != service.ReconcileModeBackfill {
		response.Error(ctx, http.StatusBadRequest, "mode 仅支持 unread / overdue / backfill")
		return
	}
	res, err := c.svc.Reconcile(ctx.Request.Context(), mode)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, res, res.Message)
}

// ListAssignments 分配历史
func (c *InboxController) ListAssignments(ctx *gin.Context) {
	var convID uint
	if v := ctx.Query("conversation_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			convID = uint(n)
		}
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.ListAssignments(ctx.Request.Context(), convID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// GetMessages 拉取会话下的消息
func (c *InboxController) GetMessages(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.GetMessagesByConversation(ctx.Request.Context(), uint(id), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// DeleteMessage 删除统一收件箱会话中的单条消息
// DELETE /api/inbox/:id/messages/:mid?source=hub|session
func (c *InboxController) DeleteMessage(ctx *gin.Context) {
	conversationID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}
	mid, err := strconv.ParseUint(ctx.Param("mid"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的消息ID")
		return
	}
	source := ctx.Query("source")
	if source != "hub" && source != "session" {
		response.Error(ctx, http.StatusBadRequest, "无效的消息来源")
		return
	}
	if err := c.svc.DeleteMessage(ctx.Request.Context(), uint(conversationID), uint(mid), source); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"deleted": mid, "source": source}, "删除成功")
}

// StaffLoad 客服负载
func (c *InboxController) StaffLoad(ctx *gin.Context) {
	staff := ctx.Param("staff")
	load, err := c.svc.StaffLoad(ctx.Request.Context(), staff)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"staff": staff, "load": load}, "查询成功")
}

// InboxIngressController 渠道接入消息中台控制器
//
// 与 InboxController 分开，避免单个文件过大（5xx 行上限）。
// 端点：POST /api/chat/ingress （公开，不需要 JWT）
// 职责：把外部渠道事件转换为内部消息，落库 + 加锁路由
type InboxIngressController struct {
	svc *service.InboxIngressService
}

// NewInboxIngressController 创建入站控制器
func NewInboxIngressController(svc *service.InboxIngressService) *InboxIngressController {
	return &InboxIngressController{svc: svc}
}

// Ingress 渠道消息统一入口
func (c *InboxIngressController) Ingress(ctx *gin.Context) {
	var event model.MessageEvent
	if err := ctx.ShouldBindJSON(&event); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	result, err := c.svc.HandleIngressMessage(ctx.Request.Context(), &event)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, result, "消息入站成功")
}

// LockHuman 锁定会话为人工接管（内部 API，由转人工门禁调用）
func (c *InboxIngressController) LockHuman(ctx *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		Reason    string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if err := c.svc.LockSessionForHuman(ctx.Request.Context(), req.SessionID, req.Reason); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"session_id": req.SessionID, "locked": true}, "人工接管锁定成功")
}

// UnlockHuman 解除人工接管
func (c *InboxIngressController) UnlockHuman(ctx *gin.Context) {
	sessionID := ctx.Param("session_id")
	if sessionID == "" {
		response.Error(ctx, http.StatusBadRequest, "session_id 必填")
		return
	}
	if err := c.svc.UnlockSessionForHuman(ctx.Request.Context(), sessionID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"session_id": sessionID, "unlocked": true}, "解除人工接管成功")
}
