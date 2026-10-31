package controller

import (
	"strconv"
	"strings"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// KBController GEO 知识库控制器。
type KBController struct {
	svc *service.KBService
}

// NewKBController 构造知识库控制器。
func NewKBController(svc *service.KBService) *KBController {
	return &KBController{svc: svc}
}

// @Summary 文档列表
// @Tags      k-b
// @Router    /api/geo/kb/documents [get]
// List 文档列表
// GET /geo/kb/documents
func (c *KBController) List(ctx *gin.Context) {
	docs, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取文档列表失败")
		return
	}
	response.Success(ctx, docs, "获取文档列表成功")
}

// @Summary 文档详情
// @Tags      k-b
// @Router    /api/geo/kb/documents/{id} [get]
// Get 文档详情
// GET /geo/kb/documents/:id
func (c *KBController) Get(ctx *gin.Context) {
	doc, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取文档失败")
		return
	}
	response.Success(ctx, doc, "获取文档成功")
}

// @Summary 新增/更新文档
// @Tags      k-b
// @Router    /api/geo/kb/documents [post]
// Save 新增/更新文档
// POST /geo/kb/documents
func (c *KBController) Save(ctx *gin.Context) {
	var req dto.SaveKnowledgeDocumentRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	doc, err := c.svc.Save(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "保存文档失败")
		return
	}
	response.Success(ctx, doc, "保存文档成功")
}

// @Summary 删除文档
// @Tags      k-b
// @Router    /api/geo/kb/documents/{id} [delete]
// Delete 删除文档
// DELETE /geo/kb/documents/:id
func (c *KBController) Delete(ctx *gin.Context) {
	if err := c.svc.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		response.ErrorFromDB(ctx, err, "删除文档失败")
		return
	}
	response.Success(ctx, nil, "删除文档成功")
}

// @Summary 关键词检索
// @Tags      k-b
// @Router    /api/geo/kb/search [get]
// Search 关键词检索
// GET /geo/kb/search?q=&limit=
func (c *KBController) Search(ctx *gin.Context) {
	q := strings.TrimSpace(ctx.Query("q"))
	if q == "" {
		response.InvalidParameterError(ctx, "q", "检索词不能为空")
		return
	}
	limit := 10
	if l := ctx.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 50 {
			limit = v
		}
	}
	results, err := c.svc.Search(ctx.Request.Context(), q, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "检索失败")
		return
	}
	response.Success(ctx, results, "检索完成")
}

// @Summary 知识库问答
// @Tags      k-b
// @Router    /api/geo/kb/ask [post]
// Ask 知识库问答
// POST /geo/kb/ask
func (c *KBController) Ask(ctx *gin.Context) {
	var req dto.KBAskRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	resp, err := c.svc.Ask(ctx.Request.Context(), req.Question)
	if err != nil {
		response.ErrorFromDB(ctx, err, "问答失败")
		return
	}
	response.Success(ctx, resp, "问答完成")
}
