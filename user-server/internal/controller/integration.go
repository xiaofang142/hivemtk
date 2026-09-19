package controller

import (
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// IntegrationController 第三方对接控制器
type IntegrationController struct {
	integrationService *service.IntegrationService
}

// NewIntegrationController 创建第三方对接控制器实例
func NewIntegrationController() *IntegrationController {
	return &IntegrationController{
		integrationService: service.NewIntegrationService(),
	}
}

// CreateAccount 创建对接账号
func (c *IntegrationController) CreateAccount(ctx *gin.Context) {

	var req service.CreateIntegrationAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	account, err := c.integrationService.CreateIntegrationAccount(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "创建对接账号") {
		return
	}

	response.Success(ctx, account, "创建成功")
}

func maskCredential(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

func (c *IntegrationController) GetAccountList(ctx *gin.Context) {

	accounts, err := c.integrationService.GetIntegrationAccountList(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	for _, acc := range accounts {
		acc.APISecret = maskCredential(acc.APISecret)
		acc.AccessToken = maskCredential(acc.AccessToken)
		acc.RefreshToken = maskCredential(acc.RefreshToken)
	}

	response.Success(ctx, accounts, "获取成功")
}

// GetAccountByID 获取对接账号详情
func (c *IntegrationController) GetAccountByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.integrationService.GetIntegrationAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	account.APISecret = maskCredential(account.APISecret)
	account.AccessToken = maskCredential(account.AccessToken)
	account.RefreshToken = maskCredential(account.RefreshToken)

	response.Success(ctx, account, "获取成功")
}

// UpdateAccount 更新对接账号
func (c *IntegrationController) UpdateAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	var req service.CreateIntegrationAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	account, err := c.integrationService.UpdateIntegrationAccount(ctx.Request.Context(), uint(id), &req)
	if HandleDBError(ctx, err, "更新对接账号") {
		return
	}

	response.Success(ctx, account, "更新成功")
}

// DeleteAccount 删除对接账号
func (c *IntegrationController) DeleteAccount(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	if HandleDBError(ctx, c.integrationService.DeleteIntegrationAccount(ctx.Request.Context(), uint(id)), "删除对接账号") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// SyncCustomers 同步客户数据
func (c *IntegrationController) SyncCustomers(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.integrationService.GetIntegrationAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.integrationService.SyncCustomers(ctx.Request.Context(), account)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}

// SyncOrders 同步订单数据
func (c *IntegrationController) SyncOrders(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.integrationService.GetIntegrationAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.integrationService.SyncOrders(ctx.Request.Context(), account)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}

// SyncProducts 同步商品数据
func (c *IntegrationController) SyncProducts(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.integrationService.GetIntegrationAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	count, err := c.integrationService.SyncProducts(ctx.Request.Context(), account)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"count": count}, "同步成功")
}

// TestIntegration 测试对接账号连接
func (c *IntegrationController) TestIntegration(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}

	account, err := c.integrationService.GetIntegrationAccountByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	if err := c.integrationService.TestConnection(ctx.Request.Context(), account); err != nil {
		response.Error(ctx, http.StatusBadRequest, "连接测试失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"account_id": account.ID,
		"platform":   account.Platform,
		"status":     "ok",
	}, "连接测试成功")
}

