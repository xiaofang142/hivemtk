package controller

import (
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CustomerSessionController 客服会话控制器
type CustomerSessionController struct {
	sessionService *service.CustomerSessionService
}

// NewCustomerSessionController 创建客服会话控制器实例
func NewCustomerSessionController() *CustomerSessionController {
	return &CustomerSessionController{
		sessionService: service.NewCustomerSessionService(),
	}
}

func getUserIDFromContext(ctx *gin.Context) uint {
	if uid, exists := ctx.Get("user_id"); exists {
		switch v := uid.(type) {
		case uint:
			return v
		case float64:
			return uint(v)
		case int:
			return uint(v)
		case int64:
			return uint(v)
		}
	}
	return 0
}

// sessionScopeFromCtx 从登录态组装数据隔离身份（role + user_id）。
func sessionScopeFromCtx(ctx *gin.Context) service.SessionScope {
	return service.SessionScope{
		Role:   ctx.GetString("role"),
		UserID: getUserIDFromContext(ctx),
	}
}

// writeSessionScopedError 数据隔离错误映射：越权 403，其余交回通用错误口径
// （not found → 404，DB 错 → 500），与隔离前的响应形态保持一致。
func writeSessionScopedError(ctx *gin.Context, err error, notFoundMsg string) {
	if errors.Is(err, service.ErrSessionForbidden) {
		response.Error(ctx, http.StatusForbidden, err.Error())
		return
	}
	// 先把「查无此条」拦下来，交通用 HandleServiceError 只会得到笼统的「记录不存在」，
	// 调用方给的 notFoundMsg 才是带 id 的可定位文案。
	if errhttp.IsNotFoundError(err) {
		response.NotFound(ctx, notFoundMsg)
		return
	}
	if !HandleServiceError(ctx, err) {
		response.Error(ctx, http.StatusNotFound, notFoundMsg)
	}
}

// isKnownSessionStatus 会话状态白名单（与 model.SessionStatus* 常量一致）。
func isKnownSessionStatus(s model.SessionStatus) bool {
	switch s {
	case model.SessionStatusPending, model.SessionStatusAIHandling, model.SessionStatusHumanHandling,
		model.SessionStatusWaiting, model.SessionStatusResolved, model.SessionStatusClosed:
		return true
	}
	return false
}

// sessionPaging 会话域统一分页归一化：page<1→1、page_size<1→默认值、超过上限按上限。
// 必须在回显前归一化，否则回显值与仓储实际取数不一致（前端翻页会错位）。
func sessionPaging(ctx *gin.Context, defSize, maxSize int) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", strconv.Itoa(defSize)))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defSize
	}
	if maxSize > 0 && pageSize > maxSize {
		pageSize = maxSize
	}
	return page, pageSize
}

// @Summary      获取会话列表
// @Tags      customer-session
// @Router    /api/customer-sessions [get]
// GetSessions 获取会话列表
func (c *CustomerSessionController) GetSessions(ctx *gin.Context) {
	status := model.SessionStatus(strings.TrimSpace(ctx.Query("status")))
	if status != "" && !isKnownSessionStatus(status) {
		response.Error(ctx, http.StatusBadRequest, "未知的会话状态: "+string(status)+
			"（可用: pending / ai_handling / human_handling / waiting / resolved / closed）")
		return
	}
	page, pageSize := sessionPaging(ctx, 20, 100)

	sessions, total, err := c.sessionService.GetSessionsScoped(ctx.Request.Context(), status, page, pageSize, sessionScopeFromCtx(ctx))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取会话列表失败")
		return
	}

	response.Success(ctx, gin.H{
		"list":      sessions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      获取会话详情
// @Tags      customer-session
// @Router    /api/customer-sessions/{id} [get]
// GetSessionByID 获取会话详情
func (c *CustomerSessionController) GetSessionByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}

	session, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx))
	if err != nil {
		writeSessionScopedError(ctx, err, fmt.Sprintf("会话不存在（id=%s）", idStr))
		return
	}

	response.Success(ctx, session, "获取成功")
}

