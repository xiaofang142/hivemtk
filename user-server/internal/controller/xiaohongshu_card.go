package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
)

// XiaohongshuCardController 小红书卡片控制器
type XiaohongshuCardController struct {
	service cardsvc.XiaohongshuCardService
}

// NewXiaohongshuCardController 创建小红书卡片控制器实例
func NewXiaohongshuCardController(service cardsvc.XiaohongshuCardService) *XiaohongshuCardController {
	return &XiaohongshuCardController{
		service: service,
	}
}

// @Summary      创建小红书卡片
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu/create [post]
// Create 创建小红书卡片
func (c *XiaohongshuCardController) Create(ctx *gin.Context) {
	var req dto.XiaohongshuCardCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Create(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrCreateFailed, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrSuccess)
}

// @Summary      更新小红书卡片
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu-card/{id} [put]
// Update 更新小红书卡片
func (c *XiaohongshuCardController) Update(ctx *gin.Context) {
	var req dto.XiaohongshuCardUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Update(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrUpdateFailed, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrUpdateSuccess)
}

// @Summary      删除小红书卡片
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu-card/{id} [delete]
// Delete 删除小红书卡片
func (c *XiaohongshuCardController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.Delete(ctx.Request.Context(), uint(id)), "删除小红书卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrDeleteSuccess)
}

// @Summary      根据ID获取小红书卡片
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu-card/{id} [get]
// GetByID 根据ID获取小红书卡片
func (c *XiaohongshuCardController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	card, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	response.Success(ctx, card, response.ErrGetSuccess)
}

// @Summary      获取小红书卡片列表
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu-card/list [get]
// GetList 获取小红书卡片列表
func (c *XiaohongshuCardController) GetList(ctx *gin.Context) {
	var req dto.XiaohongshuCardListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	cards, err := c.service.GetList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrGetFailed, err.Error())
		return
	}

	response.Success(ctx, cards, response.ErrGetSuccess)
}

// @Summary      为小红书卡片生成短链
// @Tags      xiaohongshu-card
// @Router    /api/xiaohongshu/{id}/generate-short-link [post]
// GenerateShortLink 为小红书卡片生成短链
func (c *XiaohongshuCardController) GenerateShortLink(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	// 存在性/越权校验
	if _, err := c.service.GetByID(ctx.Request.Context(), uint(id)); HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	card := &model.XiaohongshuCard{ID: uint(id)}
	if HandleDBError(ctx, c.service.GenerateShortLink(ctx.Request.Context(), card), "生成短链") {
		return
	}

	cardResp, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	response.Success(ctx, cardResp, response.ErrSuccess)
}

// @Summary      渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
// @Tags      xiaohongshu-card
// @Router    /share/xiaohongshu/{id} [get]
// SharePage 渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
func (c *XiaohongshuCardController) SharePage(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.String(http.StatusBadRequest, "无效的卡片ID")
		return
	}
	renderCardChatPage(ctx, c.service.GenerateCardChatPage, uint(id), buildBaseURL(ctx))
}