// GetSyncLogs 获取同步日志
func (c *IntegrationController) GetSyncLogs(ctx *gin.Context) {

	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	logs, total, err := c.integrationService.GetSyncLogs(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetExternalCustomers 获取外部客户列表
func (c *IntegrationController) GetExternalCustomers(ctx *gin.Context) {

	platform := ctx.Query("platform")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	customers, total, err := c.integrationService.GetExternalCustomers(ctx.Request.Context(), platform, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      customers,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetExternalOrders 获取外部订单列表
func (c *IntegrationController) GetExternalOrders(ctx *gin.Context) {

	platform := ctx.Query("platform")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	orders, total, err := c.integrationService.GetExternalOrders(ctx.Request.Context(), platform, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      orders,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetExternalOrdersByCustomer 按客户手机/姓名查询近期外部订单（客服 360 视图）
func (c *IntegrationController) GetExternalOrdersByCustomer(ctx *gin.Context) {
	phone := ctx.Query("phone")
	name := ctx.Query("name")
	if phone == "" && name == "" {
		response.Error(ctx, http.StatusBadRequest, "phone 与 name 至少提供一个")
		return
	}
	orders, err := c.integrationService.GetExternalOrdersByCustomer(ctx.Request.Context(), phone, name)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": orders, "count": len(orders)}, "获取成功")
}

// ReceiveOrderWebhook 接收电商订单状态推送（近实时刷新本地订单镜像）。
// 仅接受平台侧推送；镜像为只读，客服不创建/履约订单。
//
// 两个入口共用本方法：公开 + HMAC 的新契约（/api/integration/webhook/order/:platform）
// 与 auth 组里的旧契约（/api/integration/order-webhook/:platform，已标 deprecation）。
// 因此下面按**入口**分流，而不是假设"一定是验签进来的"。
func (c *IntegrationController) ReceiveOrderWebhook(ctx *gin.Context) {
	platform := ctx.Param("platform")
	// 验签在中间件里做，这里只做一次事后核对：公开入口的身份由路径前缀决定，
	// 而"这条路由挂了 guard"目前是注册处的约定 —— 约定会在有人新注册一条同类路由、
	// 或把 guard 挪走时失效。核对失败就当场拒，绝不把未验签的推送写成订单镜像。
	if strings.HasPrefix(ctx.FullPath(), middleware.OrderWebhookVerifiedPathPrefix) {
		if verified := ctx.GetString(middleware.VerifiedWebhookKey); verified != platform {
			logger.Warnf("[order-webhook] 公开入口缺少验签凭据：path=%s ctx平台=%q 参数平台=%q ⇒ 拒绝（路由是否挂了 guard 需排查）",
				ctx.FullPath(), verified, platform)
			response.Error(ctx, http.StatusForbidden, "回调未经签名校验")
			return
		}
	}
	var body map[string]any
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败："+err.Error())
		return
	}
	orderID, _ := body["order_id"].(string)
	status, _ := body["status"].(string)
	if orderID == "" {
		response.Error(ctx, http.StatusBadRequest, "order_id 不能为空")
		return
	}
	if status == "" {
		status = "unknown"
	}
	if err := c.integrationService.UpsertOrderFromWebhook(ctx.Request.Context(), platform, orderID, status, body); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"platform": platform, "order_id": orderID, "status": status}, "已接收并处理")
}

// GetTemplates 第三方对接模板列表（integration_templates 表）
// GET /api/integrations/templates?platform=dingtalk&category=erp&page=1&page_size=20
func (c *IntegrationController) GetTemplates(ctx *gin.Context) {
	platform := ctx.Query("platform")
	category := ctx.Query("category")
	var enabled *bool
	if v := ctx.Query("enabled"); v != "" {
		b := v == "true"
		enabled = &b
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	tplSvc := service.NewIntegrationTemplateService()
	templates, total, err := tplSvc.List(ctx.Request.Context(), platform, category, enabled, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取模板列表失败")
		return
	}
	response.SuccessWithPage(ctx, templates, int64(page), int64(pageSize), total)
}

// GetCategories 对接模板分类分组（从 integration_templates 聚合 distinct category）
// GET /api/integrations/categories
func (c *IntegrationController) GetCategories(ctx *gin.Context) {
	tplSvc := service.NewIntegrationTemplateService()
	all, err := tplSvc.ListBuiltIn(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取分类失败")
		return
	}

	type catItem struct {
		Category  string   `json:"category"`
		Label     string   `json:"label"`
		Count     int      `json:"count"`
		Platforms []string `json:"platforms"`
	}
	catMap := make(map[string]*catItem)
	for _, t := range all {
		cat, ok := catMap[t.Category]
		if !ok {
			cat = &catItem{Category: t.Category, Label: categoryLabel(t.Category), Platforms: []string{}}
			catMap[t.Category] = cat
		}
		cat.Count++
		found := false
		for _, p := range cat.Platforms {
			if p == t.Platform {
				found = true
				break
			}
		}
		if !found {
			cat.Platforms = append(cat.Platforms, t.Platform)
		}
	}
	result := make([]catItem, 0, len(catMap))
	for _, v := range catMap {
		result = append(result, *v)
	}
	response.Success(ctx, result, "获取成功")
}

func categoryLabel(cat string) string {
	labels := map[string]string{
		model.CategoryERP:     "ERP 企业资源计划",
		model.CategoryCRM:     "CRM 客户关系管理",
		model.CategoryHR:      "HR 人力资源",
		model.CategoryFinance: "Finance 财务管理",
	}
	if l, ok := labels[cat]; ok {
		return l
	}
	return cat
}

// GetExternalProducts 获取外部商品列表
func (c *IntegrationController) GetExternalProducts(ctx *gin.Context) {

	platform := ctx.Query("platform")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	products, total, err := c.integrationService.GetExternalProducts(ctx.Request.Context(), platform, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      products,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}
