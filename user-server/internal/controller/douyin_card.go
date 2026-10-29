package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
)

// DouyinCardController 抖音卡片控制器
type DouyinCardController struct {
	service cardsvc.DouyinCardService
}

// NewDouyinCardController 创建抖音卡片控制器实例
func NewDouyinCardController(service cardsvc.DouyinCardService) *DouyinCardController {
	return &DouyinCardController{
		service: service,
	}
}

// @Summary      创建抖音卡片
// @Tags      douyin-card
// @Router    /api/douyin/create [post]
// Create 创建抖音卡片
func (c *DouyinCardController) Create(ctx *gin.Context) {
	var req dto.DouyinCardCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Create(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "创建抖音卡片") {
		return
	}

	response.Success(ctx, card, response.ErrCreateSuccess)
}

// @Summary      更新抖音卡片
// @Tags      douyin-card
// @Router    /api/douyin-card/{id} [put]
// Update 更新抖音卡片
func (c *DouyinCardController) Update(ctx *gin.Context) {
	var req dto.DouyinCardUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	// 路由注册为 /douyin/update（无 :id），ID 只在 body；有 :id 时才做一致性校验
	// （同 /kuaishou/update 的处理：两条路由形状并存时都要能用）。
	//
	// 为什么不能无条件读 ctx.Param("id")：本路由没有 :id ⇒ Param 返回空串 ⇒
	// ParseUint("") 报 "invalid syntax" ⇒ 恒 400「无效的 ID 格式」，
	// 抖音卡片的编辑页**永远存不进去**。
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
	if HandleDBError(ctx, err, "更新抖音卡片") {
		return
	}

	response.Success(ctx, card, response.ErrUpdateSuccess)
}

// @Summary      删除抖音卡片
// @Tags      douyin-card
// @Router    /api/douyin-card/{id} [delete]
// Delete 删除抖音卡片
func (c *DouyinCardController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	if HandleDBError(ctx, c.service.Delete(ctx.Request.Context(), uint(id)), "删除抖音卡片") {
		return
	}

	response.Success(ctx, nil, response.ErrDeleteSuccess)
}

// @Summary      根据 ID 获取抖音卡片
// @Tags      douyin-card
// @Router    /api/douyin-card/{id} [get]
// GetByID 根据 ID 获取抖音卡片
func (c *DouyinCardController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	card, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, response.ErrResourceNotFound, err.Error())
		return
	}

	response.Success(ctx, card, response.ErrGetSuccess)
}

// @Summary      获取抖音卡片列表
// @Tags      douyin-card
// @Router    /api/douyin-card/list [get]
// GetList 获取抖音卡片列表
func (c *DouyinCardController) GetList(ctx *gin.Context) {
	var req dto.DouyinCardListRequest
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

// @Summary      为抖音卡片生成短链
// @Tags      douyin-card
// @Router    /api/douyin/{id}/generate-short-link [post]
// GenerateShortLink 为抖音卡片生成短链
func (c *DouyinCardController) GenerateShortLink(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	card, err := c.service.GetCardModelByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取抖音卡片") {
		return
	}

	if HandleDBError(ctx, c.service.GenerateShortLink(ctx.Request.Context(), card), "生成短链") {
		return
	}

	cardResp, err := c.service.GetByIDWithRefresh(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取抖音卡片") {
		return
	}

	response.Success(ctx, cardResp, response.ErrSuccess)
}

// @Summary      渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
// @Tags      douyin-card
// @Router    /share/douyin/{id} [get]
// SharePage 渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
func (c *DouyinCardController) SharePage(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.String(http.StatusBadRequest, "无效的卡片ID")
		return
	}
	renderCardChatPage(ctx, c.service.GenerateCardChatPage, uint(id), buildBaseURL(ctx))
}
