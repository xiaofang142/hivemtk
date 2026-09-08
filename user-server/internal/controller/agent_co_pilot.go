// Package controller - Agent Co-Pilot 自动执行（G1）
package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// AgentCoPilotController 自动执行配置 + 决策 API
type AgentCoPilotController struct {
	svc *service.AgentCoPilotService
}

// NewAgentCoPilotController 创建实例
func NewAgentCoPilotController() *AgentCoPilotController {
	return &AgentCoPilotController{
		svc: service.NewAgentCoPilotService(),
	}
}

// GetConfig GET /api/agent/co-pilot/config
func (c *AgentCoPilotController) GetConfig(ctx *gin.Context) {
	cfg, err := c.svc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, cfg, "获取成功")
}

// SaveConfig PUT /api/agent/co-pilot/config
func (c *AgentCoPilotController) SaveConfig(ctx *gin.Context) {
	var req service.CoPilotAutoExecuteConfig
	if !response.BindJSON(ctx, &req) {
		return
	}
	if err := c.svc.SaveConfig(ctx.Request.Context(), &req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, req, "保存成功")
}

// Evaluate POST /api/agent/co-pilot/evaluate
// 请求体: {"confidence": 0.92, "estimated_cost": 0.05}
// 返回: auto_approved + reason
func (c *AgentCoPilotController) Evaluate(ctx *gin.Context) {
	var req struct {
		Confidence    float64 `json:"confidence"`
		EstimatedCost float64 `json:"estimated_cost"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	decision, err := c.svc.Evaluate(ctx.Request.Context(), req.Confidence, req.EstimatedCost)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, decision, "评估完成")
}

// ManageCoPilotController 管理端 Co-Pilot 自动执行控制器
type ManageCoPilotController struct {
	svc *service.AgentCoPilotService
}

// NewManageCoPilotController 构造
func NewManageCoPilotController() *ManageCoPilotController {
	return &ManageCoPilotController{svc: service.NewAgentCoPilotService()}
}

// Evaluate POST /api/manage/co-pilot/evaluate
// body: {"session_id": "...", "intent_type": "...", "confidence": 0.92, "cost": 0.05}
func (c *ManageCoPilotController) Evaluate(ctx *gin.Context) {
	var req struct {
		SessionID  string  `json:"session_id"`
		IntentType string  `json:"intent_type"`
		Confidence float64 `json:"confidence"`
		Cost       float64 `json:"cost"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误："+err.Error())
		return
	}
	decision, err := c.svc.Evaluate(ctx.Request.Context(), req.Confidence, req.Cost)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, decision, "评估完成")
}

// GetConfig GET /api/manage/co-pilot/config
func (c *ManageCoPilotController) GetConfig(ctx *gin.Context) {
	cfg, err := c.svc.GetConfig(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, cfg, "ok")
}

// SetConfig PUT /api/manage/co-pilot/config
// body: {"enabled": true, "confidence_threshold": 0.85, "cost_limit": 0.5}
func (c *ManageCoPilotController) SetConfig(ctx *gin.Context) {
	var cfg service.CoPilotAutoExecuteConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, "请求参数错误："+err.Error())
		return
	}
	if err := c.svc.SaveConfig(ctx.Request.Context(), &cfg); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, cfg, "配置已保存")
}
