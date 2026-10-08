package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ProactiveReachController 主动触达控制器（按 OneID 智能选渠道）
type ProactiveReachController struct {
	svc *service.ProactiveReachService
}

// NewProactiveReachController 创建主动触达控制器
func NewProactiveReachController(svc *service.ProactiveReachService) *ProactiveReachController {
	return &ProactiveReachController{svc: svc}
}

// ProactiveSend 主动发送消息
// POST /api/reach/proactive/send
func (c *ProactiveReachController) ProactiveSend(ctx *gin.Context) {
	var req service.ProactiveReachRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	resp, err := c.svc.ReachByCustomer(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}

	response.Success(ctx, resp, "触达成功")
}

// QuickSend 快速发送（指定渠道 + 接收方）
// POST /api/reach/proactive/quick
func (c *ProactiveReachController) QuickSend(ctx *gin.Context) {
	var req struct {
		Channel   string `json:"channel" binding:"required"`
		AccountID string `json:"account_id,omitempty"`
		Phone     string `json:"phone,omitempty"`
		Email     string `json:"email,omitempty"`
		UserID    string `json:"user_id,omitempty"`
		Content   string `json:"content" binding:"required"`
		Subject   string `json:"subject,omitempty"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	v2req := &service.ProactiveReachRequest{
		Phone:     req.Phone,
		Email:     req.Email,
		Content:   req.Content,
		Subject:   req.Subject,
		AccountID: req.AccountID,
	}

	if req.UserID != "" {
		v2req.OneID = req.UserID
	}

	resp, err := c.svc.ReachByCustomer(ctx.Request.Context(), v2req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}

	response.Success(ctx, resp, "发送成功")
}

// ProactiveSendFromCustomer 从客户 ID 发起触达（v2 智能选渠道）
// POST /api/reach/proactive/customer/:customer_id
func (c *ProactiveReachController) ProactiveSendFromCustomer(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 不能为空")
		return
	}

	var body struct {
		Content           string   `json:"content" binding:"required"`
		Subject           string   `json:"subject,omitempty"`
		PreferredChannels []string `json:"preferred_channels,omitempty"`
		DryRun            bool     `json:"dry_run,omitempty"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	req := &service.ProactiveReachRequest{
		CustomerID:        customerID,
		Content:           body.Content,
		Subject:           body.Subject,
		PreferredChannels: body.PreferredChannels,
		DryRun:            body.DryRun,
	}
	resp, err := c.svc.ReachByCustomer(ctx.Request.Context(), req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "触达失败: "+err.Error())
		return
	}
	response.Success(ctx, resp, "触达成功")
}

// ListChannels 列出客户的可用渠道（v2 - 按 OneID 完整身份）
// GET /api/reach/proactive/customer/:customer_id/channels
func (c *ProactiveReachController) ListChannels(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 不能为空")
		return
	}

	cust, err := c.svc.LoadCustomer(ctx.Request.Context(), customerID, "")
	if err != nil || cust == nil {
		response.Error(ctx, http.StatusNotFound, "客户不存在")
		return
	}

	// 预览口径必须与外发闸门同源：外发认「反规范化列 + customer_channels 绑定表」的并集
	//（见 service.CustomerIdentity），这里若只认反规范化列，就会对同一个客户给出
	//「可用渠道=空、外发却说该渠道不可用」这种自相矛盾的画面。
	avail, err := c.svc.ListChannelAvailability(ctx.Request.Context(), cust)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询渠道身份失败")
		return
	}
	channels := make([]map[string]any, 0, len(avail))
	for _, a := range avail {
		channels = append(channels, map[string]any{
			"channel":      a.Channel,
			"identity":     a.Identity,
			"has_identity": a.Identity != "",
		})
	}
	response.Success(ctx, gin.H{
		"customer_id": customerID,
		"one_id":      cust.UnifiedID,
		"name":        cust.Name,
		"channels":    channels,
		"total":       len(channels),
	}, "查询成功")
}

// ValidateReach 验证触达条件（兼容性保留）
// POST /api/reach/proactive/validate
func (c *ProactiveReachController) ValidateReach(ctx *gin.Context) {
	var req service.ProactiveReachRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	req.DryRun = true
	resp, err := c.svc.ReachByCustomer(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "验证失败: "+err.Error())
		return
	}
	response.Success(ctx, resp, "验证完成")
}

// BatchProactiveSend 批量触达（兼容性保留）
// POST /api/reach/proactive/batch
func (c *ProactiveReachController) BatchProactiveSend(ctx *gin.Context) {
	var req struct {
		Targets  []service.ProactiveReachRequest `json:"targets" binding:"required"`
		Template string                          `json:"template,omitempty"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	results := make([]map[string]any, 0, len(req.Targets))
	successCount := 0
	failCount := 0

	for i, target := range req.Targets {
		if req.Template != "" && target.Content == "" {
			target.Content = req.Template
		}
		resp, err := c.svc.ReachByCustomer(ctx.Request.Context(), &target)
		result := map[string]any{"index": i}
		if err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
			failCount++
		} else {
			result["status"] = "success"
			result["data"] = resp
			successCount++
		}
		results = append(results, result)
	}

	response.Success(ctx, gin.H{
		"total":         len(req.Targets),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":       results,
	}, "批量触达完成")
}