// @Summary      创建会话
// @Tags      customer-session
// @Router    /api/customer-sessions [post]
// CreateSession 创建会话
func (c *CustomerSessionController) CreateSession(ctx *gin.Context) {
	var req service.CreateSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			"请求参数错误：platform、account_id、user_id 均为必填，one_id / user_name / user_avatar / user_phone / user_email 可选")
		return
	}

	session, err := c.sessionService.CreateSession(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "创建会话") {
		return
	}

	response.Success(ctx, session, "创建成功")
}

// @Summary      获取会话消息
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/messages [get]
// GetMessages 获取会话消息
func (c *CustomerSessionController) GetMessages(ctx *gin.Context) {
	idStr := ctx.Param("id")
	if idStr == "" {
		idStr = ctx.Param("session_id")
	}

	sessionID := idStr
	if id, err := strconv.ParseUint(idStr, 10, 32); err == nil {
		session, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx))
		if err != nil {
			writeSessionScopedError(ctx, err, fmt.Sprintf("会话不存在（id=%s）", idStr))
			return
		}
		sessionID = session.SessionID
	} else {
		// 入口给的是字符串 session_id：数字 id 那条路已挡，字符串这条路同样过可见性检查。
		if _, err := c.sessionService.GetSessionByIDStringScoped(ctx.Request.Context(), sessionID, sessionScopeFromCtx(ctx)); err != nil {
			writeSessionScopedError(ctx, err, fmt.Sprintf("会话不存在（session_id=%s）", sessionID))
			return
		}
	}

	page, pageSize := sessionPaging(ctx, 50, 200)

	messages, total, err := c.sessionService.GetMessages(ctx.Request.Context(), sessionID, page, pageSize)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, gin.H{
		"list":      messages,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary      发送消息
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/messages [post]
// SendMessage 发送消息
func (c *CustomerSessionController) SendMessage(ctx *gin.Context) {
	var req service.SendMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			"请求参数错误：content 必填，content_type / media_url / sender_type / sender_name 等可选（发送人身份由登录态决定）")
		return
	}

	if channelID, ok := ctx.Get("chat_channel_id"); ok {
		req.SenderType = "user"
		switch v := channelID.(type) {
		case uint:
			req.SenderID = strconv.FormatUint(uint64(v), 10)
		case string:
			req.SenderID = v
		default:
			req.SenderID = ""
		}
		req.SenderName = ""
	} else if uid := getUserIDFromContext(ctx); uid != 0 {
		req.SenderType = "agent"
		req.SenderID = strconv.FormatUint(uint64(uid), 10)
		if name, ok := ctx.Get("username"); ok {
			if s, ok := name.(string); ok {
				req.SenderName = s
			}
		}
	} else {
		response.Error(ctx, http.StatusUnauthorized, "未登录或无权访问", "missing auth context")
		return
	}

	if req.SenderType == "" {
		req.SenderType = "user"
	}

	if req.SessionID == "" {
		idStr := ctx.Param("id")
		if idStr == "" {
			response.Error(ctx, http.StatusBadRequest, "请求参数错误: session_id is required")
			return
		}
		req.SessionID = idStr
		if id, err := strconv.ParseUint(idStr, 10, 32); err == nil {
			session, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx))
			if err != nil {
				writeSessionScopedError(ctx, err, fmt.Sprintf("会话不存在（id=%s）", idStr))
				return
			}
			req.SessionID = session.SessionID
		}
	}
	// 数据隔离：无论入口给的是数字 id 还是字符串 session_id，发消息前统一过可见性检查
	// （不可见 → 403；不存在 → 404，与服务层原口径一致）。
	if _, err := c.sessionService.GetSessionByIDStringScoped(ctx.Request.Context(), req.SessionID, sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, fmt.Sprintf("会话不存在（session_id=%s）", req.SessionID))
		return
	}

	message, err := c.sessionService.SendMessage(ctx.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrCustomerSessionNotFound) {
			response.Error(ctx, http.StatusNotFound, "会话不存在（session_id="+req.SessionID+"）")
			return
		}
		var notSendable *service.SessionNotSendableError
		if errors.As(err, &notSendable) {
			response.Error(ctx, http.StatusBadRequest, notSendable.Error())
			return
		}
		HandleDBError(ctx, err, "发送消息")
		return
	}

	response.Success(ctx, message, "发送成功")
}

