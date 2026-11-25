package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type CustomerImportJobController struct {
	svc *service.CustomerImportJobService
}

func NewCustomerImportJobController() *CustomerImportJobController {
	return &CustomerImportJobController{svc: service.NewCustomerImportJobService()}
}

func (ctrl *CustomerImportJobController) RegisterRoutes(r *gin.RouterGroup) {
	g := r.Group("/customer-import-jobs")
	{
		g.GET("/template", ctrl.Template)
		g.POST("/precheck", ctrl.Precheck)
		g.POST("", ctrl.Create)
		g.GET("", ctrl.List)
		g.GET("/:id/errors/download", ctrl.DownloadErrors)
		g.GET("/:id/errors", ctrl.Errors)
		g.POST("/:id/execute", ctrl.Execute)
		g.GET("/:id", ctrl.Get)
	}
}

func (ctrl *CustomerImportJobController) tenantID(c *gin.Context) uint {
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		return id
	}
	return 1
}

func (ctrl *CustomerImportJobController) actor(c *gin.Context) (uint, string) {
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

func (ctrl *CustomerImportJobController) recordOp(c *gin.Context, action, resourceID, detail string) {
	uid, uname := ctrl.actor(c)
	_ = service.NewOperationLogService().Create(c.Request.Context(), &model.OperationLog{
		TenantID:   ctrl.tenantID(c),
		UserID:     uid,
		Username:   uname,
		Action:     action,
		Module:     "customer_import",
		Resource:   "customer_import_job",
		ResourceID: resourceID,
		Detail:     detail,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}

// importScope 从可信登录用户解析 CRM 数据范围。
func (ctrl *CustomerImportJobController) importScope(c *gin.Context) (service.CustomerDataScope, uint) {
	uid, _ := ctrl.actor(c)
	return ctrl.svc.Scope(c.Request.Context(), uid), uid
}

// importScopeError 把导入作业权限错误映射为 403。
func (ctrl *CustomerImportJobController) importScopeError(c *gin.Context, err error) bool {
	if errors.Is(err, service.ErrImportJobForbidden) {
		response.Error(c, http.StatusForbidden, "无权访问该导入作业")
		return true
	}
	return false
}

func (ctrl *CustomerImportJobController) Template(c *gin.Context) {
	entityType := c.DefaultQuery("entity_type", "customer")
	data, filename, err := ctrl.svc.TemplateCSV(entityType)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (ctrl *CustomerImportJobController) Precheck(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "缺少导入文件(file)")
		return
	}
	f, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "打开导入文件失败")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Ctx(c.Request.Context()).Warn().Err(err).Msg("关闭导入预检文件失败")
		}
	}()

	entityType := c.DefaultPostForm("entity_type", "customer")
	dedupStrategy := c.DefaultPostForm("dedup_strategy", "skip")
	mappingJSON := c.DefaultPostForm("mapping_json", "{}")

	_, actorID := ctrl.importScope(c)
	report, err := ctrl.svc.PrecheckScoped(c.Request.Context(), actorID, file.Filename, f, entityType, dedupStrategy, mappingJSON)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	ctrl.recordOp(c, "precheck", report.JobID, fmt.Sprintf("导入预检: %s, total=%d, invalid=%d, dup=%d", file.Filename, report.TotalRows, report.InvalidRows, report.DuplicateRows))
	response.Success(c, report, "预检完成")
}

func (ctrl *CustomerImportJobController) Execute(c *gin.Context) {
	jobID := c.Param("id")
	scope, actorID := ctrl.importScope(c)
	report, err := ctrl.svc.ExecuteScoped(c.Request.Context(), scope, actorID, jobID)
	if err != nil {
		if ctrl.importScopeError(c, err) {
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	ctrl.recordOp(c, "execute", jobID, fmt.Sprintf("导入执行: status=%s, success=%d, failed=%d, skipped=%d, updated=%d", report.Status, report.SuccessRows, report.FailedRows, report.SkippedRows, report.UpdatedRows))
	response.Success(c, report, "导入执行完成")
}

func (ctrl *CustomerImportJobController) Errors(c *gin.Context) {
	jobID := c.Param("id")
	scope, actorID := ctrl.importScope(c)
	errs, err := ctrl.svc.ErrorReportScoped(c.Request.Context(), scope, actorID, jobID)
	if err != nil {
		if ctrl.importScopeError(c, err) {
			return
		}
		response.ErrorFromDB(c, err, "获取导入错误报告失败")
		return
	}
	response.Success(c, gin.H{"job_id": jobID, "errors": errs, "total": len(errs)}, "ok")
}

func (ctrl *CustomerImportJobController) DownloadErrors(c *gin.Context) {
	jobID := c.Param("id")
	scope, actorID := ctrl.importScope(c)
	data, filename, err := ctrl.svc.ErrorReportCSVScoped(c.Request.Context(), scope, actorID, jobID)
	if err != nil {
		if ctrl.importScopeError(c, err) {
			return
		}
		response.ErrorFromDB(c, err, "下载导入错误报告失败")
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (ctrl *CustomerImportJobController) Create(c *gin.Context) {
	var req model.CustomerImportJob
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if tid, ok := tenant.FromContext(c.Request.Context()); ok {
		req.TenantID = tid
	}
	if uid, _ := ctrl.actor(c); uid > 0 {
		req.CreatedBy = &uid
	}
	if err := ctrl.svc.Create(c.Request.Context(), &req); err != nil {
		response.ErrorFromDB(c, err, "创建导入任务失败")
		return
	}
	ctrl.recordOp(c, "create", req.JobID, "创建导入任务")
	response.Success(c, req, "创建成功")
}

func (ctrl *CustomerImportJobController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	scope, actorID := ctrl.importScope(c)
	items, total, err := ctrl.svc.ListScoped(c.Request.Context(), scope, actorID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "获取导入任务失败")
		return
	}
	response.SuccessWithPage(c, items, int64(page), int64(pageSize), total)
}

func (ctrl *CustomerImportJobController) Get(c *gin.Context) {
	scope, actorID := ctrl.importScope(c)
	item, err := ctrl.svc.GetScoped(c.Request.Context(), scope, actorID, c.Param("id"))
	if err != nil {
		if ctrl.importScopeError(c, err) {
			return
		}
		response.ErrorFromDB(c, err, "获取导入任务失败")
		return
	}
	response.Success(c, item, "ok")
}
