package controller

import (
	"errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// AgentStatusController 客服状态控制器
type AgentStatusController struct {
	agentService    *service.AgentStatusService
	customerService *service.CustomerServiceAgentService
}

// NewAgentStatusController 创建客服状态控制器实例
//
// 由 router 层注入两个 service，控制器仅持有 service 依赖，零 db 引用。
// 挂载服务必须在装配层用显式 DB 建好再传进来（不在控制器里自取全局库）。
func NewAgentStatusController(
	agentStatusSvc *service.AgentStatusService,
	csAgentSvc *service.CustomerServiceAgentService,
) *AgentStatusController {
	return &AgentStatusController{
		agentService:    agentStatusSvc,
		customerService: csAgentSvc,
	}
}

// agentPublicMessage 去掉哨兵前缀，避免前端看到「坐席已存在: 坐席 62 已存在」这类重复。
func agentPublicMessage(err error, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if strings.HasPrefix(msg, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(msg, prefix))
	}
	return msg
}

// replyAgentError 坐席域统一错误出口：哨兵 → 400/404/409，其余走通用 DB 映射。
//
// 不直接把 err.Error() 塞进 404/500：仓储层错误可能带 SQLSTATE 与表名。
func replyAgentError(ctx *gin.Context, err error, op string) {
	switch {
	case errors.Is(err, service.ErrAgentNotFound):
		response.NotFound(ctx, agentPublicMessage(err, service.ErrAgentNotFound))
	case errors.Is(err, service.ErrAgentDuplicated):
		response.Error(ctx, http.StatusConflict, agentPublicMessage(err, service.ErrAgentDuplicated))
	case errors.Is(err, service.ErrAgentInvalidInput):
		response.Error(ctx, http.StatusBadRequest, agentPublicMessage(err, service.ErrAgentInvalidInput))
	default:
		response.ErrorFromDB(ctx, err, op)
	}
}

// @Summary      创建客服
// @Tags      agent-status
// @Router    /api/agents [post]
// CreateAgent 创建客服
func (c *AgentStatusController) CreateAgent(ctx *gin.Context) {
	var req service.CreateAgentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：agent_id（坐席号，正整数）与 agent_name（坐席姓名）均为必填，max_sessions 可选（1~50）")
		return
	}

	agent, err := c.agentService.CreateAgent(ctx.Request.Context(), &req)
	if err != nil {
		replyAgentError(ctx, err, "创建坐席失败")
		return
	}

	response.Success(ctx, agent, "创建成功")
}

// @Summary      获取客服状态
// @Tags      agent-status
// @Router    /api/agent-statuses/{id} [get]
// GetAgentStatus 获取客服状态
func (c *AgentStatusController) GetAgentStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	agent, err := c.agentService.GetAgentStatus(ctx.Request.Context(), uint(id))
	if err != nil {
		replyAgentError(ctx, err, "获取坐席失败")
		return
	}

	response.Success(ctx, agent, "获取成功")
}

// @Summary      获取在线客服列表
// @Tags      agent-status
// @Router    /api/agent-statuses [get]
// GetOnlineAgents 获取在线客服列表
func (c *AgentStatusController) GetOnlineAgents(ctx *gin.Context) {
	agents, err := c.agentService.GetOnlineAgents(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取在线坐席失败")
		return
	}

	response.Success(ctx, agents, "获取成功")
}

// @Summary      获取当前登录用户的坐席身份
// @Tags      agent-status
// @Router    /api/agents/me [get]
// GetMyAgent 获取当前登录用户对应的坐席身份（接入登录态，杜绝"列表首位猜测"）
// GET /api/agents/me
func (c *AgentStatusController) GetMyAgent(ctx *gin.Context) {
	uidVal, ok := ctx.Get("user_id")
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "未登录", "缺少 user_id")
		return
	}
	var userID uint
	switch v := uidVal.(type) {
	case uint:
		userID = v
	case float64:
		userID = uint(v)
	case int:
		userID = uint(v)
	default:
		response.Error(ctx, http.StatusUnauthorized, "无效的用户标识", "")
		return
	}
	nameVal, _ := ctx.Get("username")
	name, _ := nameVal.(string)
	if name == "" {
		name = "user_" + strconv.FormatUint(uint64(userID), 10)
	}
	st, err := c.customerService.GetOrCreateAgentStatusByUserID(ctx.Request.Context(), userID, name)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取坐席身份失败", err.Error())
		return
	}
	response.Success(ctx, st, "获取成功")
}

