package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ShortLinkStatsController 短链统计控制器
type ShortLinkStatsController struct {
	shortLinkService cardsvc.ShortLinkService
}

// NewShortLinkStatsController 创建短链统计控制器实例
func NewShortLinkStatsController(shortLinkService cardsvc.ShortLinkService) *ShortLinkStatsController {
	return &ShortLinkStatsController{
		shortLinkService: shortLinkService,
	}
}

// @Summary 获取短链统计
// @Tags      short-link-stats
// @Router    /api/short-link/{id}/stats [get]
// GetStats 获取短链统计
func (c *ShortLinkStatsController) GetStats(ctx *gin.Context) {
	var req dto.ShortLinkStatsRequest

	idStr := ctx.Param("id")
	if idStr == "" {
		response.Error(ctx, http.StatusBadRequest, "短链ID不能为空")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "短链ID格式错误")
		return
	}
	req.ID = uint(id)

	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: ")
		return
	}

	stats, err := c.shortLinkService.GetStats(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "获取短链统计") {
		return
	}

	response.Success(ctx, stats, "获取短链统计成功")
}

// @Summary 获取所有短链统计
// @Tags      short-link-stats
// @Router    /api/short-link/all-stats [get]
// GetAllStats 获取所有短链统计
func (c *ShortLinkStatsController) GetAllStats(ctx *gin.Context) {
	var req dto.AllShortLinksStatsRequest

	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: ")
		return
	}

	stats, err := c.shortLinkService.GetAllStats(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	response.Success(ctx, stats, "获取所有短链统计成功")
}

// @Summary 分享短链
// @Tags      short-link-stats
// @Router    /api/shortlink/{id}/share [post]
// ShareShortLink 分享短链
func (c *ShortLinkStatsController) ShareShortLink(ctx *gin.Context) {
	var req dto.ShareShortLinkRequest

	idStr := ctx.Param("id")
	if idStr == "" {
		response.Error(ctx, http.StatusBadRequest, "短链ID不能为空")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "短链ID格式错误")
		return
	}
	req.ID = uint(id)

	share, err := c.shortLinkService.ShareShortLink(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, share, "获取短链分享信息成功")
}
