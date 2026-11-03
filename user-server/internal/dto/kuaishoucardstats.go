package dto

import "time"

// KuaishouCardStatsRequest 快手卡片统计数据请求
type KuaishouCardStatsRequest struct {
	CardID uint `form:"card_id" json:"cardId" binding:"required"`
	// 日期用字符串承接（前端 el-date-picker 传的是 YYYY-MM-DD）：
	// 用 time.Time 直接绑定会在参数带日期时 400「参数错误」。
	StartDate string `form:"start_date" json:"startDate"`
	EndDate   string `form:"end_date" json:"endDate"`
	GroupBy   string `form:"group_by" json:"groupBy"`
}

// KuaishouCardStatsResponse 快手卡片统计数据响应
type KuaishouCardStatsResponse struct {
	CardID     uint        `json:"cardId"`
	CardTitle  string      `json:"cardTitle"`
	TotalViews int         `json:"totalViews"`
	DailyStats []DailyStat `json:"dailyStats"`
	// RecentActivity 该卡的最近浏览记录（前端「最近活动」表格绑定此字段，
	// 缺失时只能兜成空数组）。字段名用单数 recentActivity，
	// 与抖音/小红书的单卡响应保持一致。
	RecentActivity []Activity `json:"recentActivity"`
}

// KuaishouCardOverallStatsRequest 快手卡片总体统计数据请求
// 查询参数统一用 snake_case（start_date/end_date/group_by）：
// 前端一直按 snake_case 传，之前这里写 camelCase，gin 绑定不到 ⇒
// 日期范围与分组方式被静默丢弃，页面看到的永远是「近一个月 + 按天」。
type KuaishouCardOverallStatsRequest struct {
	// 日期用字符串承接理由同上：YYYY-MM-DD 无法绑进 time.Time，
	// 之前改成 snake_case 后只要前端带日期就 400，比“参数被忽略”更难排查。
	StartDate string `form:"start_date" json:"startDate"`
	EndDate   string `form:"end_date" json:"endDate"`
	GroupBy   string `form:"group_by" json:"groupBy"`
	Limit     int    `form:"limit" json:"limit"`
}

// KuaishouCardOverallStatsResponse 快手卡片总体统计数据响应
type KuaishouCardOverallStatsResponse struct {
	TotalCards       int           `json:"totalCards"`
	ActiveCards      int           `json:"activeCards"`
	TotalViews       int           `json:"totalViews"`
	PopularCards     []PopularCard `json:"popularCards"`
	DailyStats       []DailyStat   `json:"dailyStats"`
	RecentActivities []Activity    `json:"recentActivities"`
}

// KuaishouPopularCard 快手热门卡片
type KuaishouPopularCard struct {
	CardID    uint   `json:"cardId"`
	Title     string `json:"title"`
	Views     int    `json:"views"`
	ShortLink string `json:"shortLink"`
}

// KuaishouActivity 快手活动记录
type KuaishouActivity struct {
	ID        uint      `json:"id"`
	CardID    uint      `json:"cardId"`
	CardTitle string    `json:"cardTitle"`
	Action    string    `json:"action"`
	UserIP    string    `json:"userIp"`
	UserAgent string    `json:"userAgent"`
	ExtraData string    `json:"extraData"`
	CreatedAt time.Time `json:"createdAt"`
}
