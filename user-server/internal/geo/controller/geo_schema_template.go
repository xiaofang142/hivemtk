package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GeoSchemaTemplateController Schema 模板库 CRUD
type GeoSchemaTemplateController struct {
	db *gorm.DB
}

func NewGeoSchemaTemplateController(db *gorm.DB) *GeoSchemaTemplateController {
	return &GeoSchemaTemplateController{db: db}
}

func (c *GeoSchemaTemplateController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []model.GeoSchemaTemplate
	var total int64
	c.db.Model(&model.GeoSchemaTemplate{}).Count(&total)
	c.db.Order("page_type ASC, schema_type ASC").Offset((page - 1) * limit).Limit(limit).Find(&list)
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

func (c *GeoSchemaTemplateController) Get(ctx *gin.Context) {
	var item model.GeoSchemaTemplate
	if err := c.db.Where("id = ?", ctx.Param("id")).First(&item).Error; err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

func (c *GeoSchemaTemplateController) Create(ctx *gin.Context) {
	var item model.GeoSchemaTemplate
	if !response.BindJSON(ctx, &item) {
		return
	}
	if item.PageType == "" {
		response.Error(ctx, http.StatusBadRequest, "page_type required")
		return
	}
	if item.SchemaType == "" {
		response.Error(ctx, http.StatusBadRequest, "schema_type required")
		return
	}
	if err := c.db.Create(&item).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "created")
}

func (c *GeoSchemaTemplateController) Update(ctx *gin.Context) {
	var item model.GeoSchemaTemplate
	if err := c.db.Where("id = ?", ctx.Param("id")).First(&item).Error; err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	if !response.BindJSON(ctx, &item) {
		return
	}
	if err := c.db.Save(&item).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "updated")
}

func (c *GeoSchemaTemplateController) Delete(ctx *gin.Context) {
	if err := c.db.Delete(&model.GeoSchemaTemplate{}, "id = ?", ctx.Param("id")).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
