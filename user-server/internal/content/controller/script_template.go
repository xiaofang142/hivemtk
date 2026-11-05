package controller

import (
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ScriptTemplateController 话术模板控制器
type ScriptTemplateController struct {
	templateService *service.ScriptTemplateService
	syncService     *service.ScriptTemplateSyncService
}

// NewScriptTemplateController 创建话术模板控制器实例。
//
// 句柄由装配点注入（与 #16/#17/#18 同口径）：改前无参构造口一路 new 到仓储，
// 仓储自己摸全局数据库句柄，于是 handler 手上的 ctx 在这条链上没有任何一格被用上。
// 同步服务也由装配点建好：改前 SyncToLibrary handler 每个请求 new 一个，
// 把「装配」下沉到了请求路径上。
func NewScriptTemplateController(templateService *service.ScriptTemplateService, syncService *service.ScriptTemplateSyncService) *ScriptTemplateController {
	return &ScriptTemplateController{templateService: templateService, syncService: syncService}
}

// CreateTemplate 创建话术模板
func (c *ScriptTemplateController) CreateTemplate(ctx *gin.Context) {
	createdBy, ok := callerUserID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "未获取到调用方身份")
		return
	}
	var req service.CreateScriptTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	template, err := c.templateService.CreateTemplate(ctx.Request.Context(), createdBy, &req)
	if errhttp.HandleDBError(ctx, err, "创建话术模板") {
		return
	}

	response.Success(ctx, template, "创建成功")
}

// GetTemplateList 获取话术模板列表
func (c *ScriptTemplateController) GetTemplateList(ctx *gin.Context) {

	category := ctx.Query("category")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.templateService.GetTemplateList(ctx.Request.Context(), category, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取话术模板列表") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetTemplateByID 获取话术模板详情
func (c *ScriptTemplateController) GetTemplateByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的话术 ID")
		return
	}

	template, err := c.templateService.GetTemplateByID(ctx.Request.Context(), uint(id))
	// 改前这里是 response.Error(ctx, 404, err.Error())：把隔离闸的拒绝、SQL 故障
	// 一律说成「话术不存在」，还把内部原文递到出口。现在绑 ctx 而不在本租户 ⇒
	// ErrRecordNotFound ⇒ 404；未绑 ctx ⇒ 500 且只有固定文案。
	if errhttp.HandleDBError(ctx, err, "获取话术模板详情") {
		return
	}

	response.Success(ctx, template, "获取成功")
}

// UpdateTemplate 更新话术模板
func (c *ScriptTemplateController) UpdateTemplate(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的话术 ID")
		return
	}

	var req service.UpdateTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	template, err := c.templateService.UpdateTemplate(ctx.Request.Context(), uint(id), &req)
	if errhttp.HandleDBError(ctx, err, "更新话术模板") {
		return
	}

	response.Success(ctx, template, "更新成功")
}

// DeleteTemplate 删除话术模板
func (c *ScriptTemplateController) DeleteTemplate(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的话术 ID")
		return
	}

	if errhttp.HandleDBError(ctx, c.templateService.DeleteTemplate(ctx.Request.Context(), uint(id)), "删除话术模板") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetCategories 获取话术分类
func (c *ScriptTemplateController) GetCategories(ctx *gin.Context) {

	categories, err := c.templateService.GetCategories(ctx.Request.Context())
	if errhttp.HandleDBError(ctx, err, "获取话术分类") {
		return
	}

	response.Success(ctx, categories, "获取成功")
}

// SearchTemplates 搜索话术
func (c *ScriptTemplateController) SearchTemplates(ctx *gin.Context) {

	keyword := ctx.Query("keyword")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.templateService.SearchTemplates(ctx.Request.Context(), keyword, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "搜索话术") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetPublicTemplates 获取公开话术模板
func (c *ScriptTemplateController) GetPublicTemplates(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.templateService.GetPublicTemplates(ctx.Request.Context(), page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取公开话术") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// RecommendScript 推荐话术
func (c *ScriptTemplateController) RecommendScript(ctx *gin.Context) {

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	// 请求体里的 session_id 从来没有任何下游用过（它唯一的归宿是那张
	// 零写入点的 script_recommendations 表，见 model-list #19 决策 1），
	// 改前把它收下再丢掉，等于给调用方一个「会留痕」的错觉。
	templates, err := c.templateService.RecommendScript(ctx.Request.Context(), req.Message)
	if errhttp.HandleDBError(ctx, err, "推荐话术") {
		return
	}

	response.Success(ctx, templates, "获取推荐成功")
}

// SyncToLibrary 管理端模板 → 运行时话术库（按调用方租户同步）
func (c *ScriptTemplateController) SyncToLibrary(ctx *gin.Context) {
	stats, err := c.syncService.SyncToLibrary(ctx.Request.Context())
	if errhttp.HandleDBError(ctx, err, "同步话术模板") {
		return
	}
	response.Success(ctx, stats, "同步完成")
}
