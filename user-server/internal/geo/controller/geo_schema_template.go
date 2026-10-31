package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GeoSchemaTemplateController Schema 模板库 CRUD
type GeoSchemaTemplateController struct {
	svc *service.SchemaTemplateService
}

func NewGeoSchemaTemplateController(svc *service.SchemaTemplateService) *GeoSchemaTemplateController {
	return &GeoSchemaTemplateController{svc: svc}
}

// schemaTemplateRequest 只含可编辑字段；id/tenant_id/时间戳由系统维护。
type schemaTemplateRequest struct {
	PageType     *string        `json:"page_type"`
	SchemaType   *string        `json:"schema_type"`
	TemplateJSON datatypes.JSON `json:"template_json"`
	Active       *bool          `json:"active"`
}

func (r schemaTemplateRequest) toInput() service.SchemaTemplateInput {
	return service.SchemaTemplateInput{
		PageType:     r.PageType,
		SchemaType:   r.SchemaType,
		TemplateJSON: r.TemplateJSON,
		Active:       r.Active,
	}
}

// @Summary      查询 Schema 模板列表
// @Tags      geo-schema-template
// @Router    /api/geo/schema-templates [get]
func (c *GeoSchemaTemplateController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.List(ctx.Request.Context(), page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary GET /api/geo/schema-templates/{id}
// @Tags      geo-schema-template
// @Router    /api/geo/schema-templates/{id} [get]
func (c *GeoSchemaTemplateController) Get(ctx *gin.Context) {
	item, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "Schema 模板不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

// @Summary      创建 Schema 模板
// @Tags      geo-schema-template
// @Router    /api/geo/schema-templates [post]
func (c *GeoSchemaTemplateController) Create(ctx *gin.Context) {
	var req schemaTemplateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	item, err := c.svc.Create(ctx.Request.Context(), req.toInput())
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "created")
}

// @Summary PUT /api/geo/schema-templates/{id}
// @Tags      geo-schema-template
// @Router    /api/geo/schema-templates/{id} [put]
func (c *GeoSchemaTemplateController) Update(ctx *gin.Context) {
	var req schemaTemplateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	item, err := c.svc.Update(ctx.Request.Context(), ctx.Param("id"), req.toInput())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "Schema 模板不存在")
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "updated")
}

// @Summary DELETE /api/geo/schema-templates/{id}
// @Tags      geo-schema-template
// @Router    /api/geo/schema-templates/{id} [delete]
func (c *GeoSchemaTemplateController) Delete(ctx *gin.Context) {
	if err := c.svc.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "Schema 模板不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
