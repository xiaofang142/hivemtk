// sales_workbench_controller.go 销售工作台控制器（五层 L2）
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SalesWorkbenchController 销售工作台控制器
type SalesWorkbenchController struct {
	svc *service.SalesWorkbenchService
}

// NewSalesWorkbenchController 构造（服务由 app.InitSalesWorkbenchRuntime 装配，
// 经 router 传入 —— 与 NewOrderDraftController 同一形态，控制器不自己 new 服务，
// 否则装配层那次注入就白做了）。
func NewSalesWorkbenchController(svc *service.SalesWorkbenchService) *SalesWorkbenchController {
	return &SalesWorkbenchController{svc: svc}
}

// GetOverview GET /api/sales-workbench/overview?sales_id=xxx
//
// sales_id 由查询参数给：工作台是"某个销售看自己的概览"，排行/待办全按它聚合。
// 服务未装配（运行时还没挂上）回 503 而不是 200+空壳，免得前端把"没装配"
// 渲染成"你今天没单子"。
func (c *SalesWorkbenchController) GetOverview(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "工作台服务未装配")
		return
	}
	salesID := ctx.Query("sales_id")
	if salesID == "" {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	data := c.svc.GetOverview(ctx.Request.Context(), salesID)
	response.Success(ctx, data, "ok")
}

// windowDays 解析窗口参数 days（可选，默认 30，上限 365）。
// 非法值回 (0,false) ⇒ 控制器统一 400，不让垃圾参数进到读侧。
func windowDays(ctx *gin.Context) (int, bool) {
	raw := ctx.DefaultQuery("days", "30")
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 || days > 365 {
		return 0, false
	}
	return days, true
}

// GetTeamDashboard GET /api/sales-workbench/team-dashboard?days=30
//
// 团队排行 + AI 产能 + 销冠画像 + 漏斗的事件流读口（I7）。与 SQL 口径的
// /api/ai-productivity/* 并存不替换：两套字段集本就不同，换源=改历史数字。
// 未装配回 503 的口径与 GetOverview 一致。
func (c *SalesWorkbenchController) GetTeamDashboard(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "工作台服务未装配")
		return
	}
	days, ok := windowDays(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	data := c.svc.GetTeamDashboard(ctx.Request.Context(), days)
	if data == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "工作台服务未装配")
		return
	}
	response.Success(ctx, data, "ok")
}

// GetChampionProfile GET /api/sales-workbench/champion?days=30
// 销冠画像的事件流读口（I7），口径同上。
func (c *SalesWorkbenchController) GetChampionProfile(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "工作台服务未装配")
		return
	}
	days, ok := windowDays(ctx)
	if !ok {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	data := c.svc.GetChampionProfile(ctx.Request.Context(), days)
	if data == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "工作台服务未装配")
		return
	}
	response.Success(ctx, data, "ok")
}
