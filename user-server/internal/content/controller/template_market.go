package controller

import (
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// TemplateMarketController 模板市场控制器
type TemplateMarketController struct {
	marketService *service.TemplateMarketService
}

// NewTemplateMarketController 创建模板市场控制器实例
func NewTemplateMarketController(marketService *service.TemplateMarketService) *TemplateMarketController {
	return &TemplateMarketController{marketService: marketService}
}

// callerUserID 取当前登录用户。
//
// 必须是「取不到就 401」：JWT 中间件存的类型不止一种（user 侧 uint、团队侧 float64/string），
// 改前这里直接 `userID.(uint)`，团队身份进来就是 panic ⇒ 500 页而不是鉴权失败。
func callerUserID(ctx *gin.Context) (uint, bool) {
	raw, exists := ctx.Get("user_id")
	if !exists {
		return 0, false
	}
	id := convertToUint(raw)
	return id, id != 0
}

// GetTemplateList 获取模板列表
func (c *TemplateMarketController) GetTemplateList(ctx *gin.Context) {
	category := ctx.Query("category")
	templateType := ctx.Query("type")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.marketService.GetTemplateList(ctx.Request.Context(), category, templateType, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取模板列表") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetTemplateByID 获取模板详情
func (c *TemplateMarketController) GetTemplateByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的模板 ID")
		return
	}

	template, err := c.marketService.GetTemplateByID(ctx.Request.Context(), uint(id))
	if errhttp.HandleDBError(ctx, err, "获取模板详情") {
		return
	}

	response.Success(ctx, template, "获取成功")
}

// DownloadTemplate 下载模板
func (c *TemplateMarketController) DownloadTemplate(ctx *gin.Context) {
	userID, ok := callerUserID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的模板 ID")
		return
	}

	template, err := c.marketService.DownloadTemplate(ctx.Request.Context(), userID, uint(id))
	if errhttp.HandleDBError(ctx, err, "下载模板") {
		return
	}

	response.Success(ctx, template, "下载成功")
}

// GetOfficialTemplates 获取官方模板
func (c *TemplateMarketController) GetOfficialTemplates(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.marketService.GetOfficialTemplates(ctx.Request.Context(), page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取官方模板") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// SearchTemplates 搜索模板
func (c *TemplateMarketController) SearchTemplates(ctx *gin.Context) {
	keyword := ctx.Query("keyword")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	templates, total, err := c.marketService.SearchTemplates(ctx.Request.Context(), keyword, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "搜索模板") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      templates,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetMyDownloads 获取我的下载
func (c *TemplateMarketController) GetMyDownloads(ctx *gin.Context) {
	userID, ok := callerUserID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	records, total, err := c.marketService.GetMyDownloads(ctx.Request.Context(), userID, page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取下载记录") {
		return
	}

	response.Success(ctx, gin.H{
		"list":      records,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// CreateTemplate 创建模板
func (c *TemplateMarketController) CreateTemplate(ctx *gin.Context) {
	var template model.MarketTemplate
	if err := ctx.ShouldBindJSON(&template); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	// 建模板口对所有登录用户开放，官方标记不能让请求体自己填
	template.IsOfficial = false

	if errhttp.HandleServiceError(ctx, c.marketService.CreateTemplate(ctx.Request.Context(), &template)) {
		return
	}

	response.Success(ctx, template, "创建成功")
}

// RateTemplate 为模板评分
func (c *TemplateMarketController) RateTemplate(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的模板 ID")
		return
	}

	var req struct {
		Rating float64 `json:"rating" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if errhttp.HandleDBError(ctx, c.marketService.RateTemplate(ctx.Request.Context(), uint(id), req.Rating), "评分模板") {
		return
	}

	response.Success(ctx, gin.H{
		"template_id": id,
		"rating":      req.Rating,
	}, "评分成功")
}
