package dto

import (
	_type "hivemtk-user/internal/pkg/utils/type"
)

type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	RealName string `json:"real_name"`
	Phone    string `json:"phone"`
	Avatar   string `json:"avatar"`
	Role     string `json:"role" binding:"required,oneof=admin user"`
	Status   int    `json:"status"`
}

type UpdateUserRequest struct {
	ID       string `json:"id" binding:"omitempty"`
	Username string `json:"username"`
	Email    string `json:"email"`
	RealName string `json:"real_name"`
	Phone    string `json:"phone"`
	Avatar   string `json:"avatar"`
	Role     string `json:"role" binding:"omitempty,oneof=admin user"`
	Status   *int   `json:"status"`
}

type UpdatePasswordRequest struct {
	ID          string `json:"id" binding:"required"`
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

type GetUserListResponse struct {
	Total int64           `json:"total"`
	Users []*UserResponse `json:"users"`
}

type UserResponse struct {
	ID         string               `json:"id"`
	Username   string               `json:"username"`
	Email      string               `json:"email"`
	RealName   string               `json:"real_name"`
	Phone      string               `json:"phone"`
	Avatar     string               `json:"avatar"`
	Role       string               `json:"role"`
	Status     _type.UserStatusType `json:"status"`
	TgID       int64                `json:"tg_id"`
	CreateTime int64                `json:"create_time"`
	UpdateTime int64                `json:"update_time"`
	AccountID  string               `json:"account_id"`
	FirstName  string               `json:"first_name"`
	LastName   string               `json:"last_name"`
	UserName   string               `json:"user_name"`
}
