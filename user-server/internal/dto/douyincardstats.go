package dto

// DouyinCardStatsRequest 抖音卡片统计请求
type DouyinCardStatsRequest struct {
	CardID    uint   `json:"cardId" form:"cardId"`
	StartDate string `json:"startDate" form:"startDate"`
	EndDate   string `json:"endDate" form:"endDate"`
	GroupBy   string `json:"groupBy" form:"groupBy"`
}

// DouyinCardStatsResponse 抖音卡片统计响应
type DouyinCardStatsResponse struct {
	CardID         uint        `json:"cardId"`
	Title          string      `json:"title"`
	ViewCount      int         `json:"viewCount"`
	DailyStats     []DailyStat `json:"dailyStats"`
	RecentActivity []Activity  `json:"recentActivity"`
}

// DailyStat 日统计数据
type DailyStat struct {
	Date string `json:"date"`
	View int    `json:"view"`
}

// DouyinCardOverallStatsRequest 抖音卡片总体统计请求
type DouyinCardOverallStatsRequest struct {
	GroupBy   string `json:"groupBy" form:"groupBy"`
	StartDate string `json:"startDate" form:"startDate"`
	EndDate   string `json:"endDate" form:"endDate"`
}

// DouyinCardOverallStatsResponse 抖音卡片总体统计响应
type DouyinCardOverallStatsResponse struct {
	TotalCards     int           `json:"totalCards"`
	ActiveCards    int           `json:"activeCards"`
	TotalViews     int           `json:"totalViews"`
	PopularCards   []PopularCard `json:"popularCards"`
	DailyStats     []DailyStat   `json:"dailyStats"`
	RecentActivity []Activity    `json:"recentActivity"`
}

// PopularCard 热门卡片
type PopularCard struct {
	ID        uint   `json:"id"`
	Title     string `json:"title"`
	ViewCount int    `json:"viewCount"`
	CreatedAt string `json:"createdAt"`
}

// Activity 活动
type Activity struct {
	ID     uint `json:"id"`
	CardID uint `json:"cardId"`
	// CardTitle 来自卡片表 JOIN，前端「最近活动」直接展示
	CardTitle string `json:"cardTitle"`
	Action    string `json:"action"`
	Username  string `json:"username"`
	IPAddress string `json:"ipAddress"`
	CreatedAt string `json:"createdAt"`
}
