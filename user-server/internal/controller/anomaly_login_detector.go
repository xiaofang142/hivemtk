package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// AnomalyLoginDetectorController 异常登录预警控制器
type AnomalyLoginDetectorController struct {
	detector *service.AnomalyLoginDetector
}

// NewAnomalyLoginDetectorController 创建异常登录预警控制器
func NewAnomalyLoginDetectorController() *AnomalyLoginDetectorController {
	return &AnomalyLoginDetectorController{
		detector: service.NewAnomalyLoginDetector(),
	}
}

// ListLoginEvents 查询登录事件
// @Summary 查询登录事件列表
// @Description 返回当前用户的登录事件（含风险评估结果），按时间倒序
// @Tags A域-认证安全
// @Accept json
// @Produce json
// @Param page query int false "页码（默认1）"
// @Param page_size query int false "每页大小（默认20，最大100）"
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/auth/anomaly/login-events [get]
func (c *AnomalyLoginDetectorController) ListLoginEvents(ctx *gin.Context) {
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	events, total, err := c.detector.ListLoginEvents(ctx.Request.Context(), uid, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      events,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "查询成功")
}

// ListAlerts 查询安全告警
// @Summary 查询安全告警列表
// @Description 返回当前用户的安全告警，支持按 status 过滤
// @Tags A域-认证安全
// @Accept json
// @Produce json
// @Param status query string false "状态过滤：open/resolved/ignored"
// @Param page query int false "页码（默认1）"
// @Param page_size query int false "每页大小（默认20）"
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/auth/anomaly/alerts [get]
func (c *AnomalyLoginDetectorController) ListAlerts(ctx *gin.Context) {
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	status := ctx.Query("status")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	alerts, total, err := c.detector.ListAlerts(ctx.Request.Context(), uid, status, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      alerts,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "查询成功")
}

// ResolveAlert 处理告警
// @Summary 处理（标记已解决）安全告警
// @Description 将指定告警标记为 resolved，并写入审计日志
// @Tags A域-认证安全
// @Accept json
// @Produce json
// @Param id path int true "告警 ID"
// @Param body body object{note=string} false "处理说明"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Router /api/auth/anomaly/alerts/{id}/resolve [post]
func (c *AnomalyLoginDetectorController) ResolveAlert(ctx *gin.Context) {
	alertIDStr := ctx.Param("id")
	alertID, err := strconv.ParseUint(alertIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的告警 ID")
		return
	}
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	var body struct {
		Note string `json:"note"`
	}
	_ = ctx.ShouldBindJSON(&body)

	if err := c.detector.ResolveAlert(ctx, uint(alertID), uid, body.Note); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "告警已处理")
}

// IgnoreAlert 忽略告警
// @Summary 忽略安全告警
// @Description 将指定告警标记为 ignored，写入审计日志
// @Tags A域-认证安全
// @Accept json
// @Produce json
// @Param id path int true "告警 ID"
// @Param body body object{note=string} false "忽略说明"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Router /api/auth/anomaly/alerts/{id}/ignore [post]
func (c *AnomalyLoginDetectorController) IgnoreAlert(ctx *gin.Context) {
	alertIDStr := ctx.Param("id")
	alertID, err := strconv.ParseUint(alertIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的告警 ID")
		return
	}
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	var body struct {
		Note string `json:"note"`
	}
	_ = ctx.ShouldBindJSON(&body)

	if err := c.detector.IgnoreAlert(ctx, uint(alertID), uid, body.Note); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "告警已忽略")
}
