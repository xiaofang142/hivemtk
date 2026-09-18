package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AgentStatusController 客服状态控制器
type AgentStatusController struct {
	agentService    *service.AgentStatusService
	customerService *service.CustomerServiceAgentService
}

// NewAgentStatusController 创建客服状态控制器实例
//
// 由 router 层注入 *service.AIAgentService，控制器仅持有 service 依赖，零 db 引用。
func NewAgentStatusController(aiAgentSvc *service.AIAgentService) *AgentStatusController {
	return &AgentStatusController{
		agentService:    service.NewAgentStatusService(),
		customerService: service.NewCustomerServiceAgentServiceViaPort(aiAgentSvc),
	}
}

// CreateAgent 创建客服
func (c *AgentStatusController) CreateAgent(ctx *gin.Context) {
	var req service.CreateAgentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	agent, err := c.agentService.CreateAgent(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, agent, "创建成功")
}

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
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, agent, "获取成功")
}

// GetOnlineAgents 获取在线客服列表
func (c *AgentStatusController) GetOnlineAgents(ctx *gin.Context) {
	agents, err := c.agentService.GetOnlineAgents(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, agents, "获取成功")
}

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

// ListAllAgents 列出全部客服（监管控制台）
func (c *AgentStatusController) ListAllAgents(ctx *gin.Context) {
	agents, err := c.agentService.ListAllAgents(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, agents, "获取成功")
}

// UpdateAgentStatus 更新客服状态
func (c *AgentStatusController) UpdateAgentStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	if err := c.agentService.UpdateAgentStatus(ctx.Request.Context(), uint(id), req.Status); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "更新成功")
}

// GoOnline 客服上线
func (c *AgentStatusController) GoOnline(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	if err := c.agentService.GoOnline(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "上线成功")
}

// GoOffline 客服下线
func (c *AgentStatusController) GoOffline(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	if err := c.agentService.GoOffline(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "下线成功")
}

// GetAgentSessions 获取客服的会话
func (c *AgentStatusController) GetAgentSessions(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的客服ID")
		return
	}

	sessions, err := c.agentService.GetAgentSessions(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取客服会话") {
		return
	}

	_ = c.agentService.TouchHeartbeat(ctx.Request.Context(), uint(id))

	response.Success(ctx, sessions, "获取成功")
}
