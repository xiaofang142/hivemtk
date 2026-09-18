package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CommunityController 社群管理控制器
type CommunityController struct {
	svc *service.CommunityService
}

// NewCommunityController 创建社群管理控制器实例
func NewCommunityController() *CommunityController {
	return &CommunityController{svc: service.NewCommunityService()}
}

// GetGroups 获取社群列表
func (c *CommunityController) GetGroups(ctx *gin.Context) {
	var req dto.GetCommunityGroupsRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	groups, total, err := c.svc.GetGroups(ctx.Request.Context(), req.Page, req.PageSize, req.Search)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取社群列表失败", err.Error())
		return
	}

	resp := dto.GetCommunityGroupsResponse{
		Total: int(total),
		List:  groups,
	}

	response.Success(ctx, resp, "获取社群列表成功")
}

// GetGroupByID 获取社群详情
func (c *CommunityController) GetGroupByID(ctx *gin.Context) {
	groupID := ctx.Param("id")
	if groupID == "" {
		response.Error(ctx, 400, "参数错误", "社群ID不能为空")
		return
	}

	group, err := c.svc.GetGroupByID(ctx.Request.Context(), groupID)
	if HandleDBError(ctx, err, "获取社群详情") {
		return
	}

	response.Success(ctx, group, "获取社群详情成功")
}

// CreateGroup 创建社群
func (c *CommunityController) CreateGroup(ctx *gin.Context) {
	var req dto.CreateCommunityGroupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	group, err := c.svc.CreateGroup(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建社群失败", err.Error())
		return
	}

	response.Success(ctx, group, "创建社群成功")
}

// UpdateGroup 更新社群
func (c *CommunityController) UpdateGroup(ctx *gin.Context) {
	groupID := ctx.Param("id")
	var req dto.UpdateCommunityGroupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	err := c.svc.UpdateGroup(ctx.Request.Context(), groupID, &req)
	if HandleDBError(ctx, err, "更新社群") {
		return
	}

	response.Success(ctx, nil, "更新社群成功")
}

// DeleteGroup 删除社群
func (c *CommunityController) DeleteGroup(ctx *gin.Context) {
	groupID := ctx.Param("id")
	err := c.svc.DeleteGroup(ctx.Request.Context(), groupID)
	if HandleDBError(ctx, err, "删除社群") {
		return
	}

	response.Success(ctx, nil, "删除社群成功")
}

// GetMembers 获取社群成员列表
func (c *CommunityController) GetMembers(ctx *gin.Context) {
	var req dto.GetCommunityMembersRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	members, total, err := c.svc.GetMembers(ctx.Request.Context(), req.GroupID, req.Page, req.PageSize, req.Search)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取成员列表失败", err.Error())
		return
	}

	resp := dto.GetCommunityMembersResponse{
		Total: int(total),
		List:  members,
	}

	response.Success(ctx, resp, "获取成员列表成功")
}

// GetMemberByID 获取社群成员详情
func (c *CommunityController) GetMemberByID(ctx *gin.Context) {
	memberID := ctx.Param("id")
	if memberID == "" {
		response.Error(ctx, 400, "参数错误", "成员ID不能为空")
		return
	}

	member, err := c.svc.GetMemberByID(ctx.Request.Context(), memberID)
	if HandleDBError(ctx, err, "获取社群成员详情") {
		return
	}

	response.Success(ctx, member, "获取成员详情成功")
}

// AddMember 添加社群成员
func (c *CommunityController) AddMember(ctx *gin.Context) {
	var req dto.AddCommunityMemberRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	member, err := c.svc.AddMember(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "添加成员失败", err.Error())
		return
	}

	response.Success(ctx, member, "添加成员成功")
}

// UpdateMember 更新社群成员
func (c *CommunityController) UpdateMember(ctx *gin.Context) {
	memberID := ctx.Param("id")
	var req dto.UpdateCommunityMemberRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	err := c.svc.UpdateMember(ctx.Request.Context(), memberID, &req)
	if HandleDBError(ctx, err, "更新社群成员") {
		return
	}

	response.Success(ctx, nil, "更新成员成功")
}

// RemoveMember 移除社群成员
func (c *CommunityController) RemoveMember(ctx *gin.Context) {
	memberID := ctx.Param("id")
	err := c.svc.RemoveMember(ctx.Request.Context(), memberID)
	if HandleDBError(ctx, err, "移除社群成员") {
		return
	}

	response.Success(ctx, nil, "移除成员成功")
}

// GetMessages 获取社群消息
func (c *CommunityController) GetMessages(ctx *gin.Context) {
	var req dto.GetCommunityMessagesRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	messages, total, err := c.svc.GetMessages(ctx.Request.Context(), req.GroupID, req.Page, req.PageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取消息列表失败", err.Error())
		return
	}

	resp := dto.GetCommunityMessagesResponse{
		Total: int(total),
		List:  messages,
	}

	response.Success(ctx, resp, "获取消息列表成功")
}

// GetStatistics 获取社群统计
func (c *CommunityController) GetStatistics(ctx *gin.Context) {
	stats, err := c.svc.GetStatistics(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取统计信息失败", err.Error())
		return
	}

	response.Success(ctx, stats, "获取统计信息成功")
}

// ImportData 导入社群数据
func (c *CommunityController) ImportData(ctx *gin.Context) {
	var req struct {
		Groups []dto.CreateCommunityGroupRequest `json:"groups"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}

	if len(req.Groups) == 0 {
		response.Error(ctx, 400, "导入数据不能为空")
		return
	}

	successCount := 0
	for _, groupReq := range req.Groups {
		if _, err := c.svc.CreateGroup(ctx.Request.Context(), &groupReq); err == nil {
			successCount++
		}
	}

	response.Success(ctx, gin.H{
		"total":         len(req.Groups),
		"success_count": successCount,
	}, "导入完成")
}

// ExportData 导出社群数据
func (c *CommunityController) ExportData(ctx *gin.Context) {
	groups, _, err := c.svc.GetGroups(ctx.Request.Context(), 1, 10000, "")
	if err != nil {
		response.ErrorFromDB(ctx, err, "导出数据失败", err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"groups": groups,
		"total":  len(groups),
	}, "导出成功")
}
