package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type crossAccountSearchService interface {
	SearchCustomers(ctx context.Context, tid uint, userID uint, keyword, industry string, page, pageSize int) ([]repository.CrossAccountSearchRow, int64, service.CrossAccountSearchScope, error)
}

type crossAccountAuditFunc func(c *gin.Context, tid, uid uint, uname, keyword, industry string, page, pageSize int, total int64, scope service.CrossAccountSearchScope)

type CrossAccountSearchController struct {
	svc   crossAccountSearchService
	audit crossAccountAuditFunc
}

func NewCrossAccountSearchController() *CrossAccountSearchController {
	ctrl := &CrossAccountSearchController{svc: service.NewCrossAccountSearchService()}
	ctrl.audit = ctrl.auditSearch
	return ctrl
}

func NewCrossAccountSearchControllerWithService(svc crossAccountSearchService) *CrossAccountSearchController {
	if svc == nil {
		svc = service.NewCrossAccountSearchService()
	}
	ctrl := &CrossAccountSearchController{svc: svc}
	ctrl.audit = ctrl.auditSearch
	return ctrl
}

func (ctrl *CrossAccountSearchController) RegisterRoutes(r *gin.RouterGroup) {
	g := r.Group("/cross-account-search")
	{
		g.GET("/customers", ctrl.SearchCustomers)
	}
}

func (ctrl *CrossAccountSearchController) actor(c *gin.Context) (uint, string) {
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
	username, _ := c.Get("username")
	uname, _ := username.(string)
	return uid, uname
}

// SearchCustomers PR-8：授权账号范围内按行业/关键词检索客户。最小可用实现，后续接 pgvector/资产索引。
func (ctrl *CrossAccountSearchController) SearchCustomers(c *gin.Context) {
	tid, ok := tenant.FromContext(c.Request.Context())
	if !ok || tid == 0 {
		response.Error(c, http.StatusBadRequest, "缺少租户上下文")
		return
	}
	uid, uname := ctrl.actor(c)

	keyword := c.Query("keyword")
	industry := c.Query("industry")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	svc := ctrl.svc
	if svc == nil {
		svc = service.NewCrossAccountSearchService()
	}
	rows, total, scope, err := svc.SearchCustomers(c.Request.Context(), tid, uid, keyword, industry, page, pageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "检索失败")
		return
	}

	if ctrl.audit != nil {
		ctrl.audit(c, tid, uid, uname, keyword, industry, page, pageSize, total, scope)
	} else {
		ctrl.auditSearch(c, tid, uid, uname, keyword, industry, page, pageSize, total, scope)
	}
	response.Success(c, gin.H{
		"list":      rows,
		"total":     total,
		"page":      int64(page),
		"page_size": int64(pageSize),
		"scope":     scope,
	}, "")
}

func (ctrl *CrossAccountSearchController) auditSearch(c *gin.Context, tid, uid uint, uname, keyword, industry string, page, pageSize int, total int64, scope service.CrossAccountSearchScope) {
	detail, _ := json.Marshal(map[string]any{
		"keyword":                  keyword,
		"industry":                 industry,
		"page":                     page,
		"page_size":                pageSize,
		"result_total":             total,
		"all_accounts":             scope.AllAccounts,
		"restricted":               scope.Restricted,
		"authorized_account_count": scope.AuthorizedAccountCount,
	})
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID:   tid,
		UserID:     uid,
		Username:   uname,
		Action:     "search",
		Module:     "cross_account_search",
		Resource:   "customer",
		ResourceID: "",
		Detail:     string(detail),
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}