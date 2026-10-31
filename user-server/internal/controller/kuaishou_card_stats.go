package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"
	"strconv"

	"github.com/gin-gonic/gin"
)

// KuaishouCardStatsController 快手卡片统计控制器
type KuaishouCardStatsController struct {
	statsService *cardsvc.KuaishouCardStatsService
}

// NewKuaishouCardStatsController 创建快手卡片统计控制器实例
func NewKuaishouCardStatsController(statsService *cardsvc.KuaishouCardStatsService) *KuaishouCardStatsController {
	return &KuaishouCardStatsController{
		statsService: statsService,
	}
}

// @Summary 获取单个快手卡片的统计数据
// @Tags      kuaishou-card-stats
// @Router    /api/kuaishou-card/stats/{id} [get]
// GetCardStats 获取单个快手卡片的统计数据
func (c *KuaishouCardStatsController) GetCardStats(ctx *gin.Context) {
	var req dto.KuaishouCardStatsRequest

	cardIDStr := ctx.Param("id")
	cardID, err := strconv.ParseUint(cardIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, 400, "无效的卡片ID", err.Error())
		return
	}
	req.CardID = uint(cardID)

	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	stats, err := c.statsService.GetCardStats(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "获取快手卡片统计") {
		return
	}

	response.Success(ctx, stats, "获取统计数据成功")
}

// @Summary 获取快手卡片总体统计数据
// @Tags      kuaishou-card-stats
// @Router    /api/kuaishou-card/overall-stats [get]
// GetOverallStats 获取快手卡片总体统计数据
func (c *KuaishouCardStatsController) GetOverallStats(ctx *gin.Context) {
	var req dto.KuaishouCardOverallStatsRequest

	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	stats, err := c.statsService.GetOverallStats(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取总体统计数据失败", err.Error())
		return
	}

	response.Success(ctx, stats, "获取统计数据成功")
}
