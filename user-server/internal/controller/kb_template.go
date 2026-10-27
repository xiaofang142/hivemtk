// kb_template_controller.go 知识库模板控制器（R39-K7）。
package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type KbTemplateController struct {
	svc *service.KbTemplateService
}

func NewKbTemplateController(svc *service.KbTemplateService) *KbTemplateController {
	return &KbTemplateController{svc: svc}
}

// ListKbTemplates GET /api/kb-templates?category=&status=
// @Summary      查询知识库模板列表
// @Tags      kb-templates
// @Router    /api/kb-templates [get]
func (c *KbTemplateController) ListKbTemplates(ctx *gin.Context) {
	list, err := c.svc.List(ctx.Request.Context(), ctx.Query("category"), ctx.Query("status"))
	if err != nil {
		errhttp.HandleDBError(ctx, err, "查询知识库模板")
		return
	}
	response.Success(ctx, gin.H{"list": list}, "获取成功")
}

// GetKbTemplate GET /api/kb-templates/:id
// @Summary      查询知识库模板详情
// @Tags      kb-templates
// @Router    /api/kb-templates/{id} [get]
func (c *KbTemplateController) GetKbTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	tpl, err := c.svc.GetByID(ctx.Request.Context(), id)
	if errhttp.HandleDBError(ctx, err, "查询知识库模板") {
		return
	}
	response.Success(ctx, tpl, "获取成功")
}

// CreateKbTemplate POST /api/kb-templates
// @Summary      创建知识库模板
// @Tags      kb-templates
// @Router    /api/kb-templates [post]
func (c *KbTemplateController) CreateKbTemplate(ctx *gin.Context) {
	var req service.KbTemplateUpsertReq
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

// UpdateKbTemplate PUT /api/kb-templates/:id
// @Summary      更新知识库模板
// @Tags      kb-templates
// @Router    /api/kb-templates/{id} [put]
func (c *KbTemplateController) UpdateKbTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.KbTemplateUpsertReq
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

// DeleteKbTemplate DELETE /api/kb-templates/:id
// @Summary      删除知识库模板
// @Tags      kb-templates
// @Router    /api/kb-templates/{id} [delete]
func (c *KbTemplateController) DeleteKbTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if HandleServiceError(ctx, c.svc.Delete(ctx.Request.Context(), id)) {
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// ApplyKbTemplate POST /api/kb-templates/:id/apply —— 一键建 KB（新用户主路径）。
// @Summary      一键从模板创建知识库
// @Tags      kb-templates
// @Router    /api/kb-templates/{id}/apply [post]
func (c *KbTemplateController) ApplyKbTemplate(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req service.KbTemplateApplyReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// apply 的请求体全字段可缺省：空 body 直接走模板派生，不算参数错。
		req = service.KbTemplateApplyReq{}
	}
	kb, err := c.svc.Apply(ctx.Request.Context(), id, &req)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, kb, "知识库已创建")
}

// RateKbTemplate POST /api/kb-templates/:id/rate {score: 1-5}
// @Summary      知识库模板评分
// @Tags      kb-templates
// @Router    /api/kb-templates/{id}/rate [post]
func (c *KbTemplateController) RateKbTemplate(ctx *gin.Context) {
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
