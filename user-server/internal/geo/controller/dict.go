package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// DictController GEO 通用配置字典（数据库驱动的硬编码替代，独立部署可维护）
type DictController struct {
	svc *service.DictService
}

func NewDictController(svc *service.DictService) *DictController {
	return &DictController{svc: svc}
}

// List GET /geo/dicts?category=xxx
func (c *DictController) List(ctx *gin.Context) {
	category := ctx.Query("category")
	if category == "" {
		response.Error(ctx, http.StatusBadRequest, "category 必填")
		return
	}
	list, err := c.svc.List(ctx.Request.Context(), category)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"total": len(list), "dicts": list}, "ok")
}

type dictSetRequest struct {
	Category string `json:"category" binding:"required"`
	Key      string `json:"key" binding:"required"`
	Value    string `json:"value"`
	Remark   string `json:"remark"`
	Active   *bool  `json:"active"`
	Sort     int    `json:"sort"`
}

// Set PUT /geo/dicts（幂等写入）
func (c *DictController) Set(ctx *gin.Context) {
	var req dictSetRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	if err := c.svc.Set(ctx.Request.Context(), req.Category, req.Key, req.Value, req.Remark, active, req.Sort); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"category": req.Category, "key": req.Key}, "ok")
}

// Delete DELETE /geo/dicts/:category/:key
func (c *DictController) Delete(ctx *gin.Context) {
	category := ctx.Param("category")
	key := ctx.Param("key")
	if category == "" || key == "" {
		response.Error(ctx, http.StatusBadRequest, "category/key 必填")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), category, key); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"category": category, "key": key}, "ok")
}