// @Summary      列出全部客服（监管控制台）
// @Tags      agent-status
// @Router    /api/agent-statuses/list [get]
// ListAllAgents 列出全部客服（监管控制台）
//
// 数据隔离：admin 看全部；其余角色只回自己那条坐席记录（AgentID=系统用户ID），
// 避免任意登录用户拉走全量坐席名册与状态。
func (c *AgentStatusController) ListAllAgents(ctx *gin.Context) {
	agents, err := c.agentService.ListAllAgents(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取坐席列表失败")
		return
	}

	if ctx.GetString("role") != "admin" {
		uid := getUserIDFromContext(ctx)
		mine := make([]*model.AgentStatus, 0, 1)
		for _, a := range agents {
			if a != nil && a.AgentID == uid {
				mine = append(mine, a)
			}
		}
		agents = mine
	}

	response.Success(ctx, agents, "获取成功")
}

// @Summary      更新客服状态
// @Tags      agent-status
// @Router    /api/agent-statuses/{id}/status [put]
// UpdateAgentStatus 更新客服状态
func (c *AgentStatusController) UpdateAgentStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	// 前端监管台的"编辑坐席"弹窗把状态与接待上限放在同一个表单里提交，
	// 原先只解 status、max_sessions 被静默丢弃（保存成功但上限没变）。
	var req struct {
		Status      string `json:"status" binding:"required"`
		MaxSessions *int   `json:"max_sessions"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：status 必填（online / busy / offline），max_sessions 可选（1~50）")
		return
	}

	if err := c.agentService.UpdateAgentStatus(ctx.Request.Context(), uint(id), req.Status, req.MaxSessions); err != nil {
		replyAgentError(ctx, err, "更新坐席状态失败")
		return
	}

	response.Success(ctx, nil, "更新成功")
}

// @Summary 客服上线
// @Tags      agent-status
// @Router    /api/agent-statuses/{id}/online [post]
// GoOnline 客服上线
func (c *AgentStatusController) GoOnline(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	if err := c.agentService.GoOnline(ctx.Request.Context(), uint(id)); err != nil {
		replyAgentError(ctx, err, "坐席上线失败")
		return
	}

	response.Success(ctx, nil, "上线成功")
}

// @Summary 客服下线
// @Tags      agent-status
// @Router    /api/agent-statuses/{id}/offline [post]
// GoOffline 客服下线
func (c *AgentStatusController) GoOffline(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	if err := c.agentService.GoOffline(ctx.Request.Context(), uint(id)); err != nil {
		replyAgentError(ctx, err, "坐席下线失败")
		return
	}

	response.Success(ctx, nil, "下线成功")
}

// @Summary      获取客服的会话
// @Tags      agent-status
// @Router    /api/agent-statuses/{id}/sessions [get]
// GetAgentSessions 获取客服的会话
//
// 数据隔离：admin 可看任意坐席；其余角色只允许查自己的会话（:id 即 AgentID=用户ID）。
func (c *AgentStatusController) GetAgentSessions(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	if ctx.GetString("role") != "admin" && uint(id) != getUserIDFromContext(ctx) {
		response.Error(ctx, http.StatusForbidden, "无权访问该坐席的会话")
		return
	}

	sessions, err := c.agentService.GetAgentSessions(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取客服会话") {
		return
	}

	_ = c.agentService.TouchHeartbeat(ctx.Request.Context(), uint(id))

	response.Success(ctx, sessions, "获取成功")
}
