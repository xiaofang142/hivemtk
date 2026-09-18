package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

// DouyinCardStatsController 抖音卡片统计控制器
type DouyinCardStatsController struct {
	service service.DouyinCardStatsService
}

// NewDouyinCardStatsController 创建抖音卡片统计控制器实例
func NewDouyinCardStatsController(service service.DouyinCardStatsService) *DouyinCardStatsController {
	return &DouyinCardStatsController{
		service: service,
	}
}

// GetCardStats 获取单个卡片的统计数据
func (c *DouyinCardStatsController) GetCardStats(ctx *gin.Context) {
	cardIDStr := ctx.Param("id")
	cardID, err := strconv.ParseUint(cardIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, 400, "无效的卡片ID", err.Error())
		return
	}

	groupBy := ctx.DefaultQuery("groupBy", "day")
	startDate := ctx.Query("startDate")
	endDate := ctx.Query("endDate")

	req := &dto.DouyinCardStatsRequest{
		CardID:    uint(cardID),
		GroupBy:   groupBy,
		StartDate: startDate,
		EndDate:   endDate,
	}

	stats, err := c.service.GetCardStats(ctx.Request.Context(), req)
	if HandleDBError(ctx, err, "获取抖音卡片统计") {
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// GetOverallStats 获取所有卡片的总体统计数据
func (c *DouyinCardStatsController) GetOverallStats(ctx *gin.Context) {
	groupBy := ctx.DefaultQuery("groupBy", "day")
	startDate := ctx.Query("startDate")
	endDate := ctx.Query("endDate")

	req := &dto.DouyinCardOverallStatsRequest{
		GroupBy:   groupBy,
		StartDate: startDate,
		EndDate:   endDate,
	}

	stats, err := c.service.GetOverallStats(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取总体统计数据失败", err.Error())
		return
	}

	response.Success(ctx, stats, "获取成功")
}
