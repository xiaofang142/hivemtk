// dashboard_template_controller.go 数据大屏模板控制器（R39-K8）。
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type DashboardTemplateController struct {
	svc *service.DashboardTemplateService
}

func NewDashboardTemplateController(svc *service.DashboardTemplateService) *DashboardTemplateController {
	return &DashboardTemplateController{svc: svc}
}

// ListDashboardTemplates GET /api/dashboard-templates?status=
// @Summary      查询大屏模板列表
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates [get]
func (c *DashboardTemplateController) ListDashboardTemplates(ctx *gin.Context) {
	list, err := c.svc.List(ctx.Request.Context(), ctx.Query("status"))
	if err != nil {
		errhttp.HandleDBError(ctx, err, "查询大屏模板")
		return
	}
	response.Success(ctx, gin.H{"list": list}, "获取成功")
}

// GetDashboardTemplate GET /api/dashboard-templates/:id
// @Summary      查询大屏模板详情
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id} [get]
func (c *DashboardTemplateController) GetDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	tpl, err := c.svc.GetByID(ctx.Request.Context(), id)
	if errhttp.HandleDBError(ctx, err, "查询大屏模板") {
		return
	}
	response.Success(ctx, tpl, "获取成功")
}

// CreateDashboardTemplate POST /api/dashboard-templates（手工建 draft 模板）
// @Summary      创建大屏模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates [post]
func (c *DashboardTemplateController) CreateDashboardTemplate(ctx *gin.Context) {
	var req service.DashboardTemplateUpsertReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)
	tpl, err := c.svc.Create(ctx.Request.Context(), uid, &req)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, tpl, "创建成功")
}

// UpdateDashboardTemplate PUT /api/dashboard-templates/:id
// @Summary      更新大屏模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id} [put]
func (c *DashboardTemplateController) UpdateDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.DashboardTemplateUpsertReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	tpl, err := c.svc.Update(ctx.Request.Context(), id, &req)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, tpl, "更新成功")
}

// DeleteDashboardTemplate DELETE /api/dashboard-templates/:id
// @Summary      删除大屏模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id} [delete]
func (c *DashboardTemplateController) DeleteDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if HandleServiceError(ctx, c.svc.Delete(ctx.Request.Context(), id)) {
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// PublishDashboardTemplate POST /api/dashboard-templates/:id/publish（draft → published）
// @Summary      发布大屏模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id}/publish [post]
func (c *DashboardTemplateController) PublishDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if HandleServiceError(ctx, c.svc.Publish(ctx.Request.Context(), id)) {
		return
	}
	response.Success(ctx, nil, "发布成功")
}

// PublishScreenAsTemplate POST /api/dashboard-templates/publish-from-screen
// {screen_id, name, description} —— 从既有大屏发布模板（发布时刻快照）。
// @Summary      从既有大屏发布模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/publish-from-screen [post]
func (c *DashboardTemplateController) PublishScreenAsTemplate(ctx *gin.Context) {
	var req struct {
		ScreenID    uint   `json:"screen_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)
	tpl, err := c.svc.PublishFromScreen(ctx.Request.Context(), req.ScreenID, req.Name, req.Description, uid)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, tpl, "模板已发布")
}

// CloneDashboardTemplate POST /api/dashboard-templates/:id/clone {name, is_public}
// —— 一键复制成真实大屏。
// @Summary      一键克隆大屏模板
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id}/clone [post]
func (c *DashboardTemplateController) CloneDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.DashboardTemplateCloneReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)
	screen, err := c.svc.Clone(ctx.Request.Context(), id, uid, &req)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, screen, "大屏已创建")
}

// RateDashboardTemplate POST /api/dashboard-templates/:id/rate {score: 1-5}
// @Summary      大屏模板评分
// @Tags      dashboard-templates
// @Router    /api/dashboard-templates/{id}/rate [post]
func (c *DashboardTemplateController) RateDashboardTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		Score float64 `json:"score" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：score 必填")
		return
	}
	rating, err := c.svc.Rate(ctx.Request.Context(), id, req.Score)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"template_id": id, "rating": rating}, "评分成功")
}
