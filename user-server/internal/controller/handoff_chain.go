package controller

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// HandoffChainController 管理端会话链路 + SLA + 自动化规则控制器
type HandoffChainController struct {
	chainSvc *service.SessionChainService
	ruleSvc  *service.RuleEngineService
}

// NewHandoffChainController 构造
func NewHandoffChainController() *HandoffChainController {
	return &HandoffChainController{
		chainSvc: service.NewSessionChainServiceFromGlobal(),
		ruleSvc:  service.NewRuleEngineServiceFromGlobal(),
	}
}

// GetAutoResolveConfig GET /api/manage/session-chain/sla-config
func (c *HandoffChainController) GetAutoResolveConfig(ctx *gin.Context) {
	cfg := c.chainSvc.GetAutoResolveConfig(ctx.Request.Context())
	response.Success(ctx, cfg, "ok")
}

// SaveAutoResolveConfig PUT /api/manage/session-chain/sla-config
func (c *HandoffChainController) SaveAutoResolveConfig(ctx *gin.Context) {
	var cfg service.AutoResolveConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, 400, "请求参数错误："+err.Error())
		return
	}
	if err := c.chainSvc.SaveAutoResolveConfig(ctx.Request.Context(), &cfg); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, cfg, "SLA 配置已保存")
}

// ReopenOnInboundMessage POST /api/manage/session-chain/reopen
// body: {"session_id": "..."}
func (c *HandoffChainController) ReopenOnInboundMessage(ctx *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "session_id 必填")
		return
	}
	reopened, err := c.chainSvc.ReopenOnInboundMessage(ctx.Request.Context(), req.SessionID)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"session_id": req.SessionID, "reopened": reopened}, "ok")
}

// CreateRule POST /api/manage/rules
// body: {"event": "...", "conditions": "{\"field\":\"...\"}", "actions": "[...]", "name": "...", "enabled": true, "priority": 0, "delay_minutes": 0}
func (c *HandoffChainController) CreateRule(ctx *gin.Context) {
	var req struct {
		Event        string `json:"event" binding:"required"`
		Conditions   string `json:"conditions"`
		Actions      string `json:"actions" binding:"required"`
		Name         string `json:"name" binding:"required"`
		Enabled      bool   `json:"enabled"`
		Priority     int    `json:"priority"`
		DelayMinutes int    `json:"delay_minutes"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误："+err.Error())
		return
	}
	rule := &model.AutomationRule{
		Event:        req.Event,
		Conditions:   req.Conditions,
		Actions:      req.Actions,
		Name:         req.Name,
		Enabled:      req.Enabled,
		Priority:     req.Priority,
		DelayMinutes: req.DelayMinutes,
	}
	r, err := c.ruleSvc.Create(ctx.Request.Context(), rule)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, r, "规则已创建")
}

// ListRules GET /api/manage/rules?event=
func (c *HandoffChainController) ListRules(ctx *gin.Context) {
	list, err := c.ruleSvc.List(ctx.Request.Context(), ctx.Query("event"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// DeleteRule DELETE /api/manage/rules/:id
func (c *HandoffChainController) DeleteRule(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.ruleSvc.Delete(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "已删除")
}

// ToggleRule PUT /api/manage/rules/:id/toggle
// body: {"enabled": true}
func (c *HandoffChainController) ToggleRule(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.Error(ctx, 400, "enabled 必填")
		return
	}
	if err := c.ruleSvc.Toggle(ctx.Request.Context(), id, *req.Enabled); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"enabled": *req.Enabled}, "已更新")
}
