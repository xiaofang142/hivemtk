package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GeoPusherConfigController 蜘蛛平台配置 CRUD
type GeoPusherConfigController struct {
	db *gorm.DB
}

func NewGeoPusherConfigController(db *gorm.DB) *GeoPusherConfigController {
	return &GeoPusherConfigController{db: db}
}

func (c *GeoPusherConfigController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []model.GeoPusherConfig
	var total int64
	c.db.Model(&model.GeoPusherConfig{}).Count(&total)
	c.db.Order("platform ASC").Offset((page - 1) * limit).Limit(limit).Find(&list)
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

func (c *GeoPusherConfigController) Get(ctx *gin.Context) {
	var item model.GeoPusherConfig
	if err := c.db.Where("id = ?", ctx.Param("id")).First(&item).Error; err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

func (c *GeoPusherConfigController) Create(ctx *gin.Context) {
	var item model.GeoPusherConfig
	if !response.BindJSON(ctx, &item) {
		return
	}
	if err := c.db.Create(&item).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "created")
}

func (c *GeoPusherConfigController) Update(ctx *gin.Context) {
	var item model.GeoPusherConfig
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

func (c *GeoPusherConfigController) Delete(ctx *gin.Context) {
	if err := c.db.Delete(&model.GeoPusherConfig{}, "id = ?", ctx.Param("id")).Error; err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
