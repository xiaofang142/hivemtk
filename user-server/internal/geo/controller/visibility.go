package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// VisibilityController 可见性趋势 + Prompt 扇出研究控制器
type VisibilityController struct {
	visibilitySvc *service.VisibilityService
	fanoutSvc     *service.PromptFanoutService
}

// NewVisibilityController 创建控制器
func NewVisibilityController(visibilitySvc *service.VisibilityService, fanoutSvc *service.PromptFanoutService) *VisibilityController {
	return &VisibilityController{visibilitySvc: visibilitySvc, fanoutSvc: fanoutSvc}
}

// Trend GET /geo/visibility/trend?engine=&intent=&days=
func (c *VisibilityController) Trend(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	result, err := c.visibilitySvc.GetTrend(ctx.Request.Context(), service.TrendQuery{
		Engine: ctx.Query("engine"),
		Intent: ctx.Query("intent"),
		Days:   days,
	})
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取可见性趋势失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// EngineCompare GET /geo/visibility/engine-compare?intent=&days=
// 引擎维度对比（可见率/负面/引用 + 单日拆分），观测页主数据源
func (c *VisibilityController) EngineCompare(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	result, err := c.visibilitySvc.GetEngineCompare(ctx.Request.Context(), service.TrendQuery{
		Engine: ctx.Query("engine"),
		Intent: ctx.Query("intent"),
		Days:   days,
	})
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取引擎对比失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// Fanout POST /geo/prompt/fanout
func (c *VisibilityController) Fanout(ctx *gin.Context) {
	var req service.FanoutRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.fanoutSvc.Fanout(ctx.Request.Context(), &req)
	if err != nil {
		response.BusinessError(ctx, err.Error())
		return
	}
	response.Success(ctx, result, "扇出研究完成")
}
