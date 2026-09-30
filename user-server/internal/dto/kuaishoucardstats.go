package dto

import "time"

// KuaishouCardStatsRequest 快手卡片统计数据请求
type KuaishouCardStatsRequest struct {
	CardID    uint      `form:"cardId" json:"cardId" binding:"required"`
	StartDate time.Time `form:"startDate" json:"startDate"`
	EndDate   time.Time `form:"endDate" json:"endDate"`
	GroupBy   string    `form:"groupBy" json:"groupBy"`
}

// KuaishouCardStatsResponse 快手卡片统计数据响应
type KuaishouCardStatsResponse struct {
	CardID     uint        `json:"cardId"`
	CardTitle  string      `json:"cardTitle"`
	TotalViews int         `json:"totalViews"`
	DailyStats []DailyStat `json:"dailyStats"`
	// RecentActivity 该卡的最近浏览记录。原先本结构没有这个字段，
	// 而前端 kuaishouCard/CardStats.vue 的「最近活动」表格正是绑它
	// （:data="cardStats.recentActivity.filter(...)"），字段缺失时只能兜成空数组
	// ⇒ 表格永远空。字段名用单数 recentActivity，与抖音/小红书的单卡响应保持一致。
	RecentActivity []Activity `json:"recentActivity"`
}

// KuaishouCardOverallStatsRequest 快手卡片总体统计数据请求
type KuaishouCardOverallStatsRequest struct {
	StartDate time.Time `form:"startDate" json:"startDate"`
	EndDate   time.Time `form:"endDate" json:"endDate"`
	GroupBy   string    `form:"groupBy" json:"groupBy"`
	Limit     int       `form:"limit" json:"limit"`
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
