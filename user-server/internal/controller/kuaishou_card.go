package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
)

// KuaishouCardController 快手卡片控制器
type KuaishouCardController struct {
	service cardsvc.KuaishouCardService
}

// NewKuaishouCardController 创建快手卡片控制器实例
func NewKuaishouCardController(service cardsvc.KuaishouCardService) *KuaishouCardController {
	return &KuaishouCardController{
		service: service,
	}
}

// @Summary      创建快手卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou/create [post]
// Create 创建快手卡片
func (c *KuaishouCardController) Create(ctx *gin.Context) {
	var req dto.KuaishouCardCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Create(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "创建快手卡片") {
		return
	}

	response.Success(ctx, card, response.ErrSuccess)
}

// @Summary      更新快手卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou-card/{id} [put]
// Update 更新快手卡片
func (c *KuaishouCardController) Update(ctx *gin.Context) {
	var req dto.KuaishouCardUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	// 路由注册为 /kuaishou/update（无 :id），ID 只在 body；有 :id 时才做一致性校验
	if idStr := ctx.Param("id"); idStr != "" {
		idFromURI, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
			return
		}
		if req.ID == 0 {
			req.ID = uint(idFromURI)
		} else if req.ID != uint(idFromURI) {
			response.Error(ctx, http.StatusBadRequest, response.ErrIDMismatch, "JSON 中的 ID 与 URI 参数中的 ID 不匹配")
			return
		}
	}
	if req.ID == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, "id 必填")
		return
	}

	card, err := c.service.Update(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "更新快手卡片") {
		return
	}

	response.Success(ctx, card, response.ErrUpdateSuccess)
}

// @Summary      删除快手卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou-card/{id} [delete]
// Delete 删除快手卡片
func (c *KuaishouCardController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	err = c.service.Delete(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "删除快手卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrDeleteSuccess)
}

// @Summary      根据ID获取快手卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou-card/{id} [get]
// GetByID 根据ID获取快手卡片
func (c *KuaishouCardController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	card, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, response.ErrResourceNotFound, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrGetSuccess)
}

// @Summary      获取快手卡片列表
// @Tags      kuaishou-card
// @Router    /api/kuaishou-card/list [get]
// GetList 获取快手卡片列表
func (c *KuaishouCardController) GetList(ctx *gin.Context) {
	var req dto.KuaishouCardListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	list, err := c.service.GetList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrGetListFailed, err.Error())
		return
	}

	response.Success(ctx, list, response.ErrGetSuccess)
}

// ViewCard 浏览卡片
func (c *KuaishouCardController) ViewCard(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	err = c.service.ViewCard(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "浏览快手卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrSuccess)
}

// @Summary      点赞卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou/like/{id} [post]
// LikeCard 点赞卡片
func (c *KuaishouCardController) LikeCard(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	err = c.service.LikeCard(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "点赞快手卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrSuccess)
}

// @Summary      分享卡片
// @Tags      kuaishou-card
// @Router    /api/kuaishou/share/{id} [post]
// ShareCard 分享卡片
func (c *KuaishouCardController) ShareCard(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	platform := ctx.Query("platform")
	if platform == "" {
		platform = "unknown"
	}

	card, err := c.service.ShareCard(ctx.Request.Context(), uint(id), platform)
	if HandleDBError(ctx, err, "分享快手卡片") {
		return
	}

	response.Success(ctx, card, response.ErrSuccess)
}

// @Summary      生成短链接
// @Tags      kuaishou-card
// @Router    /api/kuaishou/{id}/generate-short-link [post]
// GenerateShortLink 生成短链接
func (c *KuaishouCardController) GenerateShortLink(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID", err.Error())
		return
	}

	card, err := c.service.GetCardModelByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, response.ErrResourceNotFound, err.Error())
		return
	}

	err = c.service.GenerateShortLink(ctx.Request.Context(), card)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrBusinessError, err.Error())
		return
	}

	updatedCard, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取快手卡片") {
		return
	}

	response.Success(ctx, updatedCard, response.ErrSuccess)
}

// @Summary      渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
// @Tags      kuaishou-card
// @Router    /share/kuaishou/{id} [get]
// SharePage 渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
func (c *KuaishouCardController) SharePage(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.String(http.StatusBadRequest, "无效的卡片ID")
		return
	}
	renderCardChatPage(ctx, c.service.GenerateCardChatPage, uint(id), buildBaseURL(ctx))
}
