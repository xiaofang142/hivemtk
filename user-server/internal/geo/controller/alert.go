package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AlertController GEO 告警中心控制器
type AlertController struct {
	svc *service.AlertService
}

// NewAlertController 创建告警控制器
func NewAlertController(svc *service.AlertService) *AlertController {
	return &AlertController{svc: svc}
}

// List GET /geo/alerts?type=&level=&page=&limit=
func (c *AlertController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	result, err := c.svc.ListAlerts(ctx.Request.Context(), service.AlertQuery{
		Type:  ctx.Query("type"),
		Level: ctx.Query("level"),
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询告警失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// UnreadCount GET /geo/alerts/unread-count
func (c *AlertController) UnreadCount(ctx *gin.Context) {
	n, err := c.svc.CountUnread(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询未读告警数失败")
		return
	}
	response.Success(ctx, gin.H{"count": n}, "ok")
}

// MarkNotified POST /geo/alerts/:id/ack 确认（已读）
func (c *AlertController) MarkNotified(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.MarkNotified(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "确认告警失败")
		return
	}
	response.Success(ctx, nil, "已确认")
}

// Delete DELETE /geo/alerts/:id
// @Summary      删除 GEO 告警
// @Description  按告警 ID 删除 GEO 告警中心里的一条记录
// @Tags         GEO-告警中心
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "告警 ID"
// @Success      200  {object}  response.Response
// @Failure      404  {object}  response.Response  "告警不存在"
// @Router       /api/geo/alerts/{id} [delete]
func (c *AlertController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.DeleteAlert(ctx.Request.Context(), uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "告警不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "删除告警失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
