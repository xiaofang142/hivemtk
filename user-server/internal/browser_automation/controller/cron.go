package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// CronController 定时触发器控制器
type CronController struct {
	svc *service.CronService
}

func NewCronController(svc *service.CronService) *CronController {
	return &CronController{svc: svc}
}

// cronErrToResponse 触发器域出口：与任务域共用同一份分类（批19c），404 的主语是触发器。
func cronErrToResponse(ctx *gin.Context, err error) {
	baErrToResponse(ctx, err, "触发器不存在")
}

// List GET /browser-automation/cron
func (c *CronController) List(ctx *gin.Context) {
	list, err := c.svc.List(ctx.Request.Context(), taskUserID(ctx))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询触发器失败")
		return
	}
	response.SuccessWithList(ctx, list, int64(len(list)))
}

// Create POST /browser-automation/cron
func (c *CronController) Create(ctx *gin.Context) {
	var req dto.CreateCronReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tr, err := c.svc.Create(ctx.Request.Context(), taskUserID(ctx), req.TaskID, req.CronExpr, req.TimeZone, enabled)
	if err != nil {
		cronErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, tr, "ok")
}

// Update PUT /browser-automation/cron/:id
func (c *CronController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	var req dto.UpdateCronReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	tr, err := c.svc.Update(ctx.Request.Context(), uint(id), taskUserID(ctx), req.CronExpr, req.TimeZone)
	if err != nil {
		cronErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, tr, "ok")
}

// Delete DELETE /browser-automation/cron/:id
func (c *CronController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id), taskUserID(ctx)); err != nil {
		cronErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "已删除")
}

// Enable POST /browser-automation/cron/:id/enable
func (c *CronController) Enable(ctx *gin.Context) {
	c.setEnabled(ctx, true)
}

// Disable POST /browser-automation/cron/:id/disable
func (c *CronController) Disable(ctx *gin.Context) {
	c.setEnabled(ctx, false)
}

func (c *CronController) setEnabled(ctx *gin.Context, enabled bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.SetEnabled(ctx.Request.Context(), uint(id), taskUserID(ctx), enabled); err != nil {
		cronErrToResponse(ctx, err)
		return
	}
	if enabled {
		response.Success(ctx, nil, "已启用")
		return
	}
	response.Success(ctx, nil, "已停用")
}
