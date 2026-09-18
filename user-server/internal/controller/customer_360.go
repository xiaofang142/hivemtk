package controller

import (
	"errors"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Customer360Controller 客户 360° 视图控制器
//
// 通过以下三个 service 访问数据：
//   - UserTagService       用户标签（user_tags 表）
//   - UserProfileService   客户档案（users 表）
//   - TagRuleService       自动标签规则（customer_tags 表）
type Customer360Controller struct {
	customer360Service *service.Customer360Service
	userTagSvc         *service.UserTagService
	userProfileSvc     *service.UserProfileService
	tagRuleSvc         *service.TagRuleService
}

// NewCustomer360Controller 创建客户 360° 视图控制器
func NewCustomer360Controller() *Customer360Controller {
	return &Customer360Controller{
		customer360Service: service.NewCustomer360Service(),
		userTagSvc:         service.NewUserTagService(),
		userProfileSvc:     service.NewUserProfileService(),
		tagRuleSvc:         service.NewTagRuleService(),
	}
}

// GetCustomer360 获取客户 360° 视图
func (c *Customer360Controller) GetCustomer360(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, dto, "获取成功")
}

// GetCustomerList 获取客户列表
func (c *Customer360Controller) GetCustomerList(ctx *gin.Context) {
	page := 1
	pageSize := 20

	if p := ctx.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	if ps := ctx.Query("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 {
			pageSize = n
			if pageSize > 100 {
				pageSize = 100
			}
		}
	}

	filters := make(map[string]string)
	if platform := ctx.Query("platform"); platform != "" {
		filters["platform"] = platform
	}
	if activityLevel := ctx.Query("activity_level"); activityLevel != "" {
		filters["activity_level"] = activityLevel
	}
	if purchasePower := ctx.Query("purchase_power"); purchasePower != "" {
		filters["purchase_power"] = purchasePower
	}

	result, total, err := c.customer360Service.GetCustomerList(ctx.Request.Context(), page, pageSize, filters)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetCustomerBasicInfo 获取客户基本信息（快速接口）
func (c *Customer360Controller) GetCustomerBasicInfo(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"basic_info":   dto.BasicInfo,
		"user_profile": dto.UserProfile,
	}, "获取成功")
}

// GetCustomerStats 获取客户统计信息
func (c *Customer360Controller) GetCustomerStats(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"session_stats":     dto.SessionStats,
		"interaction_stats": dto.InteractionStats,
	}, "获取成功")
}

// GetCustomerSessions 获取客户会话历史
func (c *Customer360Controller) GetCustomerSessions(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"session_history": dto.SessionHistory,
		"total":           len(dto.SessionHistory),
	}, "获取成功")
}

// GetCustomerEvents 获取客户行为事件流水（前端 GET /api/customer-360/events?user_id=&limit=）
func (c *Customer360Controller) GetCustomerEvents(ctx *gin.Context) {
	customerID := ctx.Query("user_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}
	limit := 50
	if v, err := strconv.Atoi(ctx.Query("limit")); err == nil && v > 0 {
		limit = v
	}

	events, err := c.customer360Service.GetCustomerEvents(ctx.Request.Context(), customerID, limit)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "客户不存在")
		return
	}

	response.Success(ctx, gin.H{
		"events": events,
		"total":  len(events),
	}, "获取成功")
}

// GetCustomerOrders 获取客户订单（前端 GET /api/customer-360/orders?user_id=&limit=）
func (c *Customer360Controller) GetCustomerOrders(ctx *gin.Context) {
	customerID := ctx.Query("user_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}
	limit := 20
	if v, err := strconv.Atoi(ctx.Query("limit")); err == nil && v > 0 {
		limit = v
	}

	items, err := c.customer360Service.GetCustomerOrders(ctx.Request.Context(), customerID, limit)
	if err != nil {
		if errors.Is(err, service.ErrCustomerNotFound) {
			response.Error(ctx, http.StatusNotFound, "客户不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"orders": items,
		"total":  len(items),
	}, "获取成功")
}

// GetCustomerMessages 获取客户消息记录
func (c *Customer360Controller) GetCustomerMessages(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"message_history": dto.MessageHistory,
		"total":           len(dto.MessageHistory),
	}, "获取成功")
}

