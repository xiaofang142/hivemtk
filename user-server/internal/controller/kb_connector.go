package controller

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/kbconnector"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// kb_connector.go 第三方知识库连接器管理端（通用框架：type 注册制）
// 现内置 ima（腾讯 IMA）与 webhttp（通用 HTTP 清单）。

func maskConfig(cfg string) string {
	if strings.TrimSpace(cfg) == "" {
		return ""
	}
	m := map[string]any{}
	if err := json.Unmarshal([]byte(cfg), &m); err != nil {
		return ""
	}
	for k := range m {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "key") || strings.Contains(kl, "secret") {
			m[k] = "***"
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// ListKBConnectors GET /api/kb-connectors
func ListKBConnectors(ctx *gin.Context) {
	g := db.GetDB()
	if g == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var rows []model.ExternalKBConnector
	if err := g.WithContext(ctx).Order("id DESC").Limit(100).Find(&rows).Error; err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{
			"id": r.ID, "type": r.Type, "name": r.Name,
			"config_masked":    maskConfig(r.Config),
			"interval_minutes": r.IntervalMinutes, "enabled": r.Enabled,
			"last_sync_at": r.LastSyncAt, "last_status": r.LastStatus,
			"last_summary": r.LastSummary,
		})
	}
	response.Success(ctx, gin.H{"list": out, "types": kbconnector.Types()}, "ok")
}

// CreateKBConnector POST /api/kb-connectors
func CreateKBConnector(ctx *gin.Context) {
	g := db.GetDB()
	if g == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		Type            string          `json:"type" binding:"required"`
		Name            string          `json:"name"`
		Config          json.RawMessage `json:"config"`
		IntervalMinutes int             `json:"interval_minutes"`
		Enabled         *bool           `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := kbconnector.LookupProvider(req.Type); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	conn := model.ExternalKBConnector{
		Type: req.Type, Name: req.Name,
		Config: string(req.Config), IntervalMinutes: req.IntervalMinutes,
		Enabled: enabled, LastStatus: "idle",
	}
	if err := g.WithContext(ctx).Create(&conn).Error; err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": conn.ID}, "创建成功")
}

// UpdateKBConnector PUT /api/kb-connectors/:id
func UpdateKBConnector(ctx *gin.Context) {
	g := db.GetDB()
	if g == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		Name            *string          `json:"name"`
		Config          *json.RawMessage `json:"config"`
		IntervalMinutes *int             `json:"interval_minutes"`
		Enabled         *bool            `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Config != nil {
		updates["config"] = string(*req.Config)
	}
	if req.IntervalMinutes != nil {
		updates["interval_minutes"] = *req.IntervalMinutes
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if len(updates) == 0 {
		response.Error(ctx, http.StatusBadRequest, "无可更新字段")
		return
	}
	if err := g.WithContext(ctx).Model(&model.ExternalKBConnector{}).
		Where("id = ?", ctx.Param("id")).Updates(updates).Error; err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已更新")
}

// DeleteKBConnector DELETE /api/kb-connectors/:id（已导入文档保留在知识库）
func DeleteKBConnector(ctx *gin.Context) {
	g := db.GetDB()
	if g == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	if err := g.WithContext(ctx).Delete(&model.ExternalKBConnector{}, ctx.Param("id")).Error; err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true, "note": "已导入文档保留在知识库"}, "已删除")
}

// SyncKBConnector POST /api/kb-connectors/:id/sync 手动触发一次同步
func SyncKBConnector(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	res, err := kbconnector.Sync(ctx, uint(id))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, res, res.Summary)
}
