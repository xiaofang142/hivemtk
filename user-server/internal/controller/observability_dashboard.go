package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ObservabilityDashboardController 统一看板（D2 AI 全链路可观测性 + B2 渠道健康，specs/007）。
//
//	GET /api/observability/dashboard —— 运营面读统一口径快照（JWT 鉴权，登录态）
//
// 只读：不写任何业务表。成本/送达率/转化漏斗/渠道在线率/封禁信号的口径全部收敛在
// service.ObservabilityDashboardService，本控制器只做参数透传与响应包装。
type ObservabilityDashboardController struct {
	svc *service.ObservabilityDashboardService
}

// NewObservabilityDashboardController 构造 controller。svc 为 nil 时端点回 503——
// 装配缺失必须是一次显式失败，而不是 nil 解引用。
func NewObservabilityDashboardController(svc *service.ObservabilityDashboardService) *ObservabilityDashboardController {
	return &ObservabilityDashboardController{svc: svc}
}

// @Summary      可观测性仪表盘快照（按时间窗）
// @Tags      observability-dashboard
// @Router    /api/observability/dashboard [get]
// Snapshot GET /api/observability/dashboard?window=7d
//
// window 只收 24h / 7d / 30d，非法回落 7d（口径见 service.windowDuration）。
func (c *ObservabilityDashboardController) Snapshot(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "observability dashboard service not initialized")
		return
	}
	snap, err := c.svc.Snapshot(ctx.Request.Context(), ctx.Query("window"))
	if err != nil {
		status := http.StatusInternalServerError
		msg := "看板数据加载失败，请稍后重试"
		if errors.Is(err, service.ErrDashboardRepoNotReady) {
			status = http.StatusServiceUnavailable
			msg = err.Error()
		} else {
			logger.Errorf("observability dashboard snapshot failed: %v", err)
		}
		response.Error(ctx, status, msg)
		return
	}
	response.Success(ctx, snap, "ok")
}
