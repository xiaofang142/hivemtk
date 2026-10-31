package controller

import (
	"errors"
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

// inboxScopeFromCtx 从 JWT 上下文构造数据权限 scope（role/username 由 JWTAuthMiddleware 写入）
func inboxScopeFromCtx(ctx *gin.Context) service.InboxScope {
	return service.InboxScope{
		Role:     ctx.GetString("role"),
		Username: ctx.GetString("username"),
	}
}

// writeScopedError 统一映射数据权限相关错误：
//   - ErrInboxForbidden → 403
//   - ErrInboxConversationMissing → 404
//   - 其余 → ErrorFromDB
func writeScopedError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInboxForbidden):
		response.Error(ctx, http.StatusForbidden, "无权访问该会话")
	case errors.Is(err, service.ErrInboxConversationMissing):
		response.NotFound(ctx, "会话不存在")
	default:
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
	}
}

// @Summary      列表
// @Tags      inbox
// @Router    /api/inbox/conversations [get]
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

	list, total, err := c.svc.ListScoped(ctx.Request.Context(), q, inboxScopeFromCtx(ctx))
	if err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.SuccessWithPage(ctx, list, int64(q.Page), int64(q.PageSize), total)
}

// @Summary      查询收件箱会话详情
// @Tags      inbox
// @Router    /api/inbox/conversations/{id} [get]
// GetByID 详情
func (c *InboxController) GetByID(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	conv, err := c.svc.GetByIDScoped(ctx.Request.Context(), uint(id), inboxScopeFromCtx(ctx))
	if err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, conv, "获取成功")
}

// @Summary 标记已读
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/read [post]
// MarkRead 标记已读
func (c *InboxController) MarkRead(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	if err := c.svc.MarkReadScoped(ctx.Request.Context(), uint(id), inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"id": id}, "标记已读成功")
}

// @Summary      置顶收件箱会话
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/pin [post]
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
	if err := c.svc.PinScoped(ctx.Request.Context(), uint(id), req.Pinned, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"pinned": req.Pinned}, "操作成功")
}

// @Summary      标星收件箱会话
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/star [post]
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
	if err := c.svc.StarScoped(ctx.Request.Context(), uint(id), req.Starred, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"starred": req.Starred}, "操作成功")
}

// @Summary      静音收件箱会话
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/mute [post]
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
	if err := c.svc.MuteScoped(ctx.Request.Context(), uint(id), req.Muted, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"muted": req.Muted}, "操作成功")
}

// @Summary 添加标签
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/tags [post]
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
	if err := c.svc.AddTagScoped(ctx.Request.Context(), uint(id), req.Tag, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"tag": req.Tag}, "添加成功")
}

// @Summary 移除标签
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/tags/{tag} [delete]
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
	if err := c.svc.RemoveTagScoped(ctx.Request.Context(), uint(id), tag, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"tag": tag}, "移除成功")
}

