package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// BridgeSelectorHealthController 桥接选择器健康度（B1-a）。
//
//	POST /api/bridge/selector-health  —— 扩展上报「试了几次 / 命中几次」（桥接凭证鉴权）
//	GET  /api/bridge/selector-health  —— 运营面读判定结果（JWT 鉴权，登录态）
//
// 两条路由挂在两个不同的组上，这不是笔误：上报只认实例级桥接凭证（用户机器上的扩展
// 没有登录态），读的是一张给运营看的表。把 GET 也挂进桥接组，等于让任意一台装有扩展的
// 机器能读到全部渠道的健康度——那是租户内部的运营数据，不归扩展的凭证管。
type BridgeSelectorHealthController struct {
	svc *service.SelectorHealthService
}

// NewBridgeSelectorHealthController 构造 controller。svc 为 nil 时全部端点回 503——
// 装配缺失必须是一次显式失败，而不是 nil 解引用。
func NewBridgeSelectorHealthController(svc *service.SelectorHealthService) *BridgeSelectorHealthController {
	return &BridgeSelectorHealthController{svc: svc}
}

// @Summary      上报桥接选择器健康数据（扩展侧打点）
// @Tags      bridge-selector-health
// @Router    /api/bridge/selector-health [post]
// Ingest POST /api/bridge/selector-health
//
// 响应体刻意保持扁平稳定（扩展侧直接读它打日志，不经过 response 包装层的 i18n）：
//
//	{ "ok": true, "threshold": 60, "degraded": ["messageItem"], "items": [...] }
func (c *BridgeSelectorHealthController) Ingest(ctx *gin.Context) {
	if c.svc == nil {
		response.Envelope(ctx, http.StatusServiceUnavailable, gin.H{"ok": false, "error": "selector health service not initialized"})
		return
	}
	var rep service.SelectorHealthReport
	if err := ctx.ShouldBindJSON(&rep); err != nil {
		response.Envelope(ctx, http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	views, err := c.svc.Ingest(ctx.Request.Context(), rep)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, service.ErrSelectorHealthInvalidReport):
			status = http.StatusBadRequest
		case errors.Is(err, service.ErrSelectorHealthRepoNotReady):
			status = http.StatusServiceUnavailable
		}
		response.Envelope(ctx, status, gin.H{"ok": false, "error": err.Error()})
		return
	}
	// degraded 只列 broken/degraded 的字段名：扩展侧在自检页上直接展示这句，
	// 让它自己去拼的话各版本会拼出不同形状。
	degraded := make([]string, 0, len(views))
	for _, v := range views {
		if v.Status == service.SelectorHealthStatusBroken || v.Status == service.SelectorHealthStatusDegraded {
			degraded = append(degraded, v.Field)
		}
	}
	response.Envelope(ctx, http.StatusOK, gin.H{
		"ok":        true,
		"threshold": c.svc.Threshold(ctx.Request.Context()),
		"degraded":  degraded,
		"items":     views,
	})
}

// @Summary      查询桥接选择器健康读数（按渠道+字段）
// @Tags      bridge-selector-health
// @Router    /api/bridge/selector-health [get]
// List GET /api/bridge/selector-health
//
// 运营面读数：按渠道 + 字段逐行给判定。`threshold` 一并返回，便于运营知道
// 「这条为什么被判 degraded」——只给状态不给口径时，运维面对一片黄只能猜。
func (c *BridgeSelectorHealthController) List(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "selector health service not initialized")
		return
	}
	views, err := c.svc.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{
		"threshold":    c.svc.Threshold(ctx.Request.Context()),
		"min_attempts": service.SelectorHealthMinAttempts,
		"items":        views,
	}, "ok")
}
