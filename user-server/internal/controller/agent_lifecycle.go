package controller

import (
	"context"
	"net/http"

	"hivemtk-user/internal/aiagent/agent/lifecycle"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// AgentLifecycleController 接收已装配的运行函数，不参与运行时构造或全局查找。
type AgentLifecycleController struct {
	run func(context.Context, uint, *lifecycle.LifecycleRequest) (*lifecycle.LifecycleResult, error)
}

func NewAgentLifecycleController(run func(context.Context, uint, *lifecycle.LifecycleRequest) (*lifecycle.LifecycleResult, error)) *AgentLifecycleController {
	return &AgentLifecycleController{run: run}
}

func (h *AgentLifecycleController) Run(c *gin.Context) {
	if h.run == nil {
		response.Error(c, http.StatusServiceUnavailable, "agent lifecycle runtime not assembled（双模式运行时未装配）")
		return
	}
	var body dto.AgentLifecycleRunRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	res, err := h.run(c.Request.Context(), body.AgentID, &lifecycle.LifecycleRequest{
		Channel: body.Channel, CustomerID: body.CustomerID, Content: body.Content,
		SessionID: body.SessionID, OneID: body.OneID, TraceID: body.TraceID,
	})
	if err != nil {
		logger.Errorf("[agent-lifecycle] run failed agent=%d customer=%s: %v", body.AgentID, body.CustomerID, err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"mode": res.Mode, "execution_id": res.ExecutionID, "one_id": res.OneID,
		"reply": res.ReplyContent, "handoff": res.Handoff,
		"stop_reason": res.StopReason, "tools_called": res.ToolsCalled,
	}, "ok")
}
