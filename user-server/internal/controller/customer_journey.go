// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"fmt"
	"net/http"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CustomerJourneyController 客户旅程大屏控制器
type CustomerJourneyController struct {
	svc *service.CustomerJourneyService
}

// NewCustomerJourneyController 创建控制器
func NewCustomerJourneyController() *CustomerJourneyController {
	return &CustomerJourneyController{svc: service.NewCustomerJourneyService()}
}

// @Summary      客户旅程总览
// @Tags      customer-journey
// @Router    /api/customer-journey/overview [get]
// GetOverview 获取全旅程总览：每个阶段的客户数 + 转化率
func (c *CustomerJourneyController) GetOverview(ctx *gin.Context) {
	customerID := strings.TrimSpace(ctx.Query("customer_id"))

	if customerID != "" {
		state := c.svc.GetState(ctx.Request.Context(), customerID)
		response.Success(ctx, state, "查询成功")
		return
	}

	overview := c.svc.GetOverview(ctx.Request.Context())
	response.Success(ctx, overview, "查询成功")
}

// @Summary 手动迁移阶段
// @Tags      customer-journey
// @Router    /api/customer-journey/transition [post]
// TransitionStage 手动迁移阶段
func (c *CustomerJourneyController) TransitionStage(ctx *gin.Context) {
	var req dto.JourneyTransitionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 与 to_stage 均为必填，source / reason 可选")
		return
	}
	customerID := strings.TrimSpace(req.CustomerID)
	toStage := strings.TrimSpace(string(req.ToStage))
	if customerID == "" || toStage == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 和 to_stage 不能为空")
		return
	}
	if !isKnownJourneyStage(toStage) {
		response.Error(ctx, http.StatusBadRequest, fmt.Sprintf("未知的阶段: %s（可用: %s）", toStage, strings.Join(journeyStageNames(), " / ")))
		return
	}
	operatorStr := journeyOperator(ctx)
	event, err := c.svc.Transition(ctx.Request.Context(), customerID, dto.JourneyStage(toStage), req.Source, operatorStr, req.Reason, nil)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "迁移失败: "+err.Error())
		return
	}
	if event == nil {
		// 服务层在「已在目标阶段」时不产生事件（幂等），别把 nil 当成功事件返回
		response.Success(ctx, gin.H{"customer_id": customerID, "current_stage": toStage, "changed": false}, "客户已处于目标阶段，无需迁移")
		return
	}
	response.Success(ctx, event, "迁移成功")
}

// @Summary      互动已记录
// @Tags      customer-journey
// @Router    /api/customer-journey/touch [post]
// TouchCustomer 记录互动（不改变阶段）
func (c *CustomerJourneyController) TouchCustomer(ctx *gin.Context) {
	var req struct {
		CustomerID string `json:"customer_id" binding:"required"`
		Source     string `json:"source"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 必填，source 可选")
		return
	}
	customerID := strings.TrimSpace(req.CustomerID)
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 不能为空")
		return
	}
	c.svc.Touch(ctx.Request.Context(), customerID, req.Source)
	response.Success(ctx, gin.H{"customer_id": customerID, "touched": true}, "互动已记录")
}

// @Summary      按阶段查询客户旅程
// @Tags      customer-journey
// @Router    /api/customer-journey/by-stage [get]
// ListByStage 按阶段列出客户
func (c *CustomerJourneyController) ListByStage(ctx *gin.Context) {
	stage := strings.TrimSpace(ctx.Query("stage"))
	if stage == "" {
		response.Error(ctx, http.StatusBadRequest, "stage 参数不能为空（可用: "+strings.Join(journeyStageNames(), " / ")+"）")
		return
	}
	if !isKnownJourneyStage(stage) {
		response.Error(ctx, http.StatusBadRequest, fmt.Sprintf("未知的阶段: %s（可用: %s）", stage, strings.Join(journeyStageNames(), " / ")))
		return
	}
	ids := c.svc.ListByStage(ctx.Request.Context(), dto.JourneyStage(stage))
	response.Success(ctx, gin.H{"stage": stage, "customer_ids": ids, "count": len(ids)}, "查询成功")
}

// @Summary      查询客户旅程阶段清单
// @Tags      customer-journey
// @Router    /api/customer-journey/stages [get]
// ListStages 列出所有阶段配置
func (c *CustomerJourneyController) ListStages(ctx *gin.Context) {
	stages := dto.AllStages
	result := make([]gin.H, 0, len(stages))
	for _, st := range stages {
		meta, ok := service.StageMetas[st]
		if !ok {
			continue
		}
		result = append(result, gin.H{
			"stage":            st,
			"label":            meta.Label,
			"description":      meta.Description,
			"default_followup": meta.DefaultFollowup.String(),
			"recommended_sop":  meta.RecommendedSOP,
			"owner_role":       meta.OwnerRole,
			"allow_ai_handle":  meta.AllowAIHandle,
			"auto_next_stage":  meta.AutoNextStage,
		})
	}
	response.Success(ctx, result, "查询成功")
}

// journeyOperator 取当前操作人标识。JWT 中间件把 user_id 存成 uint，
// 早先这里按 string 断言，恒失败导致迁移记录的操作人永远是 "manual"。
func journeyOperator(ctx *gin.Context) string {
	raw, ok := ctx.Get("user_id")
	if !ok {
		return "manual"
	}
	switch v := raw.(type) {
	case uint:
		if v > 0 {
			return fmt.Sprintf("user:%d", v)
		}
	case int:
		if v > 0 {
			return fmt.Sprintf("user:%d", v)
		}
	case uint64:
		if v > 0 {
			return fmt.Sprintf("user:%d", v)
		}
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return "manual"
}

// journeyStageNames 返回全部可用阶段的字符串形式（用于错误提示）。
func journeyStageNames() []string {
	names := make([]string, 0, len(dto.AllStages))
	for _, st := range dto.AllStages {
		names = append(names, string(st))
	}
	return names
}

// isKnownJourneyStage 判断阶段是否在词表内。
func isKnownJourneyStage(stage string) bool {
	for _, st := range dto.AllStages {
		if string(st) == stage {
			return true
		}
	}
	return false
}
