package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// NotificationController 通知中心控制器（商户端站内通知）
type NotificationController struct {
	svc *service.NotificationService
}

// NewNotificationController 构造控制器
func NewNotificationController(svc *service.NotificationService) *NotificationController {
	_ = svc.SeedIfEmpty(context.Background())
	return &NotificationController{svc: svc}
}

// @Summary      拉取通知列表
// @Tags      notification
// @Router    /api/auth/notifications [get]
// List 拉取通知列表
// GET /api/auth/notifications
// Query: page, page_size, type, is_read, keyword
func (ctrl *NotificationController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	req := service.NotificationListRequest{
		Page:  page,
		Size:  pageSize,
		Type:  c.Query("type"),
		Level: c.Query("level"),
	}

	if v, ok := c.Get("user_id"); ok {
		if uid, ok := v.(uint); ok {
			req.UserID = uid
		}
	}

	if isReadStr := c.Query("is_read"); isReadStr != "" {
		v, err := strconv.ParseBool(isReadStr)
		if err == nil {
			req.IsRead = &v
		}
	}
	req.Keyword = c.Query("keyword")

	resp, err := ctrl.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	response.Success(c, resp, "ok")
}

// @Summary      已标记为已读
// @Tags      notification
// @Router    /api/auth/notifications/{id}/read [post]
// MarkRead 标记单条已读
// POST /api/auth/notifications/:id/read
func (ctrl *NotificationController) MarkRead(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的 id")
		return
	}
	uid := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if u, ok := v.(uint); ok {
			uid = u
		}
	}
	if err := ctrl.svc.MarkRead(c.Request.Context(), uid, uint(id)); err != nil {
		if errors.Is(err, service.ErrNotificationNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil, "已标记为已读")
}

// @Summary      已全部标记为已读
// @Tags      notification
// @Router    /api/auth/notifications/read-all [post]
// MarkAllRead 全部标记已读
// POST /api/auth/notifications/read-all
func (ctrl *NotificationController) MarkAllRead(c *gin.Context) {
	uid := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if u, ok := v.(uint); ok {
			uid = u
		}
	}
	n, err := ctrl.svc.MarkAllRead(c.Request.Context(), uid)
	if err != nil {
		response.ErrorFromDB(c, err, "操作失败", err.Error())
		return
	}
	response.Success(c, gin.H{"marked": n}, "已全部标记为已读")
}

// @Summary      未读数（顶部铃铛 badge）
// @Tags      notification
// @Router    /api/auth/notifications/unread-count [get]
// UnreadCount 未读数（顶部铃铛 badge）
// GET /api/auth/notifications/unread-count
func (ctrl *NotificationController) UnreadCount(c *gin.Context) {
	uid := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if u, ok := v.(uint); ok {
			uid = u
		}
	}
	count, err := ctrl.svc.CountUnread(c.Request.Context(), uid)
	if err != nil {
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	response.Success(c, gin.H{"count": count}, "ok")
}
