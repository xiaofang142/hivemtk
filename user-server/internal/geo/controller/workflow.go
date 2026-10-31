package controller

import (
	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// WorkflowController GEO 工作流自动化控制器。
type WorkflowController struct {
	svc *service.WorkflowService
}

// NewWorkflowController 构造工作流控制器。
func NewWorkflowController(svc *service.WorkflowService) *WorkflowController {
	return &WorkflowController{svc: svc}
}

// @Summary 工作流列表
// @Tags      workflow
// @Router    /api/geo/workflow/workflows [get]
// List 工作流列表
// GET /geo/workflow/workflows
func (c *WorkflowController) List(ctx *gin.Context) {
	list, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取工作流列表失败")
		return
	}
	response.Success(ctx, list, "获取工作流列表成功")
}

// @Summary 工作流详情
// @Tags      workflow
// @Router    /api/geo/workflow/workflows/{id} [get]
// Get 工作流详情
// GET /geo/workflow/workflows/:id
func (c *WorkflowController) Get(ctx *gin.Context) {
	wf, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取工作流失败")
		return
	}
	response.Success(ctx, wf, "获取工作流成功")
}

// @Summary 创建工作流
// @Tags      workflow
// @Router    /api/geo/workflow/workflows [post]
// Create 创建工作流
// POST /geo/workflow/workflows
func (c *WorkflowController) Create(ctx *gin.Context) {
	var req dto.SaveWorkflowRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	wf, err := c.svc.Create(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建工作流失败")
		return
	}
	response.Success(ctx, wf, "创建工作流成功")
}

// @Summary 更新工作流
// @Tags      workflow
// @Router    /api/geo/workflow/workflows/{id} [put]
// Update 更新工作流
// PUT /geo/workflow/workflows/:id
func (c *WorkflowController) Update(ctx *gin.Context) {
	var req dto.SaveWorkflowRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	wf, err := c.svc.Update(ctx.Request.Context(), ctx.Param("id"), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "更新工作流失败")
		return
	}
	response.Success(ctx, wf, "更新工作流成功")
}

// @Summary 删除工作流
// @Tags      workflow
// @Router    /api/geo/workflow/workflows/{id} [delete]
// Delete 删除工作流
// DELETE /geo/workflow/workflows/:id
func (c *WorkflowController) Delete(ctx *gin.Context) {
	if err := c.svc.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		response.ErrorFromDB(ctx, err, "删除工作流失败")
		return
	}
	response.Success(ctx, nil, "删除工作流成功")
}

// @Summary 执行工作流
// @Tags      workflow
// @Router    /api/geo/workflow/workflows/{id}/run [post]
// Run 执行工作流
// POST /geo/workflow/workflows/:id/run
// 语义约定：result==nil 表示执行未能开始（DB 错误等），返回错误；
// result!= nil 表示执行已落库（含步骤失败场景），由前端按 result.status 渲染。
func (c *WorkflowController) Run(ctx *gin.Context) {
	result, err := c.svc.Run(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if result == nil {
			response.ErrorFromDB(ctx, err, "工作流执行失败")
			return
		}
		response.Success(ctx, result, "工作流执行失败")
		return
	}
	response.Success(ctx, result, "工作流执行完成")
}

// @Summary 执行记录列表
// @Tags      workflow
// @Router    /api/geo/workflow/workflows/{id}/executions [get]
// ListExecutions 执行记录列表
// GET /geo/workflow/workflows/:id/executions
func (c *WorkflowController) ListExecutions(ctx *gin.Context) {
	execs, err := c.svc.ListExecutions(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取执行记录失败")
		return
	}
	response.Success(ctx, execs, "获取执行记录成功")
}

// @Summary 模板列表
// @Tags      workflow
// @Router    /api/geo/workflow/templates [get]
// ListTemplates 模板列表
// GET /geo/workflow/templates
func (c *WorkflowController) ListTemplates(ctx *gin.Context) {
	tpls, err := c.svc.ListTemplates(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取模板列表失败")
		return
	}
	response.Success(ctx, tpls, "获取模板列表成功")
}

// @Summary 创建模板
// @Tags      workflow
// @Router    /api/geo/workflow/templates [post]
// CreateTemplate 创建模板
// POST /geo/workflow/templates
func (c *WorkflowController) CreateTemplate(ctx *gin.Context) {
	var req dto.SaveWorkflowTemplateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	tpl, err := c.svc.CreateTemplate(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建模板失败")
		return
	}
	response.Success(ctx, tpl, "创建模板成功")
}
