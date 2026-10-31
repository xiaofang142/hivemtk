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

// GeoPusherConfigController 蜘蛛平台配置 CRUD
type GeoPusherConfigController struct {
	svc *service.PusherConfigService
}

func NewGeoPusherConfigController(svc *service.PusherConfigService) *GeoPusherConfigController {
	return &GeoPusherConfigController{svc: svc}
}

// pusherConfigRequest 只含可编辑字段；id/tenant_id/used_today/last_reset_at 由系统维护。
type pusherConfigRequest struct {
	Platform   *string        `json:"platform"`
	ConfigJSON datatypes.JSON `json:"config_json"`
	DailyLimit *int           `json:"daily_limit"`
	Active     *bool          `json:"active"`
}

func (r pusherConfigRequest) toInput() service.PusherConfigInput {
	return service.PusherConfigInput{
		Platform:   r.Platform,
		ConfigJSON: r.ConfigJSON,
		DailyLimit: r.DailyLimit,
		Active:     r.Active,
	}
}

// @Summary      查询 GEO 推送平台配置列表
// @Tags      geo-pusher-config
// @Router    /api/geo/pushers [get]
func (c *GeoPusherConfigController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.List(ctx.Request.Context(), page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary GET /api/geo/pushers/{id}
// @Tags      geo-pusher-config
// @Router    /api/geo/pushers/{id} [get]
func (c *GeoPusherConfigController) Get(ctx *gin.Context) {
	item, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "推送平台配置不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

// @Summary      新增 GEO 推送平台配置
// @Tags      geo-pusher-config
// @Router    /api/geo/pushers [post]
func (c *GeoPusherConfigController) Create(ctx *gin.Context) {
	var req pusherConfigRequest
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

// @Summary PUT /api/geo/pushers/{id}
// @Tags      geo-pusher-config
// @Router    /api/geo/pushers/{id} [put]
func (c *GeoPusherConfigController) Update(ctx *gin.Context) {
	var req pusherConfigRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	item, err := c.svc.Update(ctx.Request.Context(), ctx.Param("id"), req.toInput())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "推送平台配置不存在")
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "updated")
}

// @Summary DELETE /api/geo/pushers/{id}
// @Tags      geo-pusher-config
// @Router    /api/geo/pushers/{id} [delete]
func (c *GeoPusherConfigController) Delete(ctx *gin.Context) {
	if err := c.svc.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "推送平台配置不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
