package controller

import (
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SOPTemplateController SOP 模板控制器
type SOPTemplateController struct {
	svc *service.SOPTemplateService
}

// NewSOPTemplateController 创建 SOP 模板控制器
func NewSOPTemplateController() *SOPTemplateController {
	return &SOPTemplateController{
		svc: service.NewSOPTemplateServiceDefault(),
	}
}

// RegisterRoutes 注册路由
func (c *SOPTemplateController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/sop-templates")

	g.GET("", c.List)
	g.GET("/:id", c.Get)
	g.POST("/match", c.Match)

	admin := router.Group("/sop-templates", middleware.AdminAuthMiddleware())
	{
		admin.POST("", c.Create)
		admin.PUT("/:id", c.Update)
		admin.PATCH("/:id/toggle", c.Toggle)
		admin.DELETE("/:id", c.Delete)
	}
}

// @Summary      列表查询
// @Tags      s-o-p-template
// @Router    /api/sop-templates [get]
// List 列表查询
func (c *SOPTemplateController) List(ctx *gin.Context) {
	// P0-3：kb_id 不许被静默忽略（同 faq.go List 的同一条判据：
	// sop_templates 没有 kb_id 归属列，静默丢弃参数 = 显示错数据）。
	if strings.TrimSpace(ctx.Query("kb_id")) != "" {
		response.Error(ctx, http.StatusBadRequest,
			"kb_id 过滤不支持：SOP 模板列表是全局列表（sop_templates 无 kb_id 归属）；按知识库预览请用 GET /api/knowledge-bases/:id/items")
		return
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	filter := dto.SOPTemplateFilter{
		Keyword:  ctx.Query("keyword"),
		Intent:   ctx.Query("intent"),
		Stage:    ctx.Query("stage"),
		Page:     page,
		PageSize: pageSize,
	}
	if enabledStr := ctx.Query("enabled"); enabledStr != "" {
		if v, err := strconv.ParseBool(enabledStr); err == nil {
			filter.Enabled = &v
		}
	}
	list, total, err := c.svc.List(ctx.Request.Context(), filter)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// @Summary      按 ID 查询 SOP 模板
// @Tags      s-o-p-template
// @Router    /api/sop-templates/{id} [get]
// Get 详情
func (c *SOPTemplateController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP 模板 ID")
		return
	}
	tpl, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "SOP 模板不存在")
		return
	}
	response.Success(ctx, tpl, "查询成功")
}

type sopTemplateCreateReq struct {
	Name       string  `json:"name" binding:"required"`
	Intent     string  `json:"intent" binding:"required"`
	Stage      string  `json:"stage" binding:"required"`
	Template   string  `json:"template" binding:"required"`
	Vars       string  `json:"vars"`
	Priority   int     `json:"priority"`
	Confidence float64 `json:"confidence"`
	Enabled    *bool   `json:"enabled"`
	AgentID    uint    `json:"agent_id" binding:"required"`
}

// @Summary      创建 SOP 模板
// @Tags      s-o-p-template
// @Router    /api/sop-templates [post]
// Create 新增
func (c *SOPTemplateController) Create(ctx *gin.Context) {
	var req sopTemplateCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agentID := req.AgentID
	tpl := &model.SOPTemplate{
		Name:       req.Name,
		Intent:     req.Intent,
		Stage:      req.Stage,
		Template:   req.Template,
		Vars:       req.Vars,
		Priority:   req.Priority,
		Confidence: req.Confidence,
		Enabled:    req.Enabled,
		AgentID:    &agentID,
	}
	if err := c.svc.Create(ctx.Request.Context(), tpl); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, tpl, "创建成功")
}

// @Summary      更新 SOP 模板
// @Tags      s-o-p-template
// @Router    /api/sop-templates/{id} [put]
// Update 更新
func (c *SOPTemplateController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP 模板 ID")
		return
	}
	var req sopTemplateCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agentID := req.AgentID
	tpl := &model.SOPTemplate{
		Name:       req.Name,
		Intent:     req.Intent,
		Stage:      req.Stage,
		Template:   req.Template,
		Vars:       req.Vars,
		Priority:   req.Priority,
		Confidence: req.Confidence,
		Enabled:    req.Enabled,
		AgentID:    &agentID,
	}
	if err := c.svc.Update(ctx.Request.Context(), uint(id), tpl); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "更新成功")
}

// @Summary      状态已更新
// @Tags      s-o-p-template
// @Router    /api/sop-templates/{id}/toggle [patch]
// Toggle 切换启用状态（列表页开关专用，只收 enabled，不走全量 required 校验）
func (c *SOPTemplateController) Toggle(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP 模板 ID")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if err := c.svc.SetEnabled(ctx.Request.Context(), uint(id), *req.Enabled); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id, "enabled": *req.Enabled}, "状态已更新")
}

// @Summary      删除 SOP 模板
// @Tags      s-o-p-template
// @Router    /api/sop-templates/{id} [delete]
// Delete 删除
func (c *SOPTemplateController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP 模板 ID")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

type sopTemplateMatchReq struct {
	Intent  string `json:"intent" binding:"required"`
	Stage   string `json:"stage"`
	TopK    int    `json:"top_k"`
	AgentID uint   `json:"agent_id" binding:"required"`
}

// @Summary 按 (agent_id, intent, stage) 匹配 (Task 16 强 1对1 改造后)
// @Tags      s-o-p-template
// @Router    /api/sop-templates/match [post]
// Match 按 (agent_id, intent, stage) 匹配 (Task 16 强 1对1 改造后)
func (c *SOPTemplateController) Match(ctx *gin.Context) {
	var req sopTemplateMatchReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.AgentID == 0 {
		response.Error(ctx, http.StatusBadRequest, "agent_id 必填且 > 0 (Task 16 强 1对1)")
		return
	}
	matches, err := c.svc.MatchByAgent(ctx.Request.Context(), req.AgentID, req.Intent, req.Stage, req.TopK)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, matches, "匹配成功")
}
