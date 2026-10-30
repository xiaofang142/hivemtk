package dto

import "time"

// GetCommunityGroupsRequest 获取社群列表请求
type GetCommunityGroupsRequest struct {
	Page     int    `form:"page,default=1"`
	PageSize int    `form:"page_size,default=20"`
	Search   string `form:"search"`
}

// CommunityGroupResponse 社群响应
type CommunityGroupResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetCommunityGroupsResponse 获取社群列表响应
type GetCommunityGroupsResponse struct {
	Total    int                       `json:"total"`
	List     []*CommunityGroupResponse `json:"list"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

// CreateCommunityGroupRequest 创建社群请求
type CreateCommunityGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// UpdateCommunityGroupRequest 更新社群请求
type UpdateCommunityGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetCommunityMembersRequest 获取社群成员请求
type GetCommunityMembersRequest struct {
	GroupID  string `form:"group_id" binding:"required"`
	Page     int    `form:"page,default=1"`
	PageSize int    `form:"page_size,default=20"`
	Search   string `form:"search"`
}

// CommunityMemberResponse 社群成员响应
type CommunityMemberResponse struct {
	ID       string    `json:"id"`
	GroupID  string    `json:"group_id"`
	Name     string    `json:"name"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	JoinDate time.Time `json:"join_date"`
	LastSeen time.Time `json:"last_seen"`
}

// GetCommunityMembersResponse 获取社群成员响应
type GetCommunityMembersResponse struct {
	Total    int                        `json:"total"`
	List     []*CommunityMemberResponse `json:"list"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

// AddCommunityMemberRequest 添加社群成员请求
type AddCommunityMemberRequest struct {
	GroupID  string `json:"group_id" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Username string `json:"username" binding:"required"`
	Role     string `json:"role"`
}

// UpdateCommunityMemberRequest 更新社群成员请求
type UpdateCommunityMemberRequest struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// GetCommunityMessagesRequest 获取社群消息请求
type GetCommunityMessagesRequest struct {
	GroupID  string `form:"group_id" binding:"required"`
	Page     int    `form:"page,default=1"`
	PageSize int    `form:"page_size,default=20"`
}

// CommunityMessageResponse 社群消息响应
type CommunityMessageResponse struct {
	ID          string    `json:"id"`
	GroupID     string    `json:"group_id"`
	UserID      string    `json:"user_id"`
	UserName    string    `json:"user_name"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
	MessageType string    `json:"message_type"`
}

// GetCommunityMessagesResponse 获取社群消息响应
type GetCommunityMessagesResponse struct {
	Total    int                         `json:"total"`
	List     []*CommunityMessageResponse `json:"list"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
}

// CommunityStatisticsResponse 社群统计响应
type CommunityStatisticsResponse struct {
	TotalGroups     int `json:"total_groups"`
	TotalMembers    int `json:"total_members"`
	TotalMessages   int `json:"total_messages"`
	ActiveGroups    int `json:"active_groups"`
	NewMembersToday int `json:"new_members_today"`
}

// CommunityGroupStatsResponse 单个社群统计。
// 前端社群页的「社群统计」弹窗按 member_count / message_count / active_count 三张卡渲染，
// 字段名必须与页面一致（早前后端只回全站口径的 total_*，弹窗恒为 0）。
type CommunityGroupStatsResponse struct {
	GroupID      string `json:"group_id"`
	Name         string `json:"name"`
	MemberCount  int    `json:"member_count"`
	MessageCount int    `json:"message_count"`
	ActiveCount  int    `json:"active_count"`
}

// CommunityImportError 单条导入失败明细（第几条 + 原因），便于用户改文件后重传。
type CommunityImportError struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Message string `json:"message"`
}
