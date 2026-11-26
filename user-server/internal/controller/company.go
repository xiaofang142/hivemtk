package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type CompanyController struct {
	svc *service.CompanyService
}

func NewCompanyController(svc *service.CompanyService) *CompanyController {
	return &CompanyController{svc: svc}
}

func (ctrl *CompanyController) tenantID(c *gin.Context) uint {
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		return id
	}
	return 1
}

func (ctrl *CompanyController) actor(c *gin.Context) (uint, string) {
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
	username, _ := c.Get("username")
	uname, _ := username.(string)
	return uid, uname
}

func (ctrl *CompanyController) recordOp(c *gin.Context, action, resourceID, detail string) {
	uid, uname := ctrl.actor(c)
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID:   ctrl.tenantID(c),
		UserID:     uid,
		Username:   uname,
		Action:     action,
		Module:     "crm_company",
		Resource:   "company",
		ResourceID: resourceID,
		Detail:     detail,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}

// companyScopeError 把客户数据权限拒绝映射为 403，避免落到通用 500。
func (ctrl *CompanyController) companyScopeError(c *gin.Context, err error, fallback string) bool {
	if errors.Is(err, service.ErrCRMCustomerForbidden) {
		response.Error(c, http.StatusForbidden, "无权访问该客户或公司")
		return true
	}
	return false
}

func (ctrl *CompanyController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/companies")
	{
		g.POST("", ctrl.Create)
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.GET("/:id/relations", ctrl.ListCompanyRelations)
	}
	cr := router.Group("/customers")
	{
		cr.GET("/:id/company-relations", ctrl.ListCustomerRelations)
		cr.POST("/:id/company-relations", ctrl.UpsertCustomerRelation)
	}
}

func (ctrl *CompanyController) Create(c *gin.Context) {
	var req model.Company
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.TenantID = ctrl.tenantID(c)
	uid, _ := ctrl.actor(c)
	if req.OwnerUserID == nil || *req.OwnerUserID == 0 {
		req.OwnerUserID = &uid
	}
	if req.CreatedBy == nil {
		req.CreatedBy = &uid
	}
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	if err := ctrl.svc.CreateScoped(c.Request.Context(), scope, &req); err != nil {
		if ctrl.companyScopeError(c, err, "创建公司失败") {
			return
		}
		response.ErrorFromDB(c, err, "创建公司失败")
		return
	}
	ctrl.recordOp(c, "create", strconv.FormatInt(req.ID, 10), "创建公司")
	response.Success(c, req, "创建成功")
}

func (ctrl *CompanyController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	items, total, err := ctrl.svc.ListScoped(c.Request.Context(), scope, keyword, page, pageSize)
	if err != nil {
		if ctrl.companyScopeError(c, err, "获取公司列表失败") {
			return
		}
		response.ErrorFromDB(c, err, "获取公司列表失败")
		return
	}
	response.SuccessWithPage(c, items, int64(page), int64(pageSize), total)
}

func (ctrl *CompanyController) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "id 无效")
		return
	}
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	item, err := ctrl.svc.GetByIDScoped(c.Request.Context(), scope, id)
	if err != nil {
		if ctrl.companyScopeError(c, err, "获取公司失败") {
			return
		}
		response.ErrorFromDB(c, err, "获取公司失败")
		return
	}
	response.Success(c, item, "ok")
}

func (ctrl *CompanyController) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "id 无效")
		return
	}
	var req model.Company
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.ID = id
	req.TenantID = ctrl.tenantID(c)
	uid, _ := ctrl.actor(c)
	if uid > 0 {
		req.UpdatedBy = &uid
	}
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	if err := ctrl.svc.UpdateScoped(c.Request.Context(), scope, &req); err != nil {
		response.ErrorFromDB(c, err, "更新公司失败")
		return
	}
	ctrl.recordOp(c, "update", strconv.FormatInt(id, 10), "更新公司")
	response.Success(c, req, "更新成功")
}

func (ctrl *CompanyController) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "id 无效")
		return
	}
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	if err := ctrl.svc.DeleteScoped(c.Request.Context(), scope, id); err != nil {
		response.ErrorFromDB(c, err, "删除公司失败")
		return
	}
	ctrl.recordOp(c, "delete", strconv.FormatInt(id, 10), "删除公司")
	response.Success(c, gin.H{"id": id}, "删除成功")
}

func (ctrl *CompanyController) ListCustomerRelations(c *gin.Context) {
	customerID := c.Param("id")
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	items, err := ctrl.svc.ListRelationsByCustomerScoped(c.Request.Context(), scope, customerID)
	if err != nil {
		if ctrl.companyScopeError(c, err, "获取客户公司关系失败") {
			return
		}
		response.ErrorFromDB(c, err, "获取客户公司关系失败")
		return
	}
	response.SuccessWithList(c, items, int64(len(items)))
}

func (ctrl *CompanyController) UpsertCustomerRelation(c *gin.Context) {
	customerID := c.Param("id")
	var req model.CustomerCompanyRelation
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	req.CustomerID = customerID
	req.TenantID = ctrl.tenantID(c)
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	if err := ctrl.svc.UpsertRelationScoped(c.Request.Context(), scope, &req); err != nil {
		if ctrl.companyScopeError(c, err, "保存客户公司关系失败") {
			return
		}
		response.ErrorFromDB(c, err, "保存客户公司关系失败")
		return
	}
	ctrl.recordOp(c, "upsert_relation", fmt.Sprintf("customer:%s,company:%d", customerID, req.CompanyID), "保存客户公司关系")
	response.Success(c, req, "保存成功")
}

func (ctrl *CompanyController) ListCompanyRelations(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "id 无效")
		return
	}
	uid, _ := ctrl.actor(c)
	scope := ctrl.svc.Scope(c.Request.Context(), uid)
	items, err := ctrl.svc.ListRelationsByCompanyScoped(c.Request.Context(), scope, id)
	if err != nil {
		if ctrl.companyScopeError(c, err, "获取公司关系失败") {
			return
		}
		response.ErrorFromDB(c, err, "获取公司关系失败")
		return
	}
	response.SuccessWithList(c, items, int64(len(items)))
}