// @Summary      更新会话状态
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/status [put]
// UpdateSessionStatus 更新会话状态
func (c *CustomerSessionController) UpdateSessionStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}

	var req struct {
		Status model.SessionStatus `json:"status" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			"请求参数错误：status 必填（会话状态值，如 pending / ai_handling / waiting / active / closed）")
		return
	}

	// 数据隔离：改状态前先过可见性检查（不可见 → 403）
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}

	if err := c.sessionService.UpdateSessionStatus(ctx.Request.Context(), uint(id), req.Status); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "更新成功")
}

// @Summary 评价会话
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/rate [post]
// RateSession 评价会话
func (c *CustomerSessionController) RateSession(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}

	var req struct {
		Rating  int    `json:"rating" binding:"required,min=1,max=5"`
		Comment string `json:"comment"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			"请求参数错误：rating 必填，取值 1-5 的整数；comment 可选")
		return
	}

	// 数据隔离：评价前先过可见性检查
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}

	if err := c.sessionService.RateSession(ctx.Request.Context(), uint(id), req.Rating, req.Comment); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "评价成功")
}

// @Summary 标记会话
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/tags [post]
// TagSession 标记会话
func (c *CustomerSessionController) TagSession(ctx *gin.Context) {
	// 这里吃的是会话主键（customer_sessions.id，数字），不是业务号 session_id
	// （sess_<纳秒>_<随机>）。前端 useSessionList.js 传的是 currentSession.value.id。
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID：这里需要会话主键（customer_sessions.id，数字），不是 session_id 字符串")
		return
	}

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, `请求参数错误：tags 必须是非空字符串数组，例如 {"tags": ["vip"]}（传空数组表示清空标签）`)
		return
	}

	// 数据隔离：标记前先过可见性检查。
	// 不走 writeSessionScopedError：它内部交 HandleServiceError，"不存在"会被压成
	// 笼统的「记录不存在」，这里要带上具体 id 才能定位是哪条会话。
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		if errors.Is(err, service.ErrSessionForbidden) {
			response.Error(ctx, http.StatusForbidden, err.Error())
			return
		}
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("会话不存在（id=%d）", id))
			return
		}
		response.ErrorFromDB(ctx, err, "获取会话失败")
		return
	}

	if err := c.sessionService.TagSession(ctx.Request.Context(), uint(id), req.Tags); err != nil {
		response.ErrorFromDB(ctx, err, "标记会话标签失败")
		return
	}

	response.Success(ctx, nil, "标记成功")
}

// @Summary      获取待处理会话
// @Tags      customer-session
// @Router    /api/customer-sessions/pending [get]
// GetPendingSessions 获取待处理会话
func (c *CustomerSessionController) GetPendingSessions(ctx *gin.Context) {
	page, pageSize := sessionPaging(ctx, 20, 100)

	sessions, err := c.sessionService.GetPendingSessionsScoped(ctx.Request.Context(), page, pageSize, sessionScopeFromCtx(ctx))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取待处理会话失败")
		return
	}

	total, _ := c.sessionService.CountPendingSessionsScoped(ctx.Request.Context(), sessionScopeFromCtx(ctx))

	response.Success(ctx, gin.H{
		"list":      sessions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// @Summary 关闭会话
// @Tags      customer-session
// @Router    /api/customer-sessions/{id}/close [post]
// CloseSession 关闭会话
func (c *CustomerSessionController) CloseSession(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的会话ID")
		return
	}

	// 数据隔离：关闭前先过可见性检查
	if _, err := c.sessionService.GetSessionByIDScoped(ctx.Request.Context(), uint(id), sessionScopeFromCtx(ctx)); err != nil {
		writeSessionScopedError(ctx, err, "会话不存在")
		return
	}

	if err := c.sessionService.UpdateSessionStatus(ctx.Request.Context(), uint(id), model.SessionStatusClosed); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "关闭成功")
}
