package controller

import (
	"errors"
	"strconv"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// communityPaging 归一化分页回显（与仓储 normalizeCommunityPaging 同规则，保证回显=实际）。
func communityPaging(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(strings.TrimSpace(ctx.Query("page")))
	size, _ := strconv.Atoi(strings.TrimSpace(ctx.Query("page_size")))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return page, size
}

// communitySearch 取搜索词：主字段 search，兼容前端分组页历史字段 name / keyword。
func communitySearch(ctx *gin.Context, primary string) string {
	if v := strings.TrimSpace(primary); v != "" {
		return v
	}
	for _, key := range []string{"name", "keyword"} {
		if v := strings.TrimSpace(ctx.Query(key)); v != "" {
			return v
		}
	}
	return ""
}

// replyCommunityError 社群相关错误的统一出口：
// 社群/成员不存在 → 404（文案带业务 id）；其余交 HandleDBError。
func replyCommunityError(ctx *gin.Context, err error, operation string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, service.ErrCommunityGroupNotFound), errors.Is(err, service.ErrCommunityMemberNotFound):
		response.NotFound(ctx, err.Error())
		return true
	case errhttp.IsNotFoundError(err):
		response.NotFound(ctx, err.Error())
		return true
	}
	return HandleDBError(ctx, err, operation)
}

// CommunityController 社群管理控制器
type CommunityController struct {
	svc *service.CommunityService
}

// NewCommunityController 创建社群管理控制器实例（服务由装配点注入）
func NewCommunityController(svc *service.CommunityService) *CommunityController {
	return &CommunityController{svc: svc}
}

// @Summary      获取社群列表成功
// @Tags      community
// @Router    /api/community/groups [get]
// GetGroups 获取社群列表
func (c *CommunityController) GetGroups(ctx *gin.Context) {
	var req dto.GetCommunityGroupsRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：page / page_size 必须是整数")
		return
	}

	groups, total, err := c.svc.GetGroups(ctx.Request.Context(), req.Page, req.PageSize, communitySearch(ctx, req.Search))
	if err != nil {
		replyCommunityError(ctx, err, "获取社群列表")
		return
	}

	page, pageSize := communityPaging(ctx)
	resp := dto.GetCommunityGroupsResponse{
		Total:    int(total),
		List:     groups,
		Page:     page,
		PageSize: pageSize,
	}

	response.Success(ctx, resp, "获取社群列表成功")
}

// @Summary      获取社群详情成功
// @Tags      community
// @Router    /api/community/groups/{id} [get]
// GetGroupByID 获取社群详情
func (c *CommunityController) GetGroupByID(ctx *gin.Context) {
	groupID := ctx.Param("id")
	if groupID == "" {
		response.Error(ctx, 400, "参数错误", "社群ID不能为空")
		return
	}

	group, err := c.svc.GetGroupByID(ctx.Request.Context(), groupID)
	if replyCommunityError(ctx, err, "获取社群详情") {
		return
	}

	response.Success(ctx, group, "获取社群详情成功")
}

// @Summary      创建社群成功
// @Tags      community
// @Router    /api/community/groups [post]
// CreateGroup 创建社群
func (c *CommunityController) CreateGroup(ctx *gin.Context) {
	var req dto.CreateCommunityGroupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：name（分组名称）不能为空")
		return
	}

	group, err := c.svc.CreateGroup(ctx.Request.Context(), &req)
	if replyCommunityError(ctx, err, "创建社群") {
		return
	}

	response.Success(ctx, group, "创建社群成功")
}

// @Summary      更新社群成功
// @Tags      community
// @Router    /api/community/groups/{id} [put]
// UpdateGroup 更新社群
func (c *CommunityController) UpdateGroup(ctx *gin.Context) {
	groupID := ctx.Param("id")
	var req dto.UpdateCommunityGroupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：name / description 必须是字符串")
		return
	}

	err := c.svc.UpdateGroup(ctx.Request.Context(), groupID, &req)
	if replyCommunityError(ctx, err, "更新社群") {
		return
	}

	response.Success(ctx, nil, "更新社群成功")
}

// @Summary      删除社群成功
// @Tags      community
// @Router    /api/community/groups/{id} [delete]
// DeleteGroup 删除社群
func (c *CommunityController) DeleteGroup(ctx *gin.Context) {
	groupID := ctx.Param("id")
	err := c.svc.DeleteGroup(ctx.Request.Context(), groupID)
	if replyCommunityError(ctx, err, "删除社群") {
		return
	}

	response.Success(ctx, nil, "删除社群成功")
}

// @Summary      获取成员列表成功
// @Tags      community
// @Router    /api/community/members [get]
// GetMembers 获取社群成员列表
func (c *CommunityController) GetMembers(ctx *gin.Context) {
	var req dto.GetCommunityMembersRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：group_id 必填，page / page_size 必须是整数")
		return
	}

	members, total, err := c.svc.GetMembers(ctx.Request.Context(), req.GroupID, req.Page, req.PageSize, req.Search)
	if err != nil {
		replyCommunityError(ctx, err, "获取成员列表")
		return
	}

	page, pageSize := communityPaging(ctx)
	resp := dto.GetCommunityMembersResponse{
		Total:    int(total),
		List:     members,
		Page:     page,
		PageSize: pageSize,
	}

	response.Success(ctx, resp, "获取成员列表成功")
}

