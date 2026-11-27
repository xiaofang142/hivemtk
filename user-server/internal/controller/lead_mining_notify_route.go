package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type LeadMiningNotifyRouteController struct {
	svc    *service.LeadMiningNotifyRouteService
	notify *service.LeadNotifyService
}

func NewLeadMiningNotifyRouteController() *LeadMiningNotifyRouteController {
	return &LeadMiningNotifyRouteController{
		svc:    service.NewLeadMiningNotifyRouteService(),
		notify: service.GlobalLeadNotifyService(),
	}
}

func (ctrl *LeadMiningNotifyRouteController) RegisterRoutes(auth *gin.RouterGroup, admin *gin.RouterGroup) {
	auth.GET("/lead-mining/notify-routes", ctrl.List)
	auth.GET("/lead-mining/notify-deliveries", ctrl.ListDeliveries)
	auth.GET("/lead-mining/notify-deliveries/summary", ctrl.SummarizeDeliveries)
	admin.POST("/notify-routes", ctrl.Create)
	admin.DELETE("/notify-routes/:route_id", ctrl.Delete)

	// LM-PR2 minimal clean hook: clue status/merge/convert actions registered here
	// because content_routes.go is dirty in this workspace.
	NewLeadMiningClueActionController().RegisterRoutes(auth, admin)
}

func (ctrl *LeadMiningNotifyRouteController) List(c *gin.Context) {
	items, err := ctrl.svc.List(c.Request.Context(), c.Query("channel"), c.Query("account_id"))
	if err != nil {
		response.ErrorFromDB(c, err, "获取通知路由失败")
		return
	}
	response.Success(c, items, "ok")
}

func (ctrl *LeadMiningNotifyRouteController) ListDeliveries(c *gin.Context) {
	if ctrl.notify == nil {
		response.Error(c, http.StatusServiceUnavailable, "通知服务未装配")
		return
	}
	query, ok := leadNotifyDeliveryQuery(c)
	if !ok {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	items, err := ctrl.notify.ListDeliveries(c.Request.Context(), query)
	if err != nil {
		response.ErrorFromDB(c, err, "获取通知送达历史失败")
		return
	}
	response.Success(c, items, "ok")
}

func (ctrl *LeadMiningNotifyRouteController) SummarizeDeliveries(c *gin.Context) {
	if ctrl.notify == nil {
		response.Error(c, http.StatusServiceUnavailable, "通知服务未装配")
		return
	}
	query, ok := leadNotifyDeliveryQuery(c)
	if !ok {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	items, err := ctrl.notify.SummarizeDeliveries(c.Request.Context(), query)
	if err != nil {
		response.ErrorFromDB(c, err, "获取通知送达汇总失败")
		return
	}
	response.Success(c, items, "ok")
}

func leadNotifyDeliveryQuery(c *gin.Context) (model.LeadNotifyDeliveryQuery, bool) {
	query := model.LeadNotifyDeliveryQuery{
		ClueID:  strings.TrimSpace(c.Query("clue_id")),
		EventID: strings.TrimSpace(c.Query("event_id")),
		RouteID: strings.TrimSpace(c.Query("route_id")),
		Channel: strings.TrimSpace(c.Query("channel")),
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return model.LeadNotifyDeliveryQuery{}, false
		}
		query.Limit = limit
	}
	return query, true
}

func (ctrl *LeadMiningNotifyRouteController) Create(c *gin.Context) {
	var req model.LeadMiningNotifyRoute
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if tid, ok := tenant.FromContext(c.Request.Context()); ok {
		req.TenantID = tid
	}
	if v, ok := c.Get("user_id"); ok {
		if uid, ok2 := v.(uint); ok2 {
			req.CreatedBy = &uid
		}
	}
	if err := ctrl.svc.Create(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "创建通知路由失败")
		return
	}
	response.Success(c, req, "创建成功")
}

func (ctrl *LeadMiningNotifyRouteController) Delete(c *gin.Context) {
	if err := ctrl.svc.Delete(c.Request.Context(), c.Param("route_id")); err != nil {
		response.ErrorFromDB(c, err, "删除通知路由失败")
		return
	}
	response.Success(c, gin.H{"route_id": c.Param("route_id")}, "删除成功")
}
