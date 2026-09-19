package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/browser_automation/dto"
	bamodel "hivemtk-user/internal/browser_automation/model"
	baplat "hivemtk-user/internal/browser_automation/platform"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"

	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// hostProber 交互式执行前的 Host 在线探针，生产实现是 *service.HostRegistry。
// 只挂在这一条 HTTP 路径上：cron 与自动重试要容忍「Host 暂时不在，会话排队等它」，
// 而在列表页点「执行」的人必须当场拿到「没 Host」的结论。
// 批10 的 C 腿（真机 kill nm-host 后点执行）实测：改造前 /run 回的是 200 + session_id，
// RunTask 全异步，ErrHostOffline 只在执行期产生 → 8001 在请求面永远不出现，
// 前端「离线 → 引导弹窗」形同虚设，用户看到的是「已开始执行」再等一条红色会话失败。
type hostProber interface{ EnsureOnline(userID uint) error }

// TaskController 任务控制器
type TaskController struct {
	svc  *basvc.TaskService
	host hostProber
}

func NewTaskController(svc *basvc.TaskService, host hostProber) *TaskController {
	return &TaskController{svc: svc, host: host}
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
		// 批10：离线与忙都归 409，但前端必须是两条路——离线要去装扩展/Host，忙只要等。
		// 只给 HTTP 码时 response.Error 会把所有 409 折成 DUPLICATE_ENTRY_3003，
		// 于是「已有任务执行中」也弹安装引导（真机走 UI 实测踩过）。码域见 utils/error_code.go。
		response.Error(ctx, utils.ErrorCodeBrowserHostOffline, err.Error())
	case errors.Is(err, basvc.ErrUserBusy):
		response.Error(ctx, utils.ErrorCodeBrowserTaskBusy, err.Error())
	case errors.Is(err, basvc.ErrTaskRunning):
		response.Error(ctx, http.StatusConflict, err.Error())
	case errors.Is(err, basvc.ErrDependencyNotMet):
		response.Error(ctx, http.StatusConflict, err.Error())
	case errors.Is(err, basvc.ErrInvalidURL):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Error(ctx, http.StatusNotFound, "任务不存在")
	default:
		// 类型分流放在 default 里：这两类是「带动态文案的前置条件」，没有可比对的哨兵值，
		// 只能按类型判。状态不满足=409（和忙/离线同一族：换个时机再来），入参不合法=400
		// （改请求再来）；两者都把真实文案原样带给前端，不再冒充 500。
		var sc *basvc.StateConflictError
		var ii *basvc.InvalidInputError
		switch {
		case errors.As(err, &sc):
			response.Error(ctx, utils.ErrorCodeBrowserStateConflict, sc.Error())
		case errors.As(err, &ii):
			response.Error(ctx, http.StatusBadRequest, ii.Error())
		default:
			response.Error(ctx, http.StatusInternalServerError, err.Error())
		}
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
	t.RequireConfirm = req.RequireConfirm
	t.ConfirmWaitSec = req.ConfirmWaitSec // 批8：0=沿用默认 600s（列 default 与 service 兜底同口径）
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
		if req.RequireConfirm != nil { // D7
			t.RequireConfirm = *req.RequireConfirm
		}
		if req.ConfirmWaitSec != nil { // 批8：指针语义，nil=不改确认等待预算
			t.ConfirmWaitSec = *req.ConfirmWaitSec
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
	// 交互路径的先验门：Host 不在就当场 409 BROWSER_HOST_OFFLINE_8001，不建会话。
	if err := c.host.EnsureOnline(taskUserID(ctx)); err != nil {
		taskErrToResponse(ctx, err)
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
