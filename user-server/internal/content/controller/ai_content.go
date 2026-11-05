package controller

import (
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/errhttp"

	"github.com/gin-gonic/gin"
)

// AIContentController AI内容控制器
type AIContentController struct {
	contentService  *service.AIContentService
	templateService *service.PromptTemplateService
}

// NewAIContentController 创建AI内容控制器实例
func NewAIContentController(contentService *service.AIContentService, templateService *service.PromptTemplateService) *AIContentController {
	return &AIContentController{contentService: contentService, templateService: templateService}
}

// aiUserID 取调用者身份（生成记录与模板的归属门都建在这个值上）。
//
// 用 `ctx.Get` + `convertToUint` 而不是 `ctx.GetUint`：中间件存进 gin ctx 的是 any
// （JWTAuthMiddleware 存 uint，TeamJWTAuthMiddleware 存 claims 里的 float64/string），
// `GetUint` 只认 uint、其余类型返回 0——那会让归属门在「身份取不到」时退化成
// 「查 user_id=0 的行」而不是拒绝。这里宁可回 401。
func aiUserID(ctx *gin.Context) (uint, bool) {
	v, exists := ctx.Get("user_id")
	uid := convertToUint(v)
	if !exists || uid == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return 0, false
	}
	return uid, true
}

// GenerateContent 生成内容
func (c *AIContentController) GenerateContent(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}

	var req service.GenerateContentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	result, err := c.contentService.GenerateContent(ctx.Request.Context(), uid, &req)
	if errhttp.HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, result, "生成成功")
}

// CreateHistory 创建生成历史记录（不调用外部 AI 服务，直接保存到数据库）
func (c *AIContentController) CreateHistory(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}

	var req service.CreateHistoryRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	record, err := c.contentService.CreateHistory(ctx.Request.Context(), uid, &req)
	if errhttp.HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, record, "创建成功")
}

// GetGenerationHistory 获取生成历史
func (c *AIContentController) GetGenerationHistory(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))

	filters := make(map[string]any)
	if recordType := ctx.Query("type"); recordType != "" {
		filters["type"] = recordType
	}
	if isSaved := ctx.Query("is_saved"); isSaved != "" {
		filters["is_saved"] = isSaved == "true"
	}
	if isFavorite := ctx.Query("is_favorite"); isFavorite != "" {
		filters["is_favorite"] = isFavorite == "true"
	}

	records, total, err := c.contentService.GetGenerationHistory(ctx.Request.Context(), uid, page, pageSize, filters)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      records,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetRecordByID 获取生成记录详情
func (c *AIContentController) GetRecordByID(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}

	id, valid := parseRecordID(ctx)
	if !valid {
		return
	}

	record, err := c.contentService.GetRecordByID(ctx.Request.Context(), id, uid)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, record, "获取成功")
}

// parseRecordID 路径参数 :id。0 不是合法主键，和解析失败一样回 400。
func parseRecordID(ctx *gin.Context) (uint, bool) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的记录ID")
		return 0, false
	}
	return uint(id), true
}

// SaveRecord 保存生成记录（admin 路由：管理员整理本租户内容库，不按 user_id 收窄）
func (c *AIContentController) SaveRecord(ctx *gin.Context) {
	if _, ok := aiUserID(ctx); !ok {
		return
	}
	id, valid := parseRecordID(ctx)
	if !valid {
		return
	}

	if errhttp.HandleDBError(ctx, c.contentService.SaveRecord(ctx.Request.Context(), id), "保存记录") {
		return
	}

	response.Success(ctx, nil, "保存成功")
}

// FavoriteRecord 收藏生成记录
func (c *AIContentController) FavoriteRecord(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}
	id, valid := parseRecordID(ctx)
	if !valid {
		return
	}

	var req struct {
		IsFavorite bool `json:"is_favorite"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		req.IsFavorite = true
	}

	if errhttp.HandleDBError(ctx, c.contentService.FavoriteRecord(ctx.Request.Context(), id, uid, req.IsFavorite), "收藏记录") {
		return
	}

	response.Success(ctx, nil, "操作成功")
}

// RateRecord 评分生成记录
func (c *AIContentController) RateRecord(ctx *gin.Context) {
	uid, ok := aiUserID(ctx)
	if !ok {
		return
	}
	id, valid := parseRecordID(ctx)
	if !valid {
		return
	}

	var req struct {
		Rating int `json:"rating" binding:"required,min=1,max=5"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请提供1-5的评分")
		return
	}

	if errhttp.HandleDBError(ctx, c.contentService.RateRecord(ctx.Request.Context(), id, uid, req.Rating), "评分记录") {
		return
	}

	response.Success(ctx, nil, "评分成功")
}

// DeleteRecord 删除生成记录（admin 路由：管理员整理本租户内容库，不按 user_id 收窄）
func (c *AIContentController) DeleteRecord(ctx *gin.Context) {
	if _, ok := aiUserID(ctx); !ok {
		return
	}
	id, valid := parseRecordID(ctx)
	if !valid {
		return
	}

	if errhttp.HandleServiceError(ctx, c.contentService.DeleteRecord(ctx.Request.Context(), id)) {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetTemplates 获取模板列表
func (c *AIContentController) GetTemplates(ctx *gin.Context) {
	templateType := ctx.Query("type")

	templates, err := c.templateService.GetTemplates(ctx.Request.Context(), templateType)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, templates, "获取成功")
}

// GetTemplateByID 获取模板详情
func (c *AIContentController) GetTemplateByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的模板ID")
		return
	}

	template, err := c.templateService.GetTemplateByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, template, "获取成功")
}

// CreateTemplate 创建模板
func (c *AIContentController) CreateTemplate(ctx *gin.Context) {

	var req service.CreateTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	template, err := c.templateService.CreateTemplate(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, template, "创建成功")
}

// UpdateTemplate 更新模板
func (c *AIContentController) UpdateTemplate(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的模板ID")
		return
	}

	var req service.CreateTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	template, err := c.templateService.UpdateTemplate(ctx.Request.Context(), uint(id), &req)
	if errhttp.HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, template, "更新成功")
}

// DeleteTemplate 删除模板
func (c *AIContentController) DeleteTemplate(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的模板ID")
		return
	}

	if errhttp.HandleServiceError(ctx, c.templateService.DeleteTemplate(ctx.Request.Context(), uint(id))) {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetTemplateTypes 获取模板类型列表
func (c *AIContentController) GetTemplateTypes(ctx *gin.Context) {
	types := c.templateService.GetTemplateTypes()
	response.Success(ctx, types, "获取成功")
}

func convertToUint(v any) uint {
	switch val := v.(type) {
	case uint:
		return val
	case int:
		return uint(val)
	case int64:
		return uint(val)
	case float64:
		return uint(val)
	default:
		return 0
	}
}
