package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type LeadMiningNotifyRouteController struct {
	svc *service.LeadMiningNotifyRouteService
}

func NewLeadMiningNotifyRouteController() *LeadMiningNotifyRouteController {
	return &LeadMiningNotifyRouteController{svc: service.NewLeadMiningNotifyRouteService()}
}

func (ctrl *LeadMiningNotifyRouteController) RegisterRoutes(auth *gin.RouterGroup, admin *gin.RouterGroup) {
	auth.GET("/lead-mining/notify-routes", ctrl.List)
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
