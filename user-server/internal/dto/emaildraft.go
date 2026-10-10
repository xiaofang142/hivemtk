package dto

import (
	"time"
)

// CreateEmailDraftRequest 创建草稿请求
type CreateEmailDraftRequest struct {
	Subject     string   `json:"subject" binding:"required"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments"`
}

// UpdateEmailDraftRequest 更新草稿请求
type UpdateEmailDraftRequest struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments"`
}

// EmailDraftResponse 草稿响应
type EmailDraftResponse struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Content     string    `json:"content"`
	Attachments []string  `json:"attachments"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetEmailDraftListResponse 草稿列表响应
type GetEmailDraftListResponse struct {
	Total int64                 `json:"total"`
	List  []*EmailDraftResponse `json:"list"`
}
