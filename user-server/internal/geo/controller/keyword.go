package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// KeywordController GEO 关键词管理控制器。
type KeywordController struct {
	svc *service.KeywordService
}

// NewKeywordController 构造关键词控制器。
func NewKeywordController(svc *service.KeywordService) *KeywordController {
	return &KeywordController{svc: svc}
}

// MineKeywords 挖掘关键词
// POST /geo/keywords/mine
func (c *KeywordController) MineKeywords(ctx *gin.Context) {
	var req dto.MineKeywordsRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.MineKeywords(ctx.Request.Context(), req.SeedWords, req.Mode, req.BrandName, req.Advantages)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "关键词挖掘失败")
		return
	}
	response.Success(ctx, result, "关键词挖掘成功")
}

// SemanticExpand 语义扩展关键词
// POST /geo/keywords/expand
func (c *KeywordController) SemanticExpand(ctx *gin.Context) {
	var req dto.SemanticExpandRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.SemanticExpand(ctx.Request.Context(), req.Keywords, req.BrandName)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "语义扩展失败")
		return
	}
	response.Success(ctx, result, "语义扩展成功")
}

// TopicCluster 关键词话题集群
// POST /geo/keywords/cluster
func (c *KeywordController) TopicCluster(ctx *gin.Context) {
	var req dto.TopicClusterRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.TopicCluster(ctx.Request.Context(), req.Keywords, req.BrandName)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "话题集群失败")
		return
	}
	response.Success(ctx, result, "话题集群成功")
}

// GetKeywordList 获取关键词列表
// GET /geo/keywords/list
func (c *KeywordController) GetKeywordList(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	search := ctx.Query("search")
	source := ctx.Query("source")
	layer := ctx.Query("layer")

	list, total, err := c.svc.GetKeywordList(ctx.Request.Context(), page, limit, search, source, layer)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取关键词列表失败")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(limit), total)
}

// DeleteKeyword 删除关键词
// DELETE /geo/keywords/:id
func (c *KeywordController) DeleteKeyword(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "关键词ID不能为空")
		return
	}
	if err := c.svc.DeleteKeyword(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, "删除关键词失败")
		return
	}
	response.Success(ctx, nil, "删除关键词成功")
}
