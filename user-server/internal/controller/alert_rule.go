package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// AlertRuleController 告警规则控制器
type AlertRuleController struct {
	svc *service.AlertRuleService
}

// NewAlertRuleController 构造
func NewAlertRuleController() *AlertRuleController {
	return &AlertRuleController{svc: service.NewAlertRuleService()}
}

// Create godoc
// @Summary      创建告警规则
// @Tags         告警
// @Accept       json
// @Produce      json
// @Param        body  body  service.AlertRuleRequest  true  "规则"
// @Success      201   {object}  response.Response
// @Router       /api/alerts/rules [post]
func (c *AlertRuleController) Create(ctx *gin.Context) {
	var req service.AlertRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}
	creatorID, _ := ctx.Get("user_id")
	uid, _ := toUint(creatorID)
	rule, err := c.svc.Create(ctx.Request.Context(), &req, uid)
	if err != nil {
		if strings.Contains(err.Error(), "不合法") || strings.Contains(err.Error(), "不支持") {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, rule, "创建成功")
}

// Update godoc
// @Summary      更新告警规则
// @Tags         告警
// @Accept       json
// @Produce      json
// @Param        id    path  int                       true  "规则ID"
// @Param        body  body  service.AlertRuleRequest  true  "规则"
// @Success      200   {object}  response.Response
// @Router       /api/alerts/rules/{id} [put]
func (c *AlertRuleController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "id 非法")
		return
	}
	var req service.AlertRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}
	rule, err := c.svc.Update(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		if err == service.ErrAlertRuleNotFound {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "不合法") || strings.Contains(err.Error(), "不支持") {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, rule, "更新成功")
}

// Delete godoc
// @Summary      删除告警规则
// @Tags         告警
// @Produce      json
// @Param        id  path  int  true  "规则ID"
// @Success      200  {object}  response.Response
// @Router       /api/alerts/rules/{id} [delete]
func (c *AlertRuleController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "id 非法")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		if err == service.ErrAlertRuleNotFound {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// GetByID godoc
// @Summary      获取告警规则详情
// @Tags         告警
// @Produce      json
// @Param        id  path  int  true  "规则ID"
// @Success      200  {object}  response.Response
// @Router       /api/alerts/rules/{id} [get]
func (c *AlertRuleController) GetByID(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "id 非法")
		return
	}
	rule, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, rule, "")
}

// List godoc
// @Summary      告警规则列表
// @Tags         告警
// @Produce      json
// @Param        page          query  int   false  "页码"  default(1)
// @Param        size          query  int   false  "每页"  default(20)
// @Param        enabled_only  query  bool  false  "仅启用"
// @Success      200  {object}  response.Response
// @Router       /api/alerts/rules [get]
func (c *AlertRuleController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(ctx.DefaultQuery("size", "20"))
	enabledOnly := ctx.Query("enabled_only") == "true"
	list, total, err := c.svc.List(ctx.Request.Context(), page, size, enabledOnly)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "")
}

// SetStatus godoc
// @Summary      批量启用/禁用规则
// @Tags         告警
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{ids:[1,2], enabled:true}"
// @Success      200  {object}  response.Response
// @Router       /api/alerts/rules/status [put]
func (c *AlertRuleController) SetStatus(ctx *gin.Context) {
	var req struct {
		IDs     []uint `json:"ids"`
		Enabled bool   `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}
	if len(req.IDs) == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "ids 不能为空")
		return
	}
	if err := c.svc.SetStatus(ctx.Request.Context(), req.IDs, req.Enabled); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "操作成功")
}

// ListHistory godoc
// @Summary      告警历史
// @Tags         告警
// @Produce      json
// @Param        rule_id  query  int     false  "规则ID"
// @Param        source   query  string  false  "来源"
// @Param        page     query  int     false  "页码"  default(1)
// @Param        size     query  int     false  "每页"  default(20)
// @Success      200  {object}  response.Response
// @Router       /api/alerts/histories [get]
func (c *AlertRuleController) ListHistory(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(ctx.DefaultQuery("size", "20"))
	ruleID, _ := strconv.ParseUint(ctx.Query("rule_id"), 10, 64)
	source := ctx.Query("source")
	list, total, err := c.svc.ListHistory(ctx.Request.Context(), page, size, uint(ruleID), source)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "")
}

// ResolveHistory godoc
// @Summary      手动恢复告警历史
// @Tags         告警
// @Produce      json
// @Param        rule_id  query  int  true  "规则ID"
// @Success      200  {object}  response.Response
// @Router       /api/alerts/histories/resolve [post]
func (c *AlertRuleController) ResolveHistory(ctx *gin.Context) {
	ruleID, err := strconv.ParseUint(ctx.Query("rule_id"), 10, 64)
	if err != nil || ruleID == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "rule_id 非法")
		return
	}
	if err := c.svc.ResolveHistory(ctx.Request.Context(), uint(ruleID)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "已标记为恢复")
}

// Unread godoc
// @Summary      未恢复告警概览
// @Description  返回 firing 状态告警计数与最近列表（OpsOverview 顶栏未读角标）
// @Tags         告警
// @Produce      json
// @Param        limit  query  int  false  "列表条数上限"  default(20)
// @Success      200    {object}  response.Response
// @Router       /api/monitor/alerts/unread [get]
func (c *AlertRuleController) Unread(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	out, err := c.svc.GetUnread(ctx.Request.Context(), limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"count":        out.Count,
		"unread_count": out.Count,
		"list":         out.List,
	}, "")
}

func toUint(v any) (uint, bool) {
	switch t := v.(type) {
	case uint:
		return t, true
	case uint64:
		return uint(t), true
	case float64:
		return uint(t), true
	case int:
		return uint(t), true
	}
	return 0, false
}
