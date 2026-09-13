package controller

import (
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SessionController 会话控制器（查询 + 手动中断）
type SessionController struct {
	svc *service.SessionService
}

func NewSessionController(svc *service.SessionService) *SessionController {
	return &SessionController{svc: svc}
}

// List GET /browser-automation/sessions?status=&page=&limit=
func (c *SessionController) List(ctx *gin.Context) {
	var req dto.ListSessionReq
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	list, total, err := c.svc.ListByUser(ctx.Request.Context(), taskUserID(ctx), req.Status, req.Page, req.Limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询会话失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Get GET /browser-automation/sessions/:id
func (c *SessionController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	sess, err := c.svc.Get(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "会话不存在")
		return
	}
	response.Success(ctx, sess, "ok")
}

// ListSteps GET /browser-automation/sessions/:id/steps
func (c *SessionController) ListSteps(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	steps, err := c.svc.ListSteps(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "会话不存在")
		return
	}
	response.SuccessWithList(ctx, steps, int64(len(steps)))
}

// ListLogs GET /browser-automation/sessions/:id/logs?direction=command|event|judge
// D1（G1 补口）：append-only 命令流审计查询（归属校验与 direction 过滤在 service）。
func (c *SessionController) ListLogs(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	direction := ctx.DefaultQuery("direction", "")
	if direction != "" && direction != "command" && direction != "event" && direction != "judge" {
		response.Error(ctx, http.StatusBadRequest, "direction 仅支持 command/event/judge")
		return
	}
	logs, err := c.svc.ListCommandLogs(ctx.Request.Context(), uint(id), taskUserID(ctx), direction)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "会话不存在")
		return
	}
	response.SuccessWithList(ctx, logs, int64(len(logs)))
}

// ListByTask GET /browser-automation/tasks/:id/sessions
func (c *SessionController) ListByTask(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	list, total, err := c.svc.ListByTask(ctx.Request.Context(), id, taskUserID(ctx), page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询会话失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Export GET /browser-automation/sessions/:id/export
// I5：session 全量审计包（会话+步流水+命令流+LLM 成本账）单请求归并，
// 前端「导出审计包」按钮直接落盘 JSON——铁律 4 审计链的离线归档面。
func (c *SessionController) Export(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	sess, steps, logs, plans, err := c.svc.SessionExport(ctx.Request.Context(), uint(id), taskUserID(ctx))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "会话不存在")
		return
	}
	ctx.Header("Content-Disposition", "attachment; filename=browser_session_"+ctx.Param("id")+"_audit.json")
	response.Success(ctx, gin.H{
		"session": sess, "steps": steps, "command_log": logs, "llm_plans": plans,
		"exported_at": time.Now().Format(time.RFC3339),
	}, "ok")
}

// Stop POST /browser-automation/sessions/:id/stop
func (c *SessionController) Stop(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	var req dto.StopSessionReq
	_ = ctx.ShouldBindJSON(&req) // reason 可选
	ok, err := c.svc.Stop(ctx.Request.Context(), uint(id), taskUserID(ctx), req.Reason)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "会话不存在")
		return
	}
	if !ok {
		response.Success(ctx, gin.H{"stopped": false}, "会话不在运行中")
		return
	}
	response.Success(ctx, gin.H{"stopped": true}, "中断信号已发送")
}
