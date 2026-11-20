package controller

import (
	"context"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// AgentInferenceController 只处理 HTTP 绑定和响应；运行时由组合根注入。
type AgentInferenceController struct {
	getInference  func() *agent_runtime.CoreDataFlowOrchestrator
	getPermission func() *tooluse.WhitelistPermissionChecker
}

func NewAgentInferenceController(inference func() *agent_runtime.CoreDataFlowOrchestrator, permission func() *tooluse.WhitelistPermissionChecker) *AgentInferenceController {
	return &AgentInferenceController{getInference: inference, getPermission: permission}
}

func (h *AgentInferenceController) Run(c *gin.Context) {
	orch := h.getInference()
	if orch == nil {
		response.Error(c, 503, "inference orchestrator not initialized")
		return
	}

	var req dto.AgentInferenceRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}

	payload := agent_runtime.CustomerMessagePayload{
		MessageID:   req.MessageID,
		AccountID:   req.AccountID,
		ChannelType: req.ChannelType,
		CustomerID:  req.CustomerID,
		SessionID:   req.SessionID,
		Content:     req.Content,
		MessageType: req.MessageType,
		Timestamp:   time.Now(),
		TraceID:     req.TraceID,
	}
	if payload.MessageType == "" {
		payload.MessageType = "text"
	}
	if payload.TraceID == "" {
		payload.TraceID = "inference-" + payload.SessionID + "-" + payload.CustomerID
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := orch.ProcessWithMode(ctx, payload, nil, req.Mode)
	if err != nil {
		logger.Errorf("[inference_api] run failed: %v", err)
		if result != nil && result.ErrorCode == "execution_not_configured" {
			response.Error(c, 503, response.ErrInferenceExecutionUnavailable, inferenceResultData(result))
			return
		}
		response.Error(c, 500, response.ErrInferenceFailed, inferenceResultData(result))
		return
	}
	if result.Outcome == agent_runtime.InferenceRejected {
		response.Error(c, 409, response.ErrInferenceRejected, inferenceResultData(result))
		return
	}
	if result.Outcome == agent_runtime.InferencePendingApproval {
		response.Accepted(c, inferenceResultData(result), response.MsgInferenceApprovalPending)
		return
	}
	if result.Outcome == agent_runtime.InferenceDeliveryPending {
		response.Accepted(c, inferenceResultData(result), response.MsgInferenceDeliveryPending)
		return
	}
	response.Success(c, inferenceResultData(result), "ok")
}

func inferenceResultData(result *agent_runtime.OrchestratorResult) gin.H {
	if result == nil {
		return gin.H{"outcome": agent_runtime.InferenceFailed, "publishable": false}
	}
	return gin.H{
		"delivery":                result.Delivery,
		"mode":                    result.Mode,
		"execution_status":        result.ExecutionStatus,
		"executed":                result.Executed,
		"plan":                    result.Plan,
		"planned_tool_call_count": result.PlannedToolCallCount,
		"tool_results":            result.ToolResults,
		"session_id":              result.SessionID,
		"final_reply":             result.FinalReply,
		"handoff_to_human":        result.HandoffToHuman,
		"handoff_reason":          result.HandoffReason,
		"tool_call_count":         result.ToolCallCount,
		"crisis_level":            result.CrisisLevel,
		"total_duration":          result.TotalDuration.String(),
		"outcome":                 result.Outcome,
		"publishable":             result.Publishable,
		"failed_stage":            result.FailedStage,
		"error_code":              result.ErrorCode,
		"stages":                  result.Stages,
		"degradations":            result.Degradations,
	}
}

func (h *AgentInferenceController) Stats(c *gin.Context) {
	orch := h.getInference()
	if orch == nil {
		response.Error(c, 503, "inference orchestrator not initialized")
		return
	}
	response.Success(c, gin.H{
		"stats": orch.GetStats(),
	}, "ok")
}

func (h *AgentInferenceController) GetPermissionDefault(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	response.Success(c, gin.H{
		"default_allow": pc.GetDefaultAllow(),
	}, "ok")
}

func (h *AgentInferenceController) SetPermissionDefault(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	var req dto.AgentPermissionDefaultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.SetDefaultAllow(req.DefaultAllow)
	logger.Infof("[permission] default_allow set to %v", req.DefaultAllow)
	response.Success(c, gin.H{"default_allow": req.DefaultAllow}, "ok")
}

func (h *AgentInferenceController) GetGlobalWhitelist(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	tools := pc.ListGlobalWhitelist()
	response.Success(c, gin.H{
		"tools": tools,
		"count": len(tools),
	}, "ok")
}

func (h *AgentInferenceController) AddGlobalWhitelist(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	var req dto.AgentToolWhitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.AddGlobalWhitelist(req.Tools)
	logger.Infof("[permission] global whitelist added %d tools", len(req.Tools))
	response.Success(c, gin.H{
		"tools": pc.ListGlobalWhitelist(),
		"count": len(pc.ListGlobalWhitelist()),
	}, "ok")
}

func (h *AgentInferenceController) ListConfiguredAgents(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agents := pc.ListConfiguredAgents()
	response.Success(c, gin.H{
		"agents": agents,
		"count":  len(agents),
	}, "ok")
}

func (h *AgentInferenceController) GetAgentWhitelist(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	tools := pc.ListAgentWhitelist(agentID)
	response.Success(c, gin.H{
		"agent_id": agentID,
		"tools":    tools,
		"count":    len(tools),
	}, "ok")
}

func (h *AgentInferenceController) SetAgentWhitelist(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	var req dto.AgentToolWhitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "invalid request: "+err.Error())
		return
	}
	pc.SetAgentWhitelist(agentID, req.Tools)
	logger.Infof("[permission] agent=%s whitelist set (%d tools)", agentID, len(req.Tools))
	response.Success(c, gin.H{
		"agent_id": agentID,
		"tools":    pc.ListAgentWhitelist(agentID),
		"count":    len(pc.ListAgentWhitelist(agentID)),
	}, "ok")
}

func (h *AgentInferenceController) RemoveAgentWhitelist(c *gin.Context) {
	pc := h.getPermission()
	if pc == nil {
		response.Error(c, 503, "permission checker not initialized")
		return
	}
	agentID := c.Param("agent_id")
	if agentID == "" {
		response.Error(c, 400, "agent_id required")
		return
	}
	pc.RemoveAgentWhitelist(agentID)
	logger.Infof("[permission] agent=%s whitelist removed (fallback to default policy)", agentID)
	response.Success(c, gin.H{
		"agent_id": agentID,
	}, "whitelist removed, fallback to default policy")
}
