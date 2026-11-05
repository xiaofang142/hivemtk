package controller

// ego-lite 资产目录与平台管理端 HTTP 层（2026-11-05 二次审计）。

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/pkg/utils/response"
)

// ListAssets GET /egolite/assets（用户端橱窗：published + 扫描通过）
func (h *EgoLiteController) ListAssets(c *gin.Context) {
	list, err := h.asset.PublicList(c.Request.Context())
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list}})
}

// BroadcastUpgrade POST /egolite/admin/runner/upgrade（specs/015 C10）
func (h *EgoLiteController) BroadcastUpgrade(c *gin.Context) {
	n := h.runner.BroadcastUpgrade()
	response2(c, map[string]any{"notified": n}, "已向在线 runner 下发升级指令")
}

// PublishAssetFromFlow POST /egolite/admin/assets/publish（审核发布）
func (h *EgoLiteController) PublishAssetFromFlow(c *gin.Context) {
	var body struct {
		FlowID uint `json:"flow_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.FlowID == 0 {
		response.Envelope(c, 400, gin.H{"code": 1, "message": "flow_id 必填"})
		return
	}
	a, err := h.asset.PublishFromFlow(c.Request.Context(), currentUID(c), body.FlowID)
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": a})
}

// PublishAssetPack POST /egolite/admin/assets/pack（自包含通道：官方/基线资产）
func (h *EgoLiteController) PublishAssetPack(c *gin.Context) {
	var req egolite.PackPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	a, err := h.asset.PublishFromPack(c.Request.Context(), currentUID(c), req)
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": a})
}

// ListAdminFlows GET /egolite/admin/flows（审核工作台：跨用户按状态列流程）
func (h *EgoLiteController) ListAdminFlows(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.flow.ListByStatus(c.Request.Context(), c.Query("status"), page, size)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// ListAdminAssets GET /egolite/admin/assets
func (h *EgoLiteController) ListAdminAssets(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.asset.AdminList(c.Request.Context(), page, size)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// OfflineAsset POST /egolite/admin/assets/:id/offline
func (h *EgoLiteController) OfflineAsset(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	a, err := h.asset.Offline(c.Request.Context(), uint(id))
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": a})
}

// TelemetrySummary GET /egolite/admin/telemetry/summary（S-9：只回聚合层）
func (h *EgoLiteController) TelemetrySummary(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	buckets, total, truncated, err := h.telemetry.Summary(c.Request.Context(), hours)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{
		"window_hours": hours, "total": total, "truncated": truncated,
		"buckets": buckets,
	}})
}
