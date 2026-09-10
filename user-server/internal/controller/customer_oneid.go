package controller

import (
	"context"
	"net/http"
	"strconv"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CustomerOneIDController 客户 360 OneID 控制器
// 负责多渠道身份合并、冲突解决与身份映射查询
//
// 修复：严格遵循五层架构 Controller → Service → Repository → Model，
// 移除原先对 repository.CustomerRepository 的直接依赖，改为通过
// service.CustomerQueryService 访问数据。
type CustomerOneIDController struct {
	identitySvc  *service.CustomerIdentityService
	custQuerySvc *service.CustomerQueryService
}

// NewCustomerOneIDController 创建 OneID 控制器
func NewCustomerOneIDController() *CustomerOneIDController {
	return &CustomerOneIDController{
		identitySvc:  service.NewCustomerIdentityService(),
		custQuerySvc: service.NewCustomerQueryService(),
	}
}

type mergeRequest struct {
	PrimaryID   string `json:"primary_id" binding:"required"`
	SecondaryID string `json:"secondary_id" binding:"required"`
}

// MergeIdentity godoc
// @Summary      合并 OneID 客户身份
// @Description  把两个 OneID 合并为一个，保留主 ID 全部行为轨迹
// @Tags         OneID
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  mergeRequest  true  "合并请求"
// @Success      200   {object}  response.Response  "合并成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/oneid/merge [post]
func (c *CustomerOneIDController) MergeIdentity(ctx *gin.Context) {
	var req mergeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.PrimaryID == req.SecondaryID {
		response.Error(ctx, http.StatusBadRequest, "不能合并同一客户")
		return
	}
	custSvc := service.NewCustomerService()
	svcCtx := service.WithOperator(context.Background(), service.Operator{
		UserID:   getUserIDFromContext(ctx),
		Username: ctx.GetString("username"),
	})
	if err := custSvc.MergeCustomers(svcCtx, req.PrimaryID, req.SecondaryID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	primary, _ := c.custQuerySvc.GetCustomerByID(ctx.Request.Context(), req.PrimaryID)
	response.Success(ctx, gin.H{"primary": primary, "merged_id": req.SecondaryID}, "合并成功")
}

// ListOneID godoc
// @Summary      OneID 列表
// @Description  分页查询客户 OneID 及其身份
// @Tags         OneID
// @Produce      json
// @Security     BearerAuth
// @Param        page      query  int     false  "页码"   default(1)
// @Param        page_size query  int     false  "每页"   default(20)
// @Param        keyword   query  string  false  "关键词"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/oneid [get]
func (c *CustomerOneIDController) ListOneID(ctx *gin.Context) {
	page := parsePage(ctx.Query("page"))
	pageSize := parsePageSize(ctx.Query("page_size"), 20)
	keyword := ctx.Query("keyword")
	list, total := c.custQuerySvc.ListCustomers(ctx.Request.Context(), page, pageSize, keyword)
	response.Success(ctx, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// OneIDStats godoc
// @Summary      OneID 统计
// @Description  返回 OneID 总数、冲突数、合并率等
// @Tags         OneID
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/oneid/stats [get]
func (c *CustomerOneIDController) OneIDStats(ctx *gin.Context) {
	stats := c.custQuerySvc.OneIDStats(ctx.Request.Context())
	response.Success(ctx, stats, "获取成功")
}

// ListConflicts 列出潜在的身份冲突（同一手机号/邮箱/openid 关联到不同客户）
func (c *CustomerOneIDController) ListConflicts(ctx *gin.Context) {
	page := parsePage(ctx.Query("page"))
	pageSize := parsePageSize(ctx.Query("page_size"), 20)
	conflicts, total := c.custQuerySvc.ListConflicts(ctx.Request.Context(), page, pageSize)
	response.Success(ctx, gin.H{
		"list":      conflicts,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// ResolveConflictRequest 解决冲突请求
type ResolveConflictRequest struct {
	PrimaryID   string `json:"primary_id" binding:"required"`
	SecondaryID string `json:"secondary_id" binding:"required"`
	Action      string `json:"action"`
}

// ResolveConflict 解决身份冲突
func (c *CustomerOneIDController) ResolveConflict(ctx *gin.Context) {
	id := ctx.Param("id")
	var req ResolveConflictRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.Action == "" {
		req.Action = "merge"
	}
	if req.Action == "ignore" {
		response.Success(ctx, gin.H{"id": id, "action": "ignored"}, "已忽略冲突")
		return
	}
	if req.PrimaryID == req.SecondaryID {
		response.Error(ctx, http.StatusBadRequest, "不能合并同一客户")
		return
	}
	custSvc := service.NewCustomerService()
	svcCtx := service.WithOperator(context.Background(), service.Operator{
		UserID:   getUserIDFromContext(ctx),
		Username: ctx.GetString("username"),
	})
	if err := custSvc.MergeCustomers(svcCtx, req.PrimaryID, req.SecondaryID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	primary, _ := c.custQuerySvc.GetCustomerByID(ctx.Request.Context(), req.PrimaryID)
	response.Success(ctx, gin.H{"id": id, "primary": primary, "merged_id": req.SecondaryID}, "冲突已解决")
}

// GetIdentityMappings 获取指定客户的所有身份映射
func (c *CustomerOneIDController) GetIdentityMappings(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 customer_id")
		return
	}
	customer, err := c.custQuerySvc.GetCustomerByID(ctx.Request.Context(), customerID)
	if err != nil || customer == nil || customer.ID == "" {
		response.NotFound(ctx, "客户不存在")
		return
	}
	identities := []gin.H{}
	if customer.Phone != "" {
		identities = append(identities, gin.H{"type": "phone", "value": customer.Phone, "source": "手机号"})
	}
	if customer.Email != "" {
		identities = append(identities, gin.H{"type": "email", "value": customer.Email, "source": "邮箱"})
	}
	if customer.WechatOpenID != "" {
		identities = append(identities, gin.H{"type": "wechat_open_id", "value": customer.WechatOpenID, "source": "微信"})
	}
	if customer.DouyinOpenID != "" {
		identities = append(identities, gin.H{"type": "douyin_open_id", "value": customer.DouyinOpenID, "source": "抖音"})
	}
	if customer.XiaohongshuID != "" {
		identities = append(identities, gin.H{"type": "xiaohongshu_id", "value": customer.XiaohongshuID, "source": "小红书"})
	}
	response.Success(ctx, gin.H{
		"customer":   customer,
		"identities": identities,
	}, "获取成功")
}

// LinkIdentity 链接新身份到指定客户
func (c *CustomerOneIDController) LinkIdentity(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 customer_id")
		return
	}
	var identifiers identity.Identifiers
	if err := ctx.ShouldBindJSON(&identifiers); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if err := c.identitySvc.LinkIdentity(context.Background(), customerID, identifiers.Phone, identifiers.Email, identifiers.WechatOpenID, identifiers.DouyinOpenID, identifiers.XiaohongshuID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"customer_id": customerID, "identifiers": identifiers}, "链接成功")
}

// ResolveIdentity 解析身份标识（识别或创建）
// 接收一个或多个渠道标识，返回所有匹配到的客户及归一化结果
func (c *CustomerOneIDController) ResolveIdentity(ctx *gin.Context) {
	var identifiers identity.Identifiers
	if err := ctx.ShouldBindJSON(&identifiers); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	customers, err := c.identitySvc.ResolveIdentity(context.Background(), identifiers)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	normalized := identity.Identifiers{
		Phone:         identity.NormalizePhone(identifiers.Phone),
		Email:         identity.NormalizeEmail(identifiers.Email),
		WechatOpenID:  identity.NormalizeOpenID(identifiers.WechatOpenID),
		DouyinOpenID:  identity.NormalizeOpenID(identifiers.DouyinOpenID),
		XiaohongshuID: identifiers.XiaohongshuID,
	}
	if len(customers) == 0 {
		response.Success(ctx, gin.H{
			"customers":      []any{},
			"matched":        false,
			"identifiers":    identifiers,
			"normalized_ids": normalized,
		}, "未找到匹配客户")
		return
	}
	response.Success(ctx, gin.H{
		"customers":   customers,
		"matched":     true,
		"count":       len(customers),
		"identifiers": identifiers,
	}, "解析成功")
}

func parsePage(s string) int {
	if s == "" {
		return 1
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return 1
	}
	return n
}

func (c *CustomerOneIDController) GetMergeRules(ctx *gin.Context) {
	mergeRuleSvc := service.NewOneIDMergeRuleService()
	set, err := mergeRuleSvc.GetRules(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取合并规则失败")
		return
	}
	response.Success(ctx, set, "获取成功")
}

func (c *CustomerOneIDController) SaveMergeRules(ctx *gin.Context) {
	var set service.MergeRuleSet
	if err := ctx.ShouldBindJSON(&set); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	mergeRuleSvc := service.NewOneIDMergeRuleService()
	out, err := mergeRuleSvc.SaveRules(ctx.Request.Context(), &set)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "保存合并规则失败", err.Error())
		return
	}
	response.Success(ctx, out, "保存成功")
}

// DeleteMergeRule DELETE /api/oneid/merge-rules/:id（MergeRuleConfig.vue 删除按钮）
func (c *CustomerOneIDController) DeleteMergeRule(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的规则 ID")
		return
	}
	out, err := service.NewOneIDMergeRuleService().DeleteRule(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, out, "删除成功")
}

// PreviewMergeRules godoc
// @Summary      合并规则命中预览
// @Description  按提交的规则集预览「会合并哪些身份」，返回候选合并对数量与样例
// @Tags         OneID
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  []service.MergePreviewRule  true  "规则数组（前端 MergeRuleConfig.vue 扁平结构）"
// @Success      200   {object}  response.Response  "data: {candidateCount, samples:[{from,to,score}]}"
// @Failure      400   {object}  response.Response
// @Router       /api/oneid/merge-rules/preview [post]
func (c *CustomerOneIDController) PreviewMergeRules(ctx *gin.Context) {
	var rules []service.MergePreviewRule
	if err := ctx.ShouldBindJSON(&rules); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	mergeRuleSvc := service.NewOneIDMergeRuleService()
	out, err := mergeRuleSvc.PreviewMergeRules(ctx.Request.Context(), rules)
	if err != nil {
		response.ErrorFromDB(ctx, err, "合并预览失败")
		return
	}
	response.Success(ctx, out, "预览成功")
}

func parsePageSize(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return def
	}
	if n > 100 {
		return 100
	}
	return n
}
