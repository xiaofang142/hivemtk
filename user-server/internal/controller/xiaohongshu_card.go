package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// XiaohongshuCardController 小红书卡片控制器
type XiaohongshuCardController struct {
	service service.XiaohongshuCardService
}

// NewXiaohongshuCardController 创建小红书卡片控制器实例
func NewXiaohongshuCardController(service service.XiaohongshuCardService) *XiaohongshuCardController {
	return &XiaohongshuCardController{
		service: service,
	}
}

// Create 创建小红书卡片
func (c *XiaohongshuCardController) Create(ctx *gin.Context) {
	var req dto.XiaohongshuCardCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Create(ctx, &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrCreateFailed, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrSuccess)
}

// Update 更新小红书卡片
func (c *XiaohongshuCardController) Update(ctx *gin.Context) {
	var req dto.XiaohongshuCardUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Update(ctx, &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrUpdateFailed, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrUpdateSuccess)
}

// Delete 删除小红书卡片
func (c *XiaohongshuCardController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.Delete(ctx, uint(id)), "删除小红书卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrDeleteSuccess)
}

// GetByID 根据ID获取小红书卡片
func (c *XiaohongshuCardController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	card, err := c.service.GetByID(ctx, uint(id))
	if HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	response.Success(ctx, card, response.ErrGetSuccess)
}

// GetList 获取小红书卡片列表
func (c *XiaohongshuCardController) GetList(ctx *gin.Context) {
	var req dto.XiaohongshuCardListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	cards, err := c.service.GetList(ctx, &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrGetFailed, err.Error())
		return
	}

	response.Success(ctx, cards, response.ErrGetSuccess)
}

// GenerateShortLink 为小红书卡片生成短链
func (c *XiaohongshuCardController) GenerateShortLink(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "无效的ID")
		return
	}

	// 存在性/越权校验（结果在 133 行前后重新获取，此处只消费 error）
	if _, err := c.service.GetByID(ctx, uint(id)); HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	card, err := c.service.GetCardModelByID(ctx, uint(id))
	if HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	if HandleDBError(ctx, c.service.GenerateShortLink(ctx, card), "生成短链") {
		return
	}

	cardResp, err := c.service.GetByID(ctx, uint(id))
	if HandleDBError(ctx, err, "获取小红书卡片") {
		return
	}

	response.Success(ctx, cardResp, response.ErrSuccess)
}

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
