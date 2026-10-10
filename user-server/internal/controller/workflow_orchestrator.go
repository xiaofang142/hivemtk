package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type WorkflowOrchestratorController struct {
	svc *service.WorkflowOrchestratorService
}

func NewWorkflowOrchestratorController(svc *service.WorkflowOrchestratorService) *WorkflowOrchestratorController {
	return &WorkflowOrchestratorController{svc: svc}
}

// CreateVersion godoc
// @Summary      创建工作流版本
// @Description  创建一个新的工作流版本
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.WorkflowVersionCreateRequest  true  "工作流创建参数"
// @Success      200   {object}  response.Response  "创建成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/workflows/versions [post]
func (c *WorkflowOrchestratorController) CreateVersion(ctx *gin.Context) {
	var req dto.WorkflowVersionCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	version, err := c.svc.CreateVersion(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建工作流版本失败: "+err.Error())
		return
	}

	response.Success(ctx, version, "创建成功")
}

// GetVersion godoc
// @Summary      获取工作流版本详情
// @Description  根据 ID 返回工作流版本详情
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "版本 ID"
// @Success      200  {object}  response.Response  "成功"
// @Failure      404  {object}  response.Response  "未找到"
// @Router       /api/workflows/versions/{id} [get]
func (c *WorkflowOrchestratorController) GetVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的版本 ID")
		return
	}

	version, err := c.svc.GetVersion(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "工作流版本不存在")
		return
	}

	response.Success(ctx, version, "查询成功")
}

// ListVersions godoc
// @Summary      工作流版本列表
// @Description  按 workflow_id 查询工作流版本
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        workflow_id  query  string  true  "工作流 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/versions [get]
func (c *WorkflowOrchestratorController) ListVersions(ctx *gin.Context) {
	workflowID := ctx.Query("workflow_id")

	if workflowID == "" {
		status := ctx.Query("status")
		page, pageSize, err := pagination.Parse(ctx)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		list, total, err := c.svc.ListAll(ctx.Request.Context(), workflowID, status, page, pageSize)
		if err != nil {
			response.Error(ctx, http.StatusInternalServerError, err.Error())
			return
		}
		response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
		return
	}

	versions, err := c.svc.ListVersions(ctx.Request.Context(), workflowID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, versions, "查询成功")
}

// UpdateVersion godoc
// @Summary      更新工作流版本
// @Description  更新工作流版本信息
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                           true  "版本 ID"
// @Param        body  body  dto.WorkflowVersionUpdateRequest  true  "更新参数"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/versions/{id} [put]
func (c *WorkflowOrchestratorController) UpdateVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的版本 ID")
		return
	}

	var req dto.WorkflowVersionUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	if err := c.svc.UpdateVersion(ctx.Request.Context(), uint(id), &req); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nil, "更新成功")
}

// PublishVersion godoc
// @Summary      发布工作流版本
// @Description  将工作流版本状态设置为已发布
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "版本 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/versions/{id}/publish [post]
func (c *WorkflowOrchestratorController) PublishVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的版本 ID")
		return
	}

	if err := c.svc.PublishVersion(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nil, "发布成功")
}

// ArchiveVersion godoc
// @Summary      归档工作流版本
// @Description  将工作流版本状态设置为已归档
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "版本 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/versions/{id}/archive [post]
func (c *WorkflowOrchestratorController) ArchiveVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的版本 ID")
		return
	}

	if err := c.svc.ArchiveVersion(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nil, "归档成功")
}

// DeleteVersion godoc
// @Summary      删除工作流版本
// @Description  删除指定的工作流版本
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "版本 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/versions/{id} [delete]
func (c *WorkflowOrchestratorController) DeleteVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的版本 ID")
		return
	}

	if err := c.svc.DeleteVersion(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// Execute godoc
// @Summary      执行工作流
// @Description  触发工作流执行
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.WorkflowExecuteRequest  true  "执行参数"
// @Success      200   {object}  response.Response  "执行成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/workflows/execute [post]
func (c *WorkflowOrchestratorController) Execute(ctx *gin.Context) {
	var req dto.WorkflowExecuteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	exec, err := c.svc.Execute(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, exec, "执行成功")
}

// GetExecution godoc
// @Summary      获取执行详情
// @Description  根据执行 ID 查询执行详情
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  uint  true  "执行 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/executions/{id} [get]
func (c *WorkflowOrchestratorController) GetExecution(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的执行 ID")
		return
	}

	exec, err := c.svc.GetExecution(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "执行记录不存在")
		return
	}

	response.Success(ctx, exec, "查询成功")
}

// ListExecutions godoc
// @Summary      执行实例列表
// @Description  按 workflow_id 分页查询执行实例
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        workflow_id  query  string  false  "工作流 ID"
// @Param        status       query  string  false  "状态"
// @Param        page         query  int     false  "页码"  default(1)
// @Param        page_size    query  int     false  "每页"   default(20)
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/executions [get]
func (c *WorkflowOrchestratorController) ListExecutions(ctx *gin.Context) {
	workflowID := ctx.Query("workflow_id")
	status := ctx.Query("status")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	list, total, err := c.svc.ListExecutions(ctx.Request.Context(), workflowID, status, page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// GetNodeExecutions godoc
// @Summary      获取节点执行明细
// @Description  获取指定执行的所有节点执行记录
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  uint  true  "执行 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/executions/{id}/nodes [get]
func (c *WorkflowOrchestratorController) GetNodeExecutions(ctx *gin.Context) {
	execID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的执行 ID")
		return
	}

	nodes, err := c.svc.GetNodeExecutions(ctx.Request.Context(), uint(execID))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nodes, "查询成功")
}

// StopExecution godoc
// @Summary      停止执行
// @Description  手动停止正在运行的工作流执行
// @Tags         Workflow
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  uint  true  "执行 ID"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/workflows/executions/{id}/stop [post]
func (c *WorkflowOrchestratorController) StopExecution(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的执行 ID")
		return
	}

	if err := c.svc.StopExecution(ctx.Request.Context(), uint(id)); err != nil {
		if utils.IsRecordNotFound(err) {
			response.Error(ctx, http.StatusNotFound, "执行不存在")
			return
		}

		if errors.Is(err, service.ErrExecutionNotRunning) {
			response.Error(ctx, http.StatusConflict, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, nil, "停止成功")
}
