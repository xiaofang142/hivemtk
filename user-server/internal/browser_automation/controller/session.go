package controller

import (
	"net/http"
	"strconv"

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
