package controller

import (
	"net/http"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type RagProductController struct {
	svc *service.RagProductService
}

func NewRagProductController() *RagProductController {
	return &RagProductController{svc: service.NewRagProductServiceFromGlobal()}
}

// @Summary      查询 RAG 产品配置列表
// @Tags      rag-product
// @Router    /api/rag-product-configs [get]
func (c *RagProductController) List(ctx *gin.Context) {
	products, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, products, "ok")
}

// @Summary      RAG 产品配置统计
// @Tags      rag-product
// @Router    /api/rag-product-configs/stats [get]
func (c *RagProductController) Stats(ctx *gin.Context) {
	stats, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, stats, "ok")
}

// @Summary      按 ID 查询 RAG 产品配置
// @Tags      rag-product
// @Router    /api/rag-product-configs/{id} [get]
func (c *RagProductController) Get(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "id required")
		return
	}
	p, err := c.svc.Get(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, p, "ok")
}

func (c *RagProductController) Create(ctx *gin.Context) {
	var p model.RagProduct
	if err := ctx.ShouldBindJSON(&p); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if p.Name == "" {
		response.Error(ctx, http.StatusBadRequest, "name required")
		return
	}
	if err := c.svc.Create(ctx.Request.Context(), &p); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, p, "ok")
}

func (c *RagProductController) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	var p model.RagProduct
	if err := ctx.ShouldBindJSON(&p); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = id
	if err := c.svc.Update(ctx.Request.Context(), &p); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, p, "ok")
}

func (c *RagProductController) Delete(ctx *gin.Context) {
	id := ctx.Param("id")
	if err := c.svc.Delete(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"deleted": id}, "ok")
}
