package dto

import (
	"time"
)

// CreateEmailListRequest 创建草稿请求
type CreateEmailListRequest struct {
	Subject     string   `json:"subject" binding:"required"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments"`
}

// UpdateEmailListRequest 更新列表请求
type UpdateEmailListRequest struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments"`
}

// EmailListResponse 草稿响应
type EmailListResponse struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Content     string    `json:"content"`
	Attachments []string  `json:"attachments"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	IsSend      int64     `json:"is_send"`
	SendTime    time.Time `json:"send_time"`
	IsRead      int64     `json:"is_read"`
	ReadTime    time.Time `json:"read_time"`
	JobsID      string    `json:"jobs_id"`
	IsSuccess   int64     `json:"is_success"`
}
type GetEmailListRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"limit"`
}

// GetEmailListResponse 草稿列表响应
type GetEmailListResponse struct {
	Total int64                `json:"total"`
	List  []*EmailListResponse `json:"list"`
}
