package controller

import (
	"encoding/json"
	"net/http"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// RuleEngineController 自动化规则控制器
type RuleEngineController struct {
	svc *service.RuleEngineService
}

// NewRuleEngineController 构造
func NewRuleEngineController() *RuleEngineController {
	return &RuleEngineController{svc: service.NewRuleEngineServiceFromGlobal()}
}

// List GET /api/automation-rules?event=
func (c *RuleEngineController) List(ctx *gin.Context) {
	list, err := c.svc.List(ctx.Request.Context(), ctx.Query("event"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// Create POST /api/automation-rules
func (c *RuleEngineController) Create(ctx *gin.Context) {
	var req struct {
		Name         string                  `json:"name" binding:"required"`
		Event        string                  `json:"event" binding:"required"`
		Conditions   []service.RuleCondition `json:"conditions"`
		Actions      []service.RuleAction    `json:"actions" binding:"required"`
		DelayMinutes int                     `json:"delay_minutes"`
		Priority     int                     `json:"priority"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	conds, _ := json.Marshal(req.Conditions)
	acts, _ := json.Marshal(req.Actions)
	rule := &model.AutomationRule{
		Name: req.Name, Event: req.Event,
		Conditions: string(conds), Actions: string(acts),
		DelayMinutes: req.DelayMinutes, Priority: req.Priority, Enabled: true,
	}
	r, err := c.svc.Create(ctx.Request.Context(), rule)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, r, "规则已创建")
}

// Delete DELETE /api/automation-rules/:id
func (c *RuleEngineController) Delete(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "已删除")
}

// Toggle POST /api/automation-rules/:id/toggle {enabled}
func (c *RuleEngineController) Toggle(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.Error(ctx, http.StatusBadRequest, "enabled 必填")
		return
	}
	if err := c.svc.Toggle(ctx.Request.Context(), id, *req.Enabled); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"enabled": *req.Enabled}, "已更新")
}

// Fire POST /api/automation-rules/fire {event, session_id}（手动触发/测试）
func (c *RuleEngineController) Fire(ctx *gin.Context) {
	var req struct {
		Event     string `json:"event" binding:"required"`
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "event/session_id 必填")
		return
	}
	full, err := service.NewSessionChainServiceFromGlobal().GetSession(ctx.Request.Context(), req.SessionID)
	if HandleServiceError(ctx, err) {
		return
	}
	c.svc.Dispatch(ctx.Request.Context(), req.Event, req.SessionID, full)
	response.Success(ctx, gin.H{"fired": true}, "规则事件已分发")
}
