package dto

import (
	"time"
)

// CreateEmailJobsRequest 创建任务请求
type CreateEmailJobsRequest struct {
	Subject      string `json:"subject" binding:"required"`
	EmailTotal   int64  `json:"email_total" binding:"required"`
	SendTotal    int64  `json:"send_total"`
	ReadTotal    int64  `json:"read_total"`
	SuccessTotal int64  `json:"success_total"`
	FailTotal    int64  `json:"fail_total"`
}

// UpdateEmailJobsRequest 更新任务请求
type UpdateEmailJobsRequest struct {
	ID           string `json:"id"`
	Subject      string `json:"subject"`
	EmailTotal   int64  `json:"email_total"`
	ReadTotal    int64  `json:"read_total"`
	SendTotal    int64  `json:"send_total"`
	SuccessTotal int64  `json:"success_total"`
	FailTotal    int64  `json:"fail_total"`
}

// EmailJobsResponse 任务响应
type EmailJobsResponse struct {
	ID           string    `json:"id"`
	Subject      string    `json:"subject"`
	EmailTotal   int64     `json:"email_total"`
	SendTotal    int64     `json:"send_total"`
	ReadTotal    int64     `json:"read_total"`
	SuccessTotal int64     `json:"success_total"`
	FailTotal    int64     `json:"fail_total"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type GetEmailJobsListRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"limit"`
}

// GetEmailJobsListResponse 任务列表响应
type GetEmailJobsListResponse struct {
	Total int64                `json:"total"`
	List  []*EmailJobsResponse `json:"list"`
}
