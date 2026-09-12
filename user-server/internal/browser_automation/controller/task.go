package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/browser_automation/dto"
	bamodel "hivemtk-user/internal/browser_automation/model"
	baplat "hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/browser_automation/service"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils/response"

	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TaskController 任务控制器
type TaskController struct {
	svc *service.TaskService
}

func NewTaskController(svc *service.TaskService) *TaskController {
	return &TaskController{svc: svc}
}

func taskUserID(ctx *gin.Context) uint { return ctx.GetUint("user_id") }

func parseID(ctx *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return uint(id), true
}

// taskErrToResponse 统一域内错误映射
func taskErrToResponse(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, basvc.ErrHostOffline):
		response.Error(ctx, http.StatusConflict, err.Error())
	case errors.Is(err, basvc.ErrTaskRunning):
		response.Error(ctx, http.StatusConflict, err.Error())
	case errors.Is(err, basvc.ErrDependencyNotMet):
		response.Error(ctx, http.StatusConflict, err.Error())
	case errors.Is(err, basvc.ErrInvalidURL):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Error(ctx, http.StatusNotFound, "任务不存在")
	default:
		response.Error(ctx, http.StatusInternalServerError, err.Error())
	}
}

// Create POST /browser-automation/tasks
func (c *TaskController) Create(ctx *gin.Context) {
	var req dto.CreateBrowserTaskReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	t := &bamodel.BrowserTask{}
	t.Name = req.Name
	t.Description = req.Description
	t.TaskType = req.TaskType
	t.Url = req.Url
	t.Platform = strings.TrimSpace(req.Platform) // 空值由 Service.Create 缺省 xiaohongshu
	t.BrainMode = req.BrainMode
	t.BrainGoal = req.BrainGoal
	t.LoopCount = req.LoopCount
	t.DelayMs = req.DelayMs
	t.TimeoutSec = req.TimeoutSec
	t.DependsOnTaskID = req.DependsOnTaskID
	t.DependsOnMode = req.DependsOnMode
	t.RetryOnFail = req.RetryOnFail
	t.RetryDelaySec = req.RetryDelaySec
	t.MaxRetryTimes = req.MaxRetryTimes
	if req.Steps != nil {
		raw, err := json.Marshal(req.Steps)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "步骤编排序列化失败")
			return
		}
		t.Steps = raw
	}
	if err := c.svc.Create(ctx.Request.Context(), taskUserID(ctx), t); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, t, "ok")
}

// List GET /browser-automation/tasks
func (c *TaskController) List(ctx *gin.Context) {
	var req dto.ListTaskReq
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	list, total, err := c.svc.List(ctx.Request.Context(), taskUserID(ctx), req.Status, req.TaskType, req.Page, req.Limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询任务失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

// Get GET /browser-automation/tasks/:id
func (c *TaskController) Get(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	t, err := c.svc.Get(ctx.Request.Context(), id, taskUserID(ctx))
	if err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, t, "ok")
}

// Update PUT /browser-automation/tasks/:id
func (c *TaskController) Update(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req dto.UpdateBrowserTaskReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	t, err := c.svc.Update(ctx.Request.Context(), id, taskUserID(ctx), func(t *bamodel.BrowserTask) error {
		if req.Name != "" {
			t.Name = req.Name
		}
		if req.Description != nil {
			t.Description = *req.Description
		}
		if req.TaskType != "" {
			t.TaskType = req.TaskType
		}
		if req.Url != "" {
			t.Url = req.Url
		}
		if req.Platform != nil && strings.TrimSpace(*req.Platform) != "" {
			if _, err := baplat.Get(strings.TrimSpace(*req.Platform)); err != nil {
				return err
			}
			t.Platform = strings.TrimSpace(*req.Platform)
		}
		if req.BrainMode != nil {
			t.BrainMode = *req.BrainMode
		}
		if req.BrainGoal != nil {
			t.BrainGoal = *req.BrainGoal
		}
		if req.Steps != nil {
			raw, err := json.Marshal(req.Steps)
			if err != nil {
				return errors.New("步骤编排序列化失败")
			}
			t.Steps = raw
		}
		if req.LoopCount != nil {
			t.LoopCount = *req.LoopCount
		}
		if req.DelayMs != nil {
			t.DelayMs = *req.DelayMs
		}
		if req.TimeoutSec != nil {
			t.TimeoutSec = *req.TimeoutSec
		}
		return nil
	})
	if err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, t, "ok")
}

// Delete DELETE /browser-automation/tasks/:id
func (c *TaskController) Delete(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), id, taskUserID(ctx)); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "已删除")
}

// Publish POST /browser-automation/tasks/:id/publish
func (c *TaskController) Publish(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.svc.Publish(ctx.Request.Context(), id, taskUserID(ctx)); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "已发布")
}

// Run POST /browser-automation/tasks/:id/run（异步：立即返回 session_id）
func (c *TaskController) Run(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	session, err := c.svc.RunTask(ctx.Request.Context(), id, taskUserID(ctx), 0)
	if err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"session_id": session.ID, "status": session.Status}, "已开始执行")
}

// Pause POST /browser-automation/tasks/:id/pause
func (c *TaskController) Pause(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.svc.Pause(ctx.Request.Context(), id, taskUserID(ctx)); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "已暂停")
}

// Resume POST /browser-automation/tasks/:id/resume
func (c *TaskController) Resume(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	session, err := c.svc.Resume(ctx.Request.Context(), id, taskUserID(ctx))
	if err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"session_id": session.ID, "status": session.Status}, "已恢复执行")
}

// Archive POST /browser-automation/tasks/:id/archive
func (c *TaskController) Archive(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.svc.Archive(ctx.Request.Context(), id, taskUserID(ctx)); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "已归档")
}

// SetDependency PUT /browser-automation/tasks/:id/dependency（workflow 依赖，DFS 检环）
func (c *TaskController) SetDependency(ctx *gin.Context) {
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req struct {
		DependsOnTaskID *uint  `json:"depends_on_task_id"`
		DependsOnMode   string `json:"depends_on_mode" binding:"omitempty,oneof=all_done any_success"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := c.svc.SetDependency(ctx.Request.Context(), id, taskUserID(ctx), req.DependsOnTaskID, req.DependsOnMode); err != nil {
		taskErrToResponse(ctx, err)
		return
	}
	response.Success(ctx, nil, "依赖已更新")
}
