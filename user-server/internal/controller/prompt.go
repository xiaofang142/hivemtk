package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type PromptController struct {
	svc *service.PromptService
}

// NewPromptController 创建 Prompt 控制器
func NewPromptController() *PromptController {
	return &PromptController{svc: service.NewPromptService()}
}

// NewPromptControllerWithService 注入 Service（测试用）
func NewPromptControllerWithService(svc *service.PromptService) *PromptController {
	return &PromptController{svc: svc}
}

// GetVersions 获取某个 SOP Node / Prompt ID 的所有历史版本（prompt_candidates）
// GET /api/prompts/:id/versions?sop_node_id=xxx&status=active
func (c *PromptController) GetVersions(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 prompt id")
		return
	}

	status := ctx.Query("status")
	versions, err := c.svc.ListVersions(ctx.Request.Context(), idStr, uint(id), "", status)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取 prompt 版本列表失败")
		return
	}
	response.SuccessWithList(ctx, versions, int64(len(versions)))
}

// PublishRequest 发布新版本请求
type PublishRequest struct {
	SystemPrompt       string `json:"system_prompt" binding:"required"`
	UserPromptTemplate string `json:"user_prompt_template" binding:"required"`
	SOPNodeID          string `json:"sop_node_id,omitempty"`
	SOPID              uint   `json:"sop_id,omitempty"`
	ImprovementNotes   string `json:"improvement_notes,omitempty"`
	Variables          string `json:"variables,omitempty"`
}

// Publish 发布新版本（从 draft → active，自动把旧版本降为 retired）
// POST /api/prompts/:id/publish
func (c *PromptController) Publish(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 prompt id")
		return
	}
	var req PublishRequest
	if !response.BindJSON(ctx, &req) {
		return
	}

	newVersion, err := c.svc.Publish(ctx.Request.Context(), service.PublishRequest{
		SystemPrompt:       req.SystemPrompt,
		UserPromptTemplate: req.UserPromptTemplate,
		SOPNodeID:          req.SOPNodeID,
		SOPID:              req.SOPID,
		ImprovementNotes:   req.ImprovementNotes,
		Variables:          req.Variables,
		ParentID:           uint(id),
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "发布新版本失败")
		return
	}
	response.Success(ctx, newVersion, "发布成功")
}

// GetABExperiments 获取所有 Prompt A/B 实验列表
// GET /api/prompts/ab-experiments?status=running
func (c *PromptController) GetABExperiments(ctx *gin.Context) {
	status := ctx.Query("status")
	experiments, err := c.svc.ListABTests(ctx.Request.Context(), status)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取 A/B 实验列表失败")
		return
	}
	response.SuccessWithList(ctx, experiments, int64(len(experiments)))
}

// PromptCandidateRequest Prompt CRUD 请求体
type PromptCandidateRequest struct {
	Scenario           string `json:"scenario" binding:"required"`
	Version            string `json:"version" binding:"required"`
	Title              string `json:"title" binding:"required"`
	SystemPrompt       string `json:"system_prompt" binding:"required"`
	UserPromptTemplate string `json:"user_prompt_template" binding:"required"`
	SOPNodeID          string `json:"sop_node_id,omitempty"`
	SOPID              uint   `json:"sop_id,omitempty"`
	Status             string `json:"status,omitempty"`
	ImprovementNotes   string `json:"improvement_notes,omitempty"`
	GeneratedBy        string `json:"generated_by,omitempty"`
	Variables          string `json:"variables,omitempty"`
}

// Create 创建 Prompt 候选（模板）
// POST /api/prompts
func (c *PromptController) Create(ctx *gin.Context) {
	var req PromptCandidateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	p := &model.PromptCandidate{
		Scenario:           req.Scenario,
		Version:            req.Version,
		Title:              req.Title,
		SystemPrompt:       req.SystemPrompt,
		UserPromptTemplate: req.UserPromptTemplate,
		SOPNodeID:          req.SOPNodeID,
		SOPID:              req.SOPID,
		Status:             req.Status,
		ImprovementNotes:   req.ImprovementNotes,
		GeneratedBy:        req.GeneratedBy,
	}
	if p.Status == "" {
		p.Status = model.PromptCandidateStatusDraft
	}
	if err := c.svc.Create(ctx.Request.Context(), p); err != nil {
		response.ErrorFromDB(ctx, err, "创建 Prompt 失败")
		return
	}
	response.Success(ctx, p, "创建成功")
}

// Update 更新 Prompt 候选
// PUT /api/prompts/:id
func (c *PromptController) Update(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 prompt id")
		return
	}
	p, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "Prompt 不存在")
		return
	}
	var req PromptCandidateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	if req.Scenario != "" {
		p.Scenario = req.Scenario
	}
	if req.Version != "" {
		p.Version = req.Version
	}
	if req.Title != "" {
		p.Title = req.Title
	}
	if req.SystemPrompt != "" {
		p.SystemPrompt = req.SystemPrompt
	}
	if req.UserPromptTemplate != "" {
		p.UserPromptTemplate = req.UserPromptTemplate
	}
	p.SOPNodeID = req.SOPNodeID
	if req.SOPID > 0 {
		p.SOPID = req.SOPID
	}
	if req.Status != "" {
		p.Status = req.Status
	}
	p.ImprovementNotes = req.ImprovementNotes
	if err := c.svc.Update(ctx.Request.Context(), p); err != nil {
		response.ErrorFromDB(ctx, err, "更新 Prompt 失败")
		return
	}
	response.Success(ctx, p, "更新成功")
}

// Delete 删除 Prompt 候选
// DELETE /api/prompts/:id
func (c *PromptController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 prompt id")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "删除 Prompt 失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// GetByID 按 ID 查询 Prompt 候选
// GET /api/prompts/:id
func (c *PromptController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 prompt id")
		return
	}
	p, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "Prompt 不存在")
		return
	}
	response.Success(ctx, p, "获取成功")
}

// List 分页查询 Prompt 候选（可按 sop_node_id / sop_id / status 过滤）
// GET /api/prompts?page=1&page_size=20&sop_node_id=xxx&sop_id=1&status=active
func (c *PromptController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	status := ctx.Query("status")
	sopNodeID := ctx.Query("sop_node_id")
	sopIDStr := ctx.Query("sop_id")
	var sopID uint
	if sopIDStr != "" {
		if v, err := strconv.ParseUint(sopIDStr, 10, 64); err == nil {
			sopID = uint(v)
		}
	}
	list, total, err := c.svc.List(ctx.Request.Context(), page, pageSize, status, sopNodeID, sopID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取 Prompt 列表失败")
		return
	}
	response.SuccessWithList(ctx, list, total)
}
