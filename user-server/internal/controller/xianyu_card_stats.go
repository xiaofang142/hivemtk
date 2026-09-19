package controller

import (
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// XianyuCardStatsController 闲鱼卡片统计控制器
//
// 修复：严格遵循五层架构 Controller → Service → Repository → Model，
// 移除原先方法内 new repository.XianyuCardStatsRepository 的越层调用，
// 通过 service.XianyuCardStatsService 的 GetCardStatsRaw /
// GetOverallStatsRaw 方法访问原始统计数据（保留 views/clicks/shares 完整字段）。
type XianyuCardStatsController struct {
	service service.XianyuCardStatsService
}

// NewXianyuCardStatsController 创建闲鱼卡片统计控制器实例
func NewXianyuCardStatsController(service service.XianyuCardStatsService) *XianyuCardStatsController {
	return &XianyuCardStatsController{
		service: service,
	}
}

// GetCardStats 获取卡片统计数据
func (c *XianyuCardStatsController) GetCardStats(ctx *gin.Context) {
	cardIDStr := ctx.Param("id")
	// 默认窗口按业务日（CST）给：日期串最终会和 CST 时区写入的 created_at 比较。
	queryNow := time.Now()
	startDate := ctx.DefaultQuery("start_date", timeutil.BusinessDate(queryNow.AddDate(0, 0, -7)))
	endDate := ctx.DefaultQuery("end_date", timeutil.BusinessDate(queryNow))
	_ = ctx.DefaultQuery("group_by", "day")

	cardID, err := strconv.ParseUint(cardIDStr, 10, 64)
	if err != nil {
		response.Error(ctx, 400, "卡片ID格式错误", err.Error())
		return
	}

	raw, err := c.service.GetCardStatsRaw(ctx, uint(cardID), startDate, endDate)
	if err != nil {
		dates := []string{}
		views := []int{}
		clicks := []int{}
		shares := []int{}
		data := gin.H{
			"stats": gin.H{
				"total_views":  0,
				"total_clicks": 0,
				"total_shares": 0,
				"click_rate":   0,
			},
			"chart": gin.H{
				"dates":  dates,
				"views":  views,
				"clicks": clicks,
				"shares": shares,
			},
			"details": gin.H{
				"list":      []gin.H{},
				"total":     0,
				"page":      1,
				"page_size": 10,
			},
		}
		response.Success(ctx, data, "获取成功")
		return
	}

	totalViews := raw.Views
	totalClicks := raw.Clicks
	totalShares := raw.Shares
	clickRate := 0.0
	if totalViews > 0 {
		clickRate = float64(totalClicks) / float64(totalViews)
	}

	dates := make([]string, len(raw.StatsByDate))
	views := make([]int, len(raw.StatsByDate))
	clicks := make([]int, len(raw.StatsByDate))
	shares := make([]int, len(raw.StatsByDate))
	for i, d := range raw.StatsByDate {
		dates[i] = d.Date
		views[i] = d.Views
		clicks[i] = d.Clicks
		shares[i] = d.Shares
	}

	pageStr := ctx.DefaultQuery("page", "1")
	sizeStr := ctx.DefaultQuery("page_size", "10")
	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(sizeStr)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(raw.StatsByDate) {
		start = len(raw.StatsByDate)
	}
	if end > len(raw.StatsByDate) {
		end = len(raw.StatsByDate)
	}
	details := make([]gin.H, 0, end-start)
	for _, d := range raw.StatsByDate[start:end] {
		cr := 0.0
		if d.Views > 0 {
			cr = float64(d.Clicks) / float64(d.Views)
		}
		details = append(details, gin.H{
			"date":            d.Date,
			"views":           d.Views,
			"clicks":          d.Clicks,
			"shares":          d.Shares,
			"click_rate":      cr,
			"unique_visitors": 0,
		})
	}

	data := gin.H{
		"stats": gin.H{
			"total_views":  totalViews,
			"total_clicks": totalClicks,
			"total_shares": totalShares,
			"click_rate":   clickRate,
		},
		"chart": gin.H{
			"dates":  dates,
			"views":  views,
			"clicks": clicks,
			"shares": shares,
		},
		"details": gin.H{
			"list":      details,
			"total":     len(raw.StatsByDate),
			"page":      page,
			"page_size": pageSize,
		},
	}

	response.Success(ctx, data, "获取成功")
}

// GetOverallStats 获取整体统计数据
func (c *XianyuCardStatsController) GetOverallStats(ctx *gin.Context) {
	queryNow := time.Now()
	startDate := ctx.DefaultQuery("start_date", timeutil.BusinessDate(queryNow.AddDate(0, 0, -7)))
	endDate := ctx.DefaultQuery("end_date", timeutil.BusinessDate(queryNow))
	_ = ctx.DefaultQuery("group_by", "day")

	raw2, err := c.service.GetOverallStatsRaw(ctx, startDate, endDate)
	if err != nil {
		data := gin.H{
			"overall": gin.H{
				"total_cards":  0,
				"active_cards": 0,
				"total_views":  0,
				"total_clicks": 0,
				"total_shares": 0,
			},
			"chart": gin.H{
				"dates":  []string{},
				"views":  []int{},
				"clicks": []int{},
				"shares": []int{},
			},
			"ranking": []gin.H{},
		}
		response.Success(ctx, data, "获取成功")
		return
	}

	overall := gin.H{
		"total_cards":  int(raw2.TotalCards),
		"active_cards": int(raw2.ActiveCards),
		"total_views":  raw2.TotalViewCount,
		"total_clicks": raw2.TotalClickCount,
		"total_shares": raw2.TotalShareCount,
	}

	dates := make([]string, len(raw2.StatsByDate))
	views := make([]int, len(raw2.StatsByDate))
	clicks := make([]int, len(raw2.StatsByDate))
	shares := make([]int, len(raw2.StatsByDate))
	for i, d := range raw2.StatsByDate {
		dates[i] = d.Date
		views[i] = d.Views
		clicks[i] = d.Clicks
		shares[i] = d.Shares
	}

	ranking := make([]gin.H, len(raw2.TopCards))
	for i, p := range raw2.TopCards {
		ranking[i] = gin.H{
			"id":          p.ID,
			"title":       p.Title,
			"view_count":  p.ViewCount,
			"click_count": 0,
			"click_rate":  0,
		}
	}

	data := gin.H{
		"overall": overall,
		"chart": gin.H{
			"dates":  dates,
			"views":  views,
			"clicks": clicks,
			"shares": shares,
		},
		"ranking": ranking,
	}

	response.Success(ctx, data, "获取成功")
}
