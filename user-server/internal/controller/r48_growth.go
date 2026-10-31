package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type GrowthController struct {
	csPlus    *service.CustomerServicePlusService
	webhook   *service.WebhookSubService
	analytics *service.EmailGapService
}

// NewGrowthController 构造
func NewGrowthController() *GrowthController {
	return &GrowthController{
		csPlus:    service.NewCustomerServicePlusServiceFromGlobal(),
		webhook:   service.NewWebhookSubServiceFromGlobal(),
		analytics: service.NewEmailGapServiceFromGlobal(),
	}
}

// @Summary      secret 只给看一次
// @Tags      growth
// @Router    /api/webhook-subscriptions [get]
// ListWebhookSubs GET /api/webhook-subscriptions
func (c *GrowthController) ListWebhookSubs(ctx *gin.Context) {
	list, err := c.webhook.List(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	// 出参脱敏：secret 是**签名校验用的密钥**，直接序列化 ⇒ 任何能看列表的人
	// 都能拿走它去伪造 webhook 回调。
	//
	// 为什么创建口（CreateWebhookSub）**不**脱敏：那里是"secret 只给看一次"的
	// 契约（响应文案明写"请保存 secret，用于签名校验"）—— 订阅方必须在这里
	// 拿到它才能配置对端。列表是"浏览"路径，不需要 secret，脱敏不影响任何用法。
	for _, sub := range list {
		if sub == nil {
			continue
		}
		sub.Secret = maskCredential(sub.Secret)
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      订阅已创建（请保存 secret，用于签名校验）
// @Tags      growth
// @Router    /api/webhook-subscriptions [post]
// CreateWebhookSub POST /api/webhook-subscriptions {url, events}
func (c *GrowthController) CreateWebhookSub(ctx *gin.Context) {
	var req struct {
		URL    string `json:"url" binding:"required"`
		Events string `json:"events"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	sub, err := c.webhook.Create(ctx.Request.Context(), req.URL, req.Events)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, sub, "订阅已创建（请保存 secret，用于签名校验）")
}

// @Summary      已删除
// @Tags      growth
// @Router    /api/webhook-subscriptions/{id} [delete]
// DeleteWebhookSub DELETE /api/webhook-subscriptions/:id
func (c *GrowthController) DeleteWebhookSub(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.webhook.Delete(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "已删除")
}

// @Summary      自定义属性已更新
// @Tags      growth
// @Router    /api/customers/{id}/custom-attributes [put]
// SetCustomAttributes PUT /api/customers/:id/custom-attributes {attrs}
func (c *GrowthController) SetCustomAttributes(ctx *gin.Context) {
	var req struct {
		Attrs map[string]any `json:"attrs" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "attrs 必填（JSON 对象）")
		return
	}
	merged, err := c.csPlus.SetCustomAttributes(ctx.Request.Context(), ctx.Param("id"), req.Attrs)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, merged, "自定义属性已更新")
}

// CreateSavedView POST /api/saved-views {name, route, filter}
func (c *GrowthController) CreateSavedView(ctx *gin.Context) {
	var req struct {
		Name   string `json:"name" binding:"required"`
		Route  string `json:"route" binding:"required"`
		Filter string `json:"filter"`
		UserID uint   `json:"user_id"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	uid := req.UserID
	if v, exists := ctx.Get("user_id"); exists {
		if idv, ok := v.(uint); ok {
			uid = idv
		}
	}
	v, err := c.csPlus.CreateSavedView(ctx.Request.Context(), uid, req.Name, req.Route, req.Filter)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, v, "视图已保存")
}

// @Summary      查询已保存的分析视图（按路由）
// @Tags      growth
// @Router    /api/saved-views [get]
// ListSavedViews GET /api/saved-views?route=
func (c *GrowthController) ListSavedViews(ctx *gin.Context) {
	var uid uint
	if v, exists := ctx.Get("user_id"); exists {
		if idv, ok := v.(uint); ok {
			uid = idv
		}
	}
	list, err := c.csPlus.ListSavedViews(ctx.Request.Context(), uid, ctx.Query("route"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// DeleteSavedView DELETE /api/saved-views/:id
func (c *GrowthController) DeleteSavedView(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var uid uint
	if v, exists := ctx.Get("user_id"); exists {
		if idv, ok := v.(uint); ok {
			uid = idv
		}
	}
	if err := c.csPlus.DeleteSavedView(ctx.Request.Context(), id, uid); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "视图已删除")
}

// @Summary POST /api/report-subscriptions {email, schedule}
// @Tags      growth
// @Router    /api/report-subscriptions [post]
// CreateReportSub POST /api/report-subscriptions {email, schedule}
func (c *GrowthController) CreateReportSub(ctx *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required"`
		Schedule string `json:"schedule"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	sub, err := c.csPlus.CreateReportSubscription(ctx.Request.Context(), req.Email, req.Schedule)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, sub, "订阅成功")
}

// @Summary      查询报表订阅列表
// @Tags      growth
// @Router    /api/report-subscriptions [get]
// ListReportSubs GET /api/report-subscriptions
func (c *GrowthController) ListReportSubs(ctx *gin.Context) {
	list, err := c.csPlus.ListReportSubscriptions(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      已退订
// @Tags      growth
// @Router    /api/report-subscriptions/{id} [delete]
// DeleteReportSub DELETE /api/report-subscriptions/:id
func (c *GrowthController) DeleteReportSub(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.csPlus.DeleteReportSubscription(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "已退订")
}

// @Summary      报表已发送
// @Tags      growth
// @Router    /api/report-subscriptions/send-now [post]
// SendReportsNow POST /api/report-subscriptions/send-now（手动触发一次）
func (c *GrowthController) SendReportsNow(ctx *gin.Context) {
	sent, err := c.csPlus.SendScheduledReports(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"sent": sent}, "报表已发送")
}

// @Summary      导出会话转写文本（txt/csv 附件下载）
// @Tags      growth
// @Router    /api/customer-sessions/{id}/transcript [get]
// Transcript GET /api/customer-sessions/:id/transcript?format=txt|csv
func (c *GrowthController) Transcript(ctx *gin.Context) {
	format := ctx.DefaultQuery("format", "txt")
	contentType, body, err := c.csPlus.SessionTranscript(ctx.Request.Context(), ctx.Param("id"), format == "csv")
	if HandleServiceError(ctx, err) {
		return
	}
	if contentType == "text/csv" {
		ctx.Header("Content-Disposition", "attachment; filename=transcript-"+ctx.Param("id")+".csv")
	} else {
		ctx.Header("Content-Disposition", "attachment; filename=transcript-"+ctx.Param("id")+".txt")
	}
	ctx.Data(http.StatusOK, contentType+"; charset=utf-8", []byte(body))
}

// @Summary      AI 性能分析（按天窗口）
// @Tags      growth
// @Router    /api/analytics/ai-performance [get]
// AIPerformance GET /api/analytics/ai-performance?days=7
func (c *GrowthController) AIPerformance(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	res, err := c.analytics.AIPerformance(ctx.Request.Context(), days)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}
