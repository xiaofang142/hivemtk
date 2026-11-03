package dto

import (
	"time"
)

// CreateLiveCodeRequest 创建活码请求
type CreateLiveCodeRequest struct {
	Name            string `json:"name" binding:"required"`
	ShortLink       string `json:"short_link" binding:"required"`
	ShortDomainID   int    `json:"short_domain_id" binding:"required"`
	EntryDomainID   int    `json:"entry_domain_id" binding:"required"`
	LandingDomainID int    `json:"landing_domain_id" binding:"required"`
	Status          int    `json:"status"`
	ImageURL        string `json:"image_url"`
	EntryURL        string `json:"entry_url"`
	LandingURL      string `json:"landing_url"`
}

// UpdateLiveCodeRequest 更新活码请求
type UpdateLiveCodeRequest struct {
	Name            string `json:"name"`
	ShortLink       string `json:"short_link"`
	ShortDomainID   int    `json:"short_domain_id"`
	EntryDomainID   int    `json:"entry_domain_id"`
	LandingDomainID int    `json:"landing_domain_id"`
	Status          int    `json:"status"`
	ImageURL        string `json:"image_url"`
	EntryURL        string `json:"entry_url"`
	LandingURL      string `json:"landing_url"`
}

// LiveCodeResponse 活码响应
type LiveCodeResponse struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	ShortLink       string              `json:"short_link"`
	ShortDomainID   int                 `json:"short_domain_id"`
	EntryDomainID   int                 `json:"entry_domain_id"`
	LandingDomainID int                 `json:"landing_domain_id"`
	ShortDomain     *DomainPoolResponse `json:"short_domain"`
	EntryDomain     *DomainPoolResponse `json:"entry_domain"`
	LandingDomain   *DomainPoolResponse `json:"landing_domain"`
	Status          int                 `json:"status"`
	TotalViews      int                 `json:"total_views"`
	TodayViews      int                 `json:"today_views"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	FullShortLink   string              `json:"full_short_link"`
	FullEntryLink   string              `json:"full_entry_link"`
	FullLandingLink string              `json:"full_landing_link"`
	ImageURL        string              `json:"image_url"`
	EntryURL        string              `json:"entry_url"`
	LandingURL      string              `json:"landing_url"`
}

// LiveCodeListResponse 活码列表响应
type LiveCodeListResponse struct {
	List  []*LiveCodeResponse `json:"list"`
	Total int64               `json:"total"`
}

// LiveCodeStatsResponse 活码统计响应
type LiveCodeStatsResponse struct {
	LiveCodeID     string  `json:"live_code_id"`
	TotalViews     int     `json:"total_views"`
	TodayViews     int     `json:"today_views"`
	TotalClicks    int     `json:"total_clicks"`
	TodayClicks    int     `json:"today_clicks"`
	TotalQRShown   int     `json:"total_qr_shown"`
	TotalQRClicks  int     `json:"total_qr_clicks"`
	ActiveQRCount  int     `json:"active_qr_count"`
	TotalQRCount   int     `json:"total_qr_count"`
	ConversionRate float64 `json:"conversion_rate"`
}

// GenerateQRCodeRequest 生成二维码请求
type GenerateQRCodeRequest struct {
	// ExpireDays 允许前端不传：服务层会补默认 30 天，避免必填校验原文直接暴露给用户。
	ExpireDays int `json:"expire_days" binding:"omitempty,min=1,max=3650"`
	Status     int `json:"status"`
}

// LiveCodeQRResponse 活码二维码响应
type LiveCodeQRResponse struct {
	ID         string `json:"id"`
	LiveCodeID string `json:"live_code_id"`
	ImageURL   string `json:"image_url"`
	QRImageURL string `json:"qr_image_url"`
	ExpireDays int    `json:"expire_days"`
	// DailyLimit 每日限制次数：前端「二维码管理」列直接展示，缺字段会渲染成空的「次」
	DailyLimit          int       `json:"daily_limit"`
	ViewCount           int       `json:"view_count"`
	ClickCount          int       `json:"click_count"`
	Status              int       `json:"status"`
	ExpireTime          time.Time `json:"expire_time"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	IsExpired           bool      `json:"is_expired"`
	IsDailyLimitReached bool      `json:"is_daily_limit_reached"`
}

// LiveCodeQRStatsResponse 活码二维码统计响应
type LiveCodeQRStatsResponse struct {
	QRCodeID            string                `json:"qr_code_id"`
	ExpireDays          int                   `json:"expire_days"`
	ViewCount           int                   `json:"view_count"`
	ClickCount          int                   `json:"click_count"`
	ExpireTime          time.Time             `json:"expire_time"`
	Status              int                   `json:"status"`
	IsExpired           bool                  `json:"is_expired"`
	IsDailyLimitReached bool                  `json:"is_daily_limit_reached"`
	AccessStats         []*LiveCodeQRStatItem `json:"access_stats"`
}

// LiveCodeQRStatItem 活码二维码统计项
type LiveCodeQRStatItem struct {
	ID        string    `json:"id"`
	QRCodeID  string    `json:"qr_code_id"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	Date      time.Time `json:"date"`
	CreatedAt time.Time `json:"created_at"`
}

// ShareLiveCodeRequest 分享活码请求
type ShareLiveCodeRequest struct {
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
}

// ShareLiveCodeResponse 分享活码响应
type ShareLiveCodeResponse struct {
	ShortLink   string `json:"short_link"`
	EntryLink   string `json:"entry_link"`
	LandingLink string `json:"landing_link"`
	QRImagePath string `json:"qr_image_path"`
	QRCodeID    string `json:"qr_code_id"`
}

// UpdateLiveCodeQRRequest 更新活码二维码请求
type UpdateLiveCodeQRRequest struct {
	Status     *int `json:"status"`
	ExpireDays *int `json:"expire_days"`
}