// @Summary      获取成员详情成功
// @Tags      community
// @Router    /api/community/members/{id} [get]
// GetMemberByID 获取社群成员详情
func (c *CommunityController) GetMemberByID(ctx *gin.Context) {
	memberID := ctx.Param("id")
	if memberID == "" {
		response.Error(ctx, 400, "参数错误", "成员ID不能为空")
		return
	}

	member, err := c.svc.GetMemberByID(ctx.Request.Context(), memberID)
	if replyCommunityError(ctx, err, "获取社群成员详情") {
		return
	}

	response.Success(ctx, member, "获取成员详情成功")
}

// @Summary      添加成员成功
// @Tags      community
// @Router    /api/community/members [post]
// AddMember 添加社群成员
func (c *CommunityController) AddMember(ctx *gin.Context) {
	var req dto.AddCommunityMemberRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：group_id、name、username 均为必填")
		return
	}

	member, err := c.svc.AddMember(ctx.Request.Context(), &req)
	if replyCommunityError(ctx, err, "添加成员") {
		return
	}

	response.Success(ctx, member, "添加成员成功")
}

// @Summary      更新成员成功
// @Tags      community
// @Router    /api/community/members/{id} [put]
// UpdateMember 更新社群成员
func (c *CommunityController) UpdateMember(ctx *gin.Context) {
	memberID := ctx.Param("id")
	var req dto.UpdateCommunityMemberRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：name / role / status 必须是字符串")
		return
	}

	err := c.svc.UpdateMember(ctx.Request.Context(), memberID, &req)
	if replyCommunityError(ctx, err, "更新社群成员") {
		return
	}

	response.Success(ctx, nil, "更新成员成功")
}

// @Summary      移除成员成功
// @Tags      community
// @Router    /api/community/members/{id} [delete]
// RemoveMember 移除社群成员
func (c *CommunityController) RemoveMember(ctx *gin.Context) {
	memberID := ctx.Param("id")
	err := c.svc.RemoveMember(ctx.Request.Context(), memberID)
	if replyCommunityError(ctx, err, "移除社群成员") {
		return
	}

	response.Success(ctx, nil, "移除成员成功")
}

// @Summary      获取消息列表成功
// @Tags      community
// @Router    /api/community/messages [get]
// GetMessages 获取社群消息
func (c *CommunityController) GetMessages(ctx *gin.Context) {
	var req dto.GetCommunityMessagesRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：group_id 必填，page / page_size 必须是整数")
		return
	}

	messages, total, err := c.svc.GetMessages(ctx.Request.Context(), req.GroupID, req.Page, req.PageSize)
	if err != nil {
		replyCommunityError(ctx, err, "获取消息列表")
		return
	}

	page, pageSize := communityPaging(ctx)
	resp := dto.GetCommunityMessagesResponse{
		Total:    int(total),
		List:     messages,
		Page:     page,
		PageSize: pageSize,
	}

	response.Success(ctx, resp, "获取消息列表成功")
}

// @Summary      获取统计信息成功
// @Tags      community
// @Router    /api/community/stats [get]
// GetStatistics 获取社群统计
func (c *CommunityController) GetStatistics(ctx *gin.Context) {
	// 社群页的统计弹窗按单群取数（?group_id=），只传空时才回全站口径。
	if groupID := strings.TrimSpace(ctx.Query("group_id")); groupID != "" {
		stats, err := c.svc.GetGroupStatistics(ctx.Request.Context(), groupID)
		if replyCommunityError(ctx, err, "获取社群统计") {
			return
		}
		response.Success(ctx, stats, "获取统计信息成功")
		return
	}

	stats, err := c.svc.GetStatistics(ctx.Request.Context())
	if replyCommunityError(ctx, err, "获取统计信息") {
		return
	}

	response.Success(ctx, stats, "获取统计信息成功")
}

// @Summary      导入完成
// @Tags      community
// @Router    /api/community/import [post]
// ImportData 导入社群数据
func (c *CommunityController) ImportData(ctx *gin.Context) {
	var req struct {
		Groups []dto.CreateCommunityGroupRequest `json:"groups"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误：请求体必须是 {\"groups\": [{\"name\": \"...\"}]}")
		return
	}

	if len(req.Groups) == 0 {
		response.Error(ctx, 400, "导入数据不能为空")
		return
	}

	// 逐条导入并留下失败明细：早前静默吞错，用户只能看到「成功 3 / 共 5」，
	// 不知道哪两条为什么没进来，也没有任何日志线索。
	successCount := 0
	failures := make([]dto.CommunityImportError, 0)
	for idx, groupReq := range req.Groups {
		if _, err := c.svc.CreateGroup(ctx.Request.Context(), &groupReq); err != nil {
			failures = append(failures, dto.CommunityImportError{
				Index:   idx + 1,
				Name:    groupReq.Name,
				Message: communityImportMessage(err),
			})
			continue
		}
		successCount++
	}

	response.Success(ctx, gin.H{
		"total":         len(req.Groups),
		"success_count": successCount,
		"failed_count":  len(failures),
		"errors":        failures,
	}, "导入完成")
}

// @Summary      导出成功
// @Tags      community
// @Router    /api/community/export [post]
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

// communityImportMessage 把导入单条失败的原因洗成可读文案（去掉哨兵前缀）。
func communityImportMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, suffix := range []string{": " + service.ErrCommunityGroupNotFound.Error()} {
		msg = strings.TrimSuffix(msg, suffix)
	}
	return msg
}
