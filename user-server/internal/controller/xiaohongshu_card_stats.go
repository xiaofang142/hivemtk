package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/timeutil"
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
		// 默认窗口的「今天/7 天前」按业务日（CST）算：SQL 里 created_at::date 走的
		// 就是 CST，用宿主机时区的 Format 会在 UTC 16:00–23:59 之间整体错一天。
		now := time.Now()
		req.EndDate = timeutil.BusinessDate(now)
		req.StartDate = timeutil.BusinessDate(now.AddDate(0, 0, -7))
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
		now := time.Now()
		req.EndDate = timeutil.BusinessDate(now)
		req.StartDate = timeutil.BusinessDate(now.AddDate(0, 0, -30))
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
