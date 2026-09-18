package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type XiaohongshuCardStatsController struct {
	statsService service.XiaohongshuCardStatsService
}

func NewXiaohongshuCardStatsController(statsService service.XiaohongshuCardStatsService) *XiaohongshuCardStatsController {
	return &XiaohongshuCardStatsController{
		statsService: statsService,
	}
}

// GetCardStats 获取单个小红书卡片的统计数据
func (c *XiaohongshuCardStatsController) GetCardStats(ctx *gin.Context) {
	cardIDStr := ctx.Param("id")
	cardID, err := strconv.ParseUint(cardIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, 400, "无效的卡片ID", err.Error())
		return
	}

	req := &dto.XiaohongshuCardStatsRequest{
		CardID:    uint(cardID),
		StartDate: ctx.Query("start_date"),
		EndDate:   ctx.Query("end_date"),
		GroupBy:   ctx.Query("group_by"),
	}

	if req.StartDate == "" || req.EndDate == "" {
		req.EndDate = time.Now().Format("2006-01-02")
		req.StartDate = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}

	if req.GroupBy == "" {
		req.GroupBy = "day"
	}

	stats, err := c.statsService.GetCardStats(ctx.Request.Context(), req)
	if HandleDBError(ctx, err, "获取小红书卡片统计") {
		return
	}

	response.Success(ctx, stats, "获取统计数据成功")
}

// GetOverallStats 获取小红书卡片的总体统计数据
func (c *XiaohongshuCardStatsController) GetOverallStats(ctx *gin.Context) {
	req := &dto.XiaohongshuCardOverallStatsRequest{
		GroupBy:   ctx.Query("group_by"),
		StartDate: ctx.Query("start_date"),
		EndDate:   ctx.Query("end_date"),
	}

	if req.StartDate == "" || req.EndDate == "" {
		req.EndDate = time.Now().Format("2006-01-02")
		req.StartDate = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}

	if req.GroupBy == "" {
		req.GroupBy = "day"
	}

	stats, err := c.statsService.GetOverallStats(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取统计数据失败", err.Error())
		return
	}

	response.Success(ctx, stats, "获取统计数据成功")
}
