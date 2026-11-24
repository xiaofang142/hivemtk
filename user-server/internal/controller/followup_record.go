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

type FollowupRecordController struct {
	svc *service.FollowupRecordService
}

func NewFollowupRecordController() *FollowupRecordController {
	return &FollowupRecordController{svc: service.NewFollowupRecordService()}
}

func (ctrl *FollowupRecordController) RegisterRoutes(r *gin.RouterGroup) {
	g := r.Group("/followup-records")
	{
		g.POST("", ctrl.Create)
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.PUT("/:id", ctrl.Update)
	}
}

func (ctrl *FollowupRecordController) tenantID(c *gin.Context) uint {
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		return id
	}
	return 1
}

func (ctrl *FollowupRecordController) actor(c *gin.Context) (uint, string) {
	var uid uint
	if v, ok := c.Get("user_id"); ok {
		switch n := v.(type) {
		case uint:
			uid = n
		case int:
			uid = uint(n)
		case int64:
			uid = uint(n)
		}
	}
	uname, _ := c.Get("username")
	username, _ := uname.(string)
	return uid, username
}

func (ctrl *FollowupRecordController) recordOp(c *gin.Context, action, resourceID, detail string) {
	uid, uname := ctrl.actor(c)
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID: ctrl.tenantID(c), UserID: uid, Username: uname,
		Action: action, Module: "crm_followup", Resource: "followup_record",
		ResourceID: resourceID, Detail: detail, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
}

func (ctrl *FollowupRecordController) Create(c *gin.Context) {
	var req model.FollowupRecord
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.TenantID = ctrl.tenantID(c)
	uid, _ := ctrl.actor(c)
	if req.OwnerID == 0 {
		req.OwnerID = uid
	}
	if req.CreatedBy == nil && uid > 0 {
		req.CreatedBy = &uid
	}
	if err := ctrl.svc.Create(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "创建跟进记录失败")
		return
	}
	ctrl.recordOp(c, "create", fmt.Sprintf("%s", req.RecordID), "创建跟进记录")
	response.Success(c, req, "创建成功")
}

func (ctrl *FollowupRecordController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	customerID := c.Query("customer_id")
	reminderID := c.Query("reminder_id")
	ownerID, _ := strconv.ParseUint(c.Query("owner_id"), 10, 64)
	items, total, err := ctrl.svc.List(c.Request.Context(), customerID, reminderID, uint(ownerID), page, pageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "获取跟进记录失败")
		return
	}
	response.SuccessWithPage(c, items, int64(page), int64(pageSize), total)
}

func (ctrl *FollowupRecordController) Get(c *gin.Context) {
	rec, err := ctrl.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFromDB(c, err, "获取跟进记录失败")
		return
	}
	response.Success(c, rec, "ok")
}

func (ctrl *FollowupRecordController) Update(c *gin.Context) {
	var req model.FollowupRecord
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.RecordID = c.Param("id")
	req.TenantID = ctrl.tenantID(c)
	uid, _ := ctrl.actor(c)
	if uid > 0 {
		req.UpdatedBy = &uid
	}
	if err := ctrl.svc.Update(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "更新跟进记录失败")
		return
	}
	ctrl.recordOp(c, "update", req.RecordID, "更新跟进记录")
	response.Success(c, req, "更新成功")
}
