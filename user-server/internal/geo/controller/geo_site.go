package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GeoSiteController 站点部署配置 CRUD
type GeoSiteController struct {
	db *gorm.DB
}

func NewGeoSiteController(db *gorm.DB) *GeoSiteController {
	return &GeoSiteController{db: db}
}

func (c *GeoSiteController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []model.GeoSite
	var total int64
	c.db.Model(&model.GeoSite{}).Count(&total)
	c.db.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list)
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

func (c *GeoSiteController) Get(ctx *gin.Context) {
	var item model.GeoSite
	if err := c.db.Where("id = ?", ctx.Param("id")).First(&item).Error; err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

func (c *GeoSiteController) Create(ctx *gin.Context) {
	var item model.GeoSite
	if !response.BindJSON(ctx, &item) {
		return
	}
	item.Active = true
	if err := c.db.Create(&item).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "created")
}

func (c *GeoSiteController) Update(ctx *gin.Context) {
	var item model.GeoSite
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

func (c *GeoSiteController) Delete(ctx *gin.Context) {
	if err := c.db.Delete(&model.GeoSite{}, "id = ?", ctx.Param("id")).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
