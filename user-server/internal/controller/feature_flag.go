// feature_flag_controller.go FeatureFlag 控制器（K2 五层 L2：参数绑定+调 Service+响应）
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// FeatureFlagController 功能开关控制器
type FeatureFlagController struct {
	svc *service.FeatureFlagService
}

// NewFeatureFlagController 构造
func NewFeatureFlagController() *FeatureFlagController {
	return &FeatureFlagController{svc: service.NewFeatureFlagServiceFromGlobal()}
}

func actorIDFrom(ctx *gin.Context) uint {
	if v, ok := ctx.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// @Summary      分页查询功能旗标列表
// @Tags      feature-flag
// @Router    /api/feature-flags [get]
// List GET /api/feature-flags
func (c *FeatureFlagController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	list, total, err := c.svc.List(ctx.Request.Context(), page, pageSize)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary      按 ID 或 key 查询功能旗标
// @Tags      feature-flag
// @Router    /api/feature-flags/{id} [get]
// Get GET /api/feature-flags/:id（兼容数字 ID 与 key）
func (c *FeatureFlagController) Get(ctx *gin.Context) {
	f, err := c.svc.GetByIDOrKey(ctx.Request.Context(), ctx.Param("id"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "ok")
}

// @Summary      创建功能旗标
// @Tags      feature-flag
// @Router    /api/feature-flags [post]
// Create POST /api/feature-flags
func (c *FeatureFlagController) Create(ctx *gin.Context) {
	var req service.FlagCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：key 与 name 均为必填（description / enabled / rollout_percentage 可选）")
		return
	}
	f, err := c.svc.Create(ctx.Request.Context(), &req, actorIDFrom(ctx))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "创建成功")
}

// @Summary      更新功能旗标
// @Tags      feature-flag
// @Router    /api/feature-flags/{id} [put]
// Update PUT /api/feature-flags/:id
func (c *FeatureFlagController) Update(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.FlagUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name / description / rollout_percentage / enabled 需与服务端定义一致")
		return
	}
	f, err := c.svc.Update(ctx.Request.Context(), id, &req, actorIDFrom(ctx))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "更新成功")
}

// @Summary      删除功能旗标
// @Tags      feature-flag
// @Router    /api/feature-flags/{id} [delete]
// Delete DELETE /api/feature-flags/:id
func (c *FeatureFlagController) Delete(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), id, actorIDFrom(ctx)); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "删除成功")
}

// @Summary      已启用
// @Tags      feature-flag
// @Router    /api/feature-flags/{id}/enable [post]
// Enable POST /api/feature-flags/:id/enable
func (c *FeatureFlagController) Enable(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	f, err := c.svc.SetEnabled(ctx.Request.Context(), id, true, actorIDFrom(ctx))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "已启用")
}

// @Summary      已禁用
// @Tags      feature-flag
// @Router    /api/feature-flags/{id}/disable [post]
// Disable POST /api/feature-flags/:id/disable
func (c *FeatureFlagController) Disable(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	f, err := c.svc.SetEnabled(ctx.Request.Context(), id, false, actorIDFrom(ctx))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "已禁用")
}

// @Summary      灰度已更新
// @Tags      feature-flag
// @Router    /api/feature-flags/{id}/rollout [post]
// Rollout POST /api/feature-flags/:id/rollout {percentage}
func (c *FeatureFlagController) Rollout(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		Percentage *int `json:"percentage" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Percentage == nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：percentage 必填")
		return
	}
	f, err := c.svc.SetRollout(ctx.Request.Context(), id, *req.Percentage, actorIDFrom(ctx))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "灰度已更新")
}

// @Summary POST /api/feature-flags/evaluate {key, attributes}
// @Tags      feature-flag
// @Router    /api/feature-flags/evaluate [post]
// Evaluate POST /api/feature-flags/evaluate {key, attributes}
func (c *FeatureFlagController) Evaluate(ctx *gin.Context) {
	var req struct {
		Key        string         `json:"key" binding:"required"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：key 必填（attributes 可选）")
		return
	}
	res := c.svc.Evaluate(ctx.Request.Context(), req.Key, req.Attributes)
	response.Success(ctx, res, "ok")
}

// @Summary POST /api/feature-flags/evaluate-batch {keys[], attributes}
// @Tags      feature-flag
// @Router    /api/feature-flags/evaluate-batch [post]
// EvaluateBatch POST /api/feature-flags/evaluate-batch {keys[], attributes}
func (c *FeatureFlagController) EvaluateBatch(ctx *gin.Context) {
	var req struct {
		Keys       []string       `json:"keys" binding:"required"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：keys（开关 key 列表）必填")
		return
	}
	response.Success(ctx, gin.H{"results": c.svc.EvaluateBatch(ctx.Request.Context(), req.Keys, req.Attributes)}, "ok")
}

// @Summary      查询功能旗标变更审计日志
// @Tags      feature-flag
// @Router    /api/feature-flags/{id}/audit [get]
// Audit GET /api/feature-flags/:id/audit
func (c *FeatureFlagController) Audit(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	logs, err := c.svc.ListAudit(ctx.Request.Context(), id)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": logs, "total": len(logs)}, "ok")
}

// @Summary      查询功能旗标求值日志
// @Tags      feature-flag
// @Router    /api/feature-flags/{id}/eval-log [get]
// EvalLogs GET /api/feature-flags/:id/eval-log（兼容数字 ID 与 key）
func (c *FeatureFlagController) EvalLogs(ctx *gin.Context) {
	f, err := c.svc.GetByIDOrKey(ctx.Request.Context(), ctx.Param("id"))
	if HandleServiceError(ctx, err) {
		return
	}
	logs, err := c.svc.ListEvalLogs(ctx.Request.Context(), f.Key)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": logs, "total": len(logs)}, "ok")
}

// @Summary      查询长期未更新的过期旗标
// @Tags      feature-flag
// @Router    /api/feature-flags/stale [get]
// Stale GET /api/feature-flags/stale
func (c *FeatureFlagController) Stale(ctx *gin.Context) {
	list, err := c.svc.ListStale(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}