// UpdateCustomerTags 更新客户标签
func (c *Customer360Controller) UpdateCustomerTags(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	finalTags, err := c.userTagSvc.ReplaceUserTags(ctx.Request.Context(), userID, req.Tags)
	if err != nil {
		response.ErrorFromDB(ctx, err, "保存标签失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"user_id": userID,
		"tags":    finalTags,
	}, "更新成功")
}

// GetCustomerTags 获取客户标签
func (c *Customer360Controller) GetCustomerTags(ctx *gin.Context) {
	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 user_id 参数")
		return
	}

	tags, err := c.userTagSvc.GetUserTags(ctx.Request.Context(), userID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取标签失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"user_id": userID,
		"tags":    tags,
	}, "获取成功")
}

// GetCustomer360ByID 通过路径参数 :id 获取客户 360° 视图（兼容前端 /api/customer/360/:id）
func (c *Customer360Controller) GetCustomer360ByID(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	dto, err := c.customer360Service.GetCustomer360ByCustomerID(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, dto, "获取成功")
}

// AddCustomerTag 添加客户标签（兼容前端 POST /api/customer/:id/tags）
func (c *Customer360Controller) AddCustomerTag(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	var req struct {
		Tag string `json:"tag" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	tags, err := c.userTagSvc.AddUserTag(ctx.Request.Context(), userID, req.Tag)
	if err != nil {
		response.ErrorFromDB(ctx, err, "添加标签失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"user_id": userID,
		"tags":    tags,
	}, "添加成功")
}

// RemoveCustomerTag 移除客户标签（兼容前端 DELETE /api/customer/:id/tags/:tag）
func (c *Customer360Controller) RemoveCustomerTag(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	tagName := ctx.Param("tag")
	if tagName == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少标签名称")
		return
	}

	tags, err := c.userTagSvc.RemoveUserTag(ctx.Request.Context(), userID, tagName)
	if err != nil {
		response.ErrorFromDB(ctx, err, "移除标签失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"user_id": userID,
		"tags":    tags,
	}, "移除成功")
}

// GetCustomerDetail 获取客户详情（兼容前端 GET /api/customer/:id）
func (c *Customer360Controller) GetCustomerDetail(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	dto, err := c.customer360Service.GetCustomer360ByCustomerID(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, dto, "获取成功")
}

// UpdateCustomer 更新客户信息（兼容前端 PUT /api/customer/:id）
func (c *Customer360Controller) UpdateCustomer(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	var raw map[string]any
	if err := ctx.ShouldBindJSON(&raw); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	input := service.UserUpdateInput{}
	if v, ok := raw["real_name"].(string); ok && v != "" {
		input.RealName = &v
	}
	if v, ok := raw["email"].(string); ok {
		input.Email = &v
	}
	if v, ok := raw["phone"].(string); ok {
		input.Phone = &v
	}
	if v, ok := raw["status"].(float64); ok {
		status := int(v)
		input.Status = &status
	}

	view, err := c.userProfileSvc.UpdateUserByID(ctx.Request.Context(), userID, input)
	if err != nil {
		if err.Error() == "客户不存在" {
			response.Error(ctx, http.StatusNotFound, err.Error())
		} else if err.Error() == "没有可更新的字段" {
			response.Error(ctx, http.StatusBadRequest, err.Error())
		} else {
			response.ErrorFromDB(ctx, err, "更新客户失败："+err.Error())
		}
		return
	}

	dto, dtoErr := c.customer360Service.GetCustomer360ByCustomerID(ctx.Request.Context(), userID)
	if dtoErr != nil {
		response.Success(ctx, gin.H{
			"id":          view.ID,
			"username":    view.Username,
			"real_name":   view.RealName,
			"email":       view.Email,
			"phone":       view.Phone,
			"status":      view.Status,
			"update_time": view.UpdateTime,
		}, "更新成功")
		return
	}

	response.Success(ctx, dto, "更新成功")
}

// GetCustomerBehaviors 获取客户行为记录（兼容前端 GET /api/customer/:id/behaviors）
func (c *Customer360Controller) GetCustomerBehaviors(ctx *gin.Context) {
	userID := ctx.Param("id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	dto, err := c.customer360Service.GetCustomer360(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"session_history":   dto.SessionHistory,
		"interaction_stats": dto.InteractionStats,
		"total":             len(dto.SessionHistory),
	}, "获取成功")
}

// GetCustomerCommunications 获取客户沟通记录（兼容前端 GET /api/customer/:id/communications）
func (c *Customer360Controller) GetCustomerCommunications(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户ID")
		return
	}

	dto, err := c.customer360Service.GetCustomer360ByCustomerID(ctx.Request.Context(), customerID)
	if err != nil {
		if errors.Is(err, service.ErrCustomerNotFound) {
			response.Error(ctx, http.StatusNotFound, "客户不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"message_history": dto.MessageHistory,
		"total":           len(dto.MessageHistory),
	}, "获取成功")
}

// ListTagRules 获取自动标签规则列表
// GET /api/customer-360/tag-rules
func (c *Customer360Controller) ListTagRules(ctx *gin.Context) {
	rules, err := c.tagRuleSvc.ListTagRules(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取标签规则失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"items": rules,
		"total": len(rules),
	}, "获取成功")
}

type saveTagRuleRequest struct {
	ID       string         `json:"id"`
	Name     string         `json:"name" binding:"required"`
	Category string         `json:"category"`
	Rule     map[string]any `json:"rule"`
	Active   *bool          `json:"active"`
}

// SaveTagRule 创建或更新自动标签规则
// POST /api/customer-360/tag-rules
func (c *Customer360Controller) SaveTagRule(ctx *gin.Context) {
	var req saveTagRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	result, err := c.tagRuleSvc.SaveTagRule(ctx.Request.Context(), service.SaveTagRuleInput{
		ID:       req.ID,
		Name:     req.Name,
		Category: req.Category,
		Rule:     req.Rule,
		Active:   req.Active,
	})
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "规则格式错误: "+err.Error())
		return
	}
	response.Success(ctx, result, "保存成功")
}

// UpdateTagRule 更新指定自动标签规则
// PUT /api/customer-360/tag-rules/:id
func (c *Customer360Controller) UpdateTagRule(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少规则ID")
		return
	}
	var req saveTagRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	result, err := c.tagRuleSvc.UpdateTagRule(ctx.Request.Context(), id, service.SaveTagRuleInput{
		ID:       id,
		Name:     req.Name,
		Category: req.Category,
		Rule:     req.Rule,
		Active:   req.Active,
	})
	if err != nil {
		if err.Error() == "规则不存在" {
			response.Error(ctx, http.StatusNotFound, err.Error())
		} else {
			response.Error(ctx, http.StatusBadRequest, "规则格式错误: "+err.Error())
		}
		return
	}
	response.Success(ctx, result, "更新成功")
}

// DeleteTagRule 删除自动标签规则
// DELETE /api/customer-360/tag-rules/:id
func (c *Customer360Controller) DeleteTagRule(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少规则ID")
		return
	}
	if err := c.tagRuleSvc.DeleteTagRule(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, "删除失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

// GetTagStats 获取标签统计
// GET /api/customer-360/tag-stats
func (c *Customer360Controller) GetTagStats(ctx *gin.Context) {
	stats, err := c.tagRuleSvc.GetTagStats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取标签失败: "+err.Error())
		return
	}
	response.Success(ctx, stats, "获取成功")
}
