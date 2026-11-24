package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type BusinessAccountAuthController struct {
	svc *service.BusinessAccountAuthService
}

func NewBusinessAccountAuthController() *BusinessAccountAuthController {
	return &BusinessAccountAuthController{svc: service.NewBusinessAccountAuthService()}
}

func (ctrl *BusinessAccountAuthController) RegisterRoutes(r *gin.RouterGroup) {
	g := r.Group("/business-account-auths")
	{
		g.POST("", ctrl.Upsert)
		g.GET("", ctrl.List)
		g.DELETE("/:id", ctrl.Delete)
	}
}

func (ctrl *BusinessAccountAuthController) tenantID(c *gin.Context) uint {
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		return id
	}
	return 1
}

func (ctrl *BusinessAccountAuthController) actor(c *gin.Context) (uint, string) {
	var uid uint
	if v, ok := c.Get("user_id"); ok {
		switch n := v.(type) {
		case uint:
			uid = n
		case int:
			uid = uint(n)
		case int64:
			uid = uint(n)
		case float64:
			uid = uint(n)
		}
	}
	uname, _ := c.Get("username")
	username, _ := uname.(string)
	return uid, username
}

func (ctrl *BusinessAccountAuthController) recordOp(c *gin.Context, action, resourceID, detail string) {
	uid, uname := ctrl.actor(c)
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID: ctrl.tenantID(c), UserID: uid, Username: uname,
		Action: action, Module: "crm_business_account_auth", Resource: "business_account_authorization",
		ResourceID: resourceID, Detail: detail, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
}

func (ctrl *BusinessAccountAuthController) Upsert(c *gin.Context) {
	var req model.BusinessAccountAuthorization
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if tid, ok := tenant.FromContext(c.Request.Context()); ok {
		req.TenantID = tid
	}
	if err := ctrl.svc.Upsert(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "保存授权失败")
		return
	}
	ctrl.recordOp(c, "upsert", fmt.Sprintf("%d", req.ID), fmt.Sprintf("user=%d channel=%s account=%s scope=%s", req.UserID, req.Channel, req.AccountID, req.Scope))
	response.Success(c, req, "保存成功")
}

func (ctrl *BusinessAccountAuthController) List(c *gin.Context) {
	userID, _ := strconv.ParseUint(c.Query("user_id"), 10, 64)
	var (
		items []model.BusinessAccountAuthorization
		err   error
	)
	if userID > 0 {
		items, err = ctrl.svc.ListByUser(c.Request.Context(), uint(userID))
	} else {
		items, err = ctrl.svc.ListByTenant(c.Request.Context())
	}
	if err != nil {
		response.ErrorFromDB(c, err, "获取授权列表失败")
		return
	}
	response.SuccessWithList(c, items, int64(len(items)))
}

func (ctrl *BusinessAccountAuthController) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "id 无效")
		return
	}
	if err := ctrl.svc.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFromDB(c, err, "删除授权失败")
		return
	}
	ctrl.recordOp(c, "delete", fmt.Sprintf("%d", id), "删除业务账号授权")
	response.Success(c, gin.H{"id": id}, "删除成功")
}
