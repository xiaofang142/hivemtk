// 独立部署版本：单租户，Controller 仅做参数解析与响应包装
package controller

import (
	"net/http"

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

// GetOverview 获取全旅程总览：每个阶段的客户数 + 转化率
func (c *CustomerJourneyController) GetOverview(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")

	if customerID != "" {
		state := c.svc.GetState(ctx.Request.Context(), customerID)
		response.Success(ctx, state, "查询成功")
		return
	}

	overview := c.svc.GetOverview(ctx.Request.Context())
	response.Success(ctx, overview, "查询成功")
}

// TransitionStage 手动迁移阶段
func (c *CustomerJourneyController) TransitionStage(ctx *gin.Context) {
	var req dto.JourneyTransitionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.CustomerID == "" || req.ToStage == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 和 to_stage 不能为空")
		return
	}
	operatorID, _ := ctx.Get("user_id")
	operatorStr, _ := operatorID.(string)
	if operatorStr == "" {
		operatorStr = "manual"
	}
	event, err := c.svc.Transition(ctx, req.CustomerID, dto.JourneyStage(req.ToStage), req.Source, operatorStr, req.Reason, nil)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "迁移失败: "+err.Error())
		return
	}
	response.Success(ctx, event, "迁移成功")
}

// TouchCustomer 记录互动（不改变阶段）
func (c *CustomerJourneyController) TouchCustomer(ctx *gin.Context) {
	var req struct {
		CustomerID string `json:"customer_id" binding:"required"`
		Source     string `json:"source"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	c.svc.Touch(ctx.Request.Context(), req.CustomerID, req.Source)
	response.Success(ctx, nil, "互动已记录")
}

// ListByStage 按阶段列出客户
func (c *CustomerJourneyController) ListByStage(ctx *gin.Context) {
	stage := ctx.Query("stage")
	if stage == "" {
		response.Error(ctx, http.StatusBadRequest, "stage 参数不能为空")
		return
	}
	ids := c.svc.ListByStage(ctx.Request.Context(), dto.JourneyStage(stage))
	response.Success(ctx, gin.H{"stage": stage, "customer_ids": ids, "count": len(ids)}, "查询成功")
}

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