// @Summary      分配会话给客服
// @Tags      inbox
// @Router    /api/inbox/assign [post]
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	// 数据隔离：conversation_id 走 body 寻址，而 DataScopeGuard 的登记表只认路径参数
	// （METHOD /path 里的 :param），这类口它扫不到。所以这一档必须自己过可见性检查，
	// 口径与 /inbox/conversations/:id/* 那批口完全一致（非超管只见 assigned_to=本人）。
	if _, err := c.svc.GetByIDScoped(ctx.Request.Context(), req.ConversationID, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
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

// @Summary 自动分配
// @Tags      inbox
// @Router    /api/inbox/assign/auto [post]
// AutoAssign 自动分配
func (c *InboxController) AutoAssign(ctx *gin.Context) {
	var req struct {
		ConversationID uint     `json:"conversation_id" binding:"required"`
		Candidates     []string `json:"candidates" binding:"required"`
		Mode           string   `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	// 数据隔离：同上，conversation_id 走 body 寻址，守卫登记表扫不到，
	// 自动分配前先过可见性检查，避免坐席把别人的会话抢走或改写归属。
	if _, err := c.svc.GetByIDScoped(ctx.Request.Context(), req.ConversationID, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
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

// @Summary 统计
// @Tags      inbox
// @Router    /api/inbox/stats [get]
// Stats 统计
func (c *InboxController) Stats(ctx *gin.Context) {
	stats, err := c.svc.GetStatsScoped(ctx.Request.Context(), inboxScopeFromCtx(ctx))
	if err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, stats, "统计成功")
}

// @Summary      收件箱对账 / 治理
// @Tags      inbox
// @Router    /api/inbox/reconcile [post]
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
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, res, res.Message)
}

// @Summary      分配历史
// @Tags      inbox
// @Router    /api/inbox/assignments [get]
// ListAssignments 分配历史
func (c *InboxController) ListAssignments(ctx *gin.Context) {
	var convID uint
	if v := ctx.Query("conversation_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			convID = uint(n)
		}
	}
	// 数据隔离：带 conversation_id 时按归属校验（与 /inbox/conversations/:id 同一口径）；
	// 不带 id 的"全量分配历史"只放超管——非超管拿它等于拿到全体坐席的会话清单与客户信息。
	if convID != 0 {
		if _, err := c.svc.GetByIDScoped(ctx.Request.Context(), convID, inboxScopeFromCtx(ctx)); err != nil {
			writeScopedError(ctx, err)
			return
		}
	} else if ctx.GetString("role") != model.SystemRoleCodeAdmin {
		response.Error(ctx, http.StatusForbidden, "无权查看全量分配历史")
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.ListAssignments(ctx.Request.Context(), convID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary      拉取会话下的消息
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/messages [get]
// GetMessages 拉取会话下的消息
func (c *InboxController) GetMessages(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	list, total, err := c.svc.GetMessagesByConversationScoped(ctx.Request.Context(), uint(id), page, pageSize, inboxScopeFromCtx(ctx))
	if err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary      删除收件箱会话中的单条消息
// @Tags      inbox
// @Router    /api/inbox/conversations/{id}/messages/{mid} [delete]
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
	if err := c.svc.DeleteMessageScoped(ctx.Request.Context(), uint(conversationID), uint(mid), source, inboxScopeFromCtx(ctx)); err != nil {
		writeScopedError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"deleted": mid, "source": source}, "删除成功")
}

// @Summary      查询客服负载
// @Tags      inbox
// @Router    /api/inbox/staff/{staff}/load [get]
// StaffLoad 客服负载
//
// 这条口读的是"某个坐席手上还有几会话"，属于同事的工作量数据，不是调用者自己的行，
// 所以行级守卫（DataScopeGuard 按行的归属列判）在这里没有格子：参数本身就是归属。
// 口径与 InboxScope 同源——非超管只能查自己名下的负载，别人的要 403。
func (c *InboxController) StaffLoad(ctx *gin.Context) {
	staff := ctx.Param("staff")
	if ctx.GetString("role") != model.SystemRoleCodeAdmin {
		me := ctx.GetString("username")
		if me == "" || staff != me {
			response.Error(ctx, http.StatusForbidden, "无权查看他人的负载")
			return
		}
	}
	load, err := c.svc.StaffLoad(ctx.Request.Context(), staff)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"staff": staff, "load": load}, "查询成功")
}

// @Summary      可分配坐席名单（脱敏四列）
// @Tags      inbox
// @Router    /api/inbox/agents [get]
// Agents 可分配坐席名单（脱敏四列）
//
// 收件箱的分配下拉与自动分配候选都从这里取：它随工作台一起授予坐席，
// 不再让坐席去碰仅超管的用户清单口（那条口回整行账号资料，本就不该给工作台用）。
func (c *InboxController) Agents(ctx *gin.Context) {
	agents, err := c.svc.ListAssignableAgents(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.SuccessWithList(ctx, agents, int64(len(agents)))
}

// InboxIngressController 渠道接入消息中台控制器
//
// 与 InboxController 分开，避免单个文件过大（5xx 行上限）。
// 端点：POST /api/chat/ingress （公开，不需要 JWT）
// 职责：把外部渠道事件转换为内部消息，落库 + 加锁路由
type InboxIngressController struct {
	svc        *service.InboxIngressService
	sessionSvc *service.CustomerSessionService
}

// NewInboxIngressController 创建入站控制器
func NewInboxIngressController(svc *service.InboxIngressService) *InboxIngressController {
	return &InboxIngressController{svc: svc}
}

// SetSessionService 注入客服会话服务。
//
// lock-human 按 body 里的 session_id 寻址，DataScopeGuard 的登记表只认路径参数、
// 扫不到它（对称的 unlock-human/:session_id 反而在登记表里被守着，这一侧敞着）。
// 于是这一档必须在 controller 里自己过一遍服务层 scope，口径与
// /customer-sessions/:id/* 那批口完全一致。
func (c *InboxIngressController) SetSessionService(svc *service.CustomerSessionService) {
	c.sessionSvc = svc
}

// @Summary 渠道消息统一入口
// @Tags      inbox-ingress
// @Router    /api/chat/ingress [post]
// Ingress 渠道消息统一入口
func (c *InboxIngressController) Ingress(ctx *gin.Context) {
	var event model.MessageEvent
	if err := ctx.ShouldBindJSON(&event); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	result, err := c.svc.HandleIngressMessage(ctx.Request.Context(), &event)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, result, "消息入站成功")
}

// @Summary 锁定会话为人工接管（内部 API，由转人工硬闸调用）
// @Tags      inbox-ingress
// @Router    /api/inbox/lock-human [post]
// LockHuman 锁定会话为人工接管（内部 API，由转人工硬闸调用）
func (c *InboxIngressController) LockHuman(ctx *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		Reason    string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	// 数据隔离：未装配会话服务时 fail-closed（与 DataScopeGuard"没装配查询源就 403"同一哲学），
	// 装配了才按服务层 scope 校验归属——否则任意登录用户能给别人的会话上人工锁，
	// 把坐席 A 的会话 AI 回复全掐掉（跨租户干扰）。
	if c.sessionSvc == nil {
		response.Error(ctx, http.StatusForbidden, "人工接管服务未装配，已按 fail-closed 拒绝")
		return
	}
	if _, err := c.sessionSvc.GetSessionByIDStringScoped(ctx.Request.Context(), req.SessionID, sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}
	if err := c.svc.LockSessionForHuman(ctx.Request.Context(), req.SessionID, req.Reason); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"session_id": req.SessionID, "locked": true}, "人工接管锁定成功")
}

// @Summary 解除人工接管
// @Tags      inbox-ingress
// @Router    /api/inbox/unlock-human/{session_id} [post]
// UnlockHuman 解除人工接管
func (c *InboxIngressController) UnlockHuman(ctx *gin.Context) {
	sessionID := ctx.Param("session_id")
	if sessionID == "" {
		response.Error(ctx, http.StatusBadRequest, "session_id 必填")
		return
	}
	if err := c.svc.UnlockSessionForHuman(ctx.Request.Context(), sessionID); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"session_id": sessionID, "unlocked": true}, "解除人工接管成功")
}
