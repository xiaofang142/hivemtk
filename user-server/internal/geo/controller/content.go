package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ContentController GEO 内容管理控制器。
type ContentController struct {
	svc *service.ContentService
}

// NewContentController 构造内容控制器。
func NewContentController(svc *service.ContentService) *ContentController {
	return &ContentController{svc: svc}
}

// @Summary 生成 GEO 内容
// @Tags      content
// @Router    /api/geo/content/generate [post]
// GenerateContent 生成 GEO 内容
// POST /geo/content/generate
func (c *ContentController) GenerateContent(ctx *gin.Context) {
	var req dto.GenerateContentRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.GenerateContent(ctx.Request.Context(), req.Lang, req.Keyword, req.BrandName, req.Advantages, req.WordCount, req.Style)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "内容生成失败")
		return
	}
	response.Success(ctx, result, "内容生成成功")
}

// @Summary 批量生成
// @Tags      content
// @Router    /api/geo/content/batch-generate [post]
// GenerateContentBatch 批量生成
// POST /geo/content/batch-generate
// 同步有界批（上限 BatchGenerateMaxKeywords=20），逐条容错，失败单条回传 error
// 由前端标记可重试；超上限截断回传 truncated_to。
func (c *ContentController) GenerateContentBatch(ctx *gin.Context) {
	var req dto.BatchGenerateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result := c.svc.GenerateContentBatch(ctx.Request.Context(), req)
	response.Success(ctx, result, "批量生成完成")
}

// @Summary 优化内容
// @Tags      content
// @Router    /api/geo/content/optimize [post]
// OptimizeContent 优化内容
// POST /geo/content/optimize
func (c *ContentController) OptimizeContent(ctx *gin.Context) {
	var req dto.OptimizeContentRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.OptimizeContent(ctx.Request.Context(), req.ArticleID, req.Content, req.BrandName, req.Advantages)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "内容优化失败")
		return
	}
	response.Success(ctx, result, "内容优化成功")
}

// @Summary 内容评分
// @Tags      content
// @Router    /api/geo/content/score [post]
// ScoreContent 内容评分
// POST /geo/content/score
func (c *ContentController) ScoreContent(ctx *gin.Context) {
	var req dto.ScoreContentRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.ScoreContent(ctx.Request.Context(), req.ArticleID, req.Content, req.BrandName, req.Keyword)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "内容评分失败")
		return
	}
	response.Success(ctx, result, "内容评分成功")
}

// @Summary 增强 E-E-A-T
// @Tags      content
// @Router    /api/geo/content/eeat [post]
// EnhanceEEAT 增强 E-E-A-T
// POST /geo/content/eeat
func (c *ContentController) EnhanceEEAT(ctx *gin.Context) {
	var req struct {
		ArticleID  string   `json:"article_id"`
		Content    string   `json:"content"`
		BrandName  string   `json:"brand_name"`
		Advantages []string `json:"advantages"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.EnhanceEEAT(ctx.Request.Context(), req.ArticleID, req.Content, req.BrandName, req.Advantages)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "E-E-A-T 增强失败")
		return
	}
	response.Success(ctx, result, "E-E-A-T 增强成功")
}

// @Summary 生成结构化数据 Schema
// @Tags      content
// @Router    /api/geo/content/schema [post]
// GenerateSchema 生成结构化数据 Schema
// POST /geo/content/schema
func (c *ContentController) GenerateSchema(ctx *gin.Context) {
	var req struct {
		ArticleID   string `json:"article_id"`
		BrandName   string `json:"brand_name"`
		Description string `json:"description"`
		Domain      string `json:"domain"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.GenerateSchema(ctx.Request.Context(), req.ArticleID, req.BrandName, req.Description, req.Domain)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "Schema 生成失败")
		return
	}
	response.Success(ctx, result, "Schema 生成成功")
}

// @Summary 内容查重
// @Tags      content
// @Router    /api/geo/content/uniqueness [post]
// CheckUniqueness 内容查重
// POST /geo/content/uniqueness
func (c *ContentController) CheckUniqueness(ctx *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.CheckUniqueness(ctx.Request.Context(), req.Content)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "内容查重失败")
		return
	}
	response.Success(ctx, result, "内容查重成功")
}

// @Summary      获取文章列表
// @Tags      content
// @Router    /api/geo/content/list [get]
// GetArticleList 获取文章列表
// GET /geo/content/list
func (c *ContentController) GetArticleList(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	list, total, err := c.svc.GetArticleList(ctx.Request.Context(), page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取文章列表失败")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(limit), total)
}

// @Summary 获取文章详情
// @Tags      content
// @Router    /api/geo/content/{id} [get]
// GetArticleByID 获取文章详情
// GET /geo/content/:id
func (c *ContentController) GetArticleByID(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "文章ID不能为空")
		return
	}
	result, err := c.svc.GetArticleByID(ctx.Request.Context(), id)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取文章详情失败")
		return
	}
	response.Success(ctx, result, "获取文章详情成功")
}
