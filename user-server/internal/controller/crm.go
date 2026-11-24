package controller

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// CRMController CRM 主数据统一入口（US1）：CRUD / 去重 / 合并 / 导入导出 /
// 360 / AI / 审计 / 归档对接。仅解析入参与组响应，业务全部在 Service 层。
type CRMController struct {
	customers  *service.CRMCustomerService
	permission *service.CustomerPermissionService
	dedupe     *service.CustomerDedupeService
	imports    *service.CustomerImportExportService
	crm360     *service.CRM360Service
	ai         *service.CustomerAIService
	archive    *service.ClueArchiveService
}

func NewCRMController() *CRMController {
	return &CRMController{
		customers:  service.NewCRMCustomerService(),
		permission: service.NewCustomerPermissionService(),
		dedupe:     service.NewCustomerDedupeService(),
		imports:    service.NewCustomerImportExportService(),
		crm360:     service.NewCRM360Service(),
		ai:         service.NewCustomerAIService(),
		archive:    service.NewClueArchiveService(),
	}
}

// SetProfileService 注入既有 360 Profile 服务（Router 装配点调用）。
func (c *CRMController) SetProfileService(svc *service.Customer360Service) {
	if c.crm360 != nil {
		c.crm360.SetProfileService(svc)
	}
}

// SetClueNotifier 注入归档负责人推送出口（US2 T042 实现，Router 装配点注入）。
func (c *CRMController) SetClueNotifier(n service.CustomerOwnerNotifier) {
	if c.archive != nil {
		c.archive.SetNotifier(n)
	}
}

func (c *CRMController) scope(ctx *gin.Context) service.CustomerDataScope {
	uid := getUserIDFromContext(ctx)
	scope, _ := c.permission.Resolve(ctx.Request.Context(), uid)
	return scope
}

// ListCustomers GET /api/manage/crm/customers
func (c *CRMController) ListCustomers(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	ownerID, _ := strconv.ParseUint(ctx.Query("owner_user_id"), 10, 32)
	rows, total, err := c.customers.List(ctx.Request.Context(), c.scope(ctx),
		ctx.Query("keyword"), ctx.Query("industry"), uint(ownerID), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询客户失败")
		return
	}
	response.SuccessWithList(ctx, rows, total)
}

// GetCustomer GET /api/manage/crm/customers/:id
func (c *CRMController) GetCustomer(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	row, err := c.permission.GetByIDScoped(ctx.Request.Context(), c.scope(ctx), ctx.Param("id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询客户失败")
		return
	}
	response.Success(ctx, row, "ok")
}

// CreateCustomer POST /api/manage/crm/customers
func (c *CRMController) CreateCustomer(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMCustomerSaveRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	row, err := c.customers.Create(ctx.Request.Context(), c.scope(ctx), service.CRMCustomerSave{
		Name: in.Name, Phone: in.Phone, Email: in.Email, Industry: in.Industry,
		SourceAccountID: in.SourceAccountID, SourceChannel: in.SourceChannel,
		OwnerUserID: in.OwnerUserID, Tags: in.Tags,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建客户失败")
		return
	}
	response.Success(ctx, row, "创建成功")
}

// UpdateCustomer PUT /api/manage/crm/customers/:id
func (c *CRMController) UpdateCustomer(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMCustomerSaveRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	row, err := c.customers.Update(ctx.Request.Context(), c.scope(ctx), ctx.Param("id"), service.CRMCustomerSave{
		Name: in.Name, Phone: in.Phone, Email: in.Email, Industry: in.Industry,
		SourceAccountID: in.SourceAccountID, SourceChannel: in.SourceChannel,
		OwnerUserID: in.OwnerUserID, Tags: in.Tags,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "更新客户失败")
		return
	}
	response.Success(ctx, row, "更新成功")
}

// DeleteCustomer DELETE /api/manage/crm/customers/:id
func (c *CRMController) DeleteCustomer(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	if err := c.customers.Delete(ctx.Request.Context(), c.scope(ctx), ctx.Param("id")); err != nil {
		response.ErrorFromDB(ctx, err, "删除客户失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// DedupeScan POST /api/manage/crm/dedupe/scan
func (c *CRMController) DedupeScan(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMDedupeScanRequest
	_ = ctx.ShouldBindJSON(&in)
	rows, err := c.dedupe.FindDuplicates(ctx.Request.Context(), service.DedupeScanOptions{
		MaxCustomers: in.MaxCustomers, NameThreshold: in.NameThreshold, SameAccountOnly: in.SameAccountOnly,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "去重扫描失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

// Merge POST /api/manage/crm/merge
func (c *CRMController) Merge(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMMergeRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	operator := in.OperatorName
	if operator == "" {
		username, _ := ctx.Get("username")
		operator, _ = username.(string)
	}
	row, err := c.dedupe.Merge(ctx.Request.Context(), in.PrimaryID, in.SecondaryID, operator, in.Reason, in.Source)
	if err != nil {
		response.ErrorFromDB(ctx, err, "合并客户失败")
		return
	}
	response.Success(ctx, row, "合并成功")
}

// MergeAudit GET /api/manage/crm/merge/audit
func (c *CRMController) MergeAudit(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	id := ctx.Query("customer_id")
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 不能为空")
		return
	}
	rows, err := c.dedupe.ListMergeAudit(ctx.Request.Context(), id)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询合并审计失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

// Import POST /api/manage/crm/import
func (c *CRMController) Import(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMImportRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	rows := make([]service.CustomerImportRow, 0, len(in.Rows))
	for _, r := range in.Rows {
		rows = append(rows, service.CustomerImportRow{
			Name: r.Name, Phone: r.Phone, Email: r.Email, Industry: r.Industry,
			SourceAccountID: r.SourceAccountID, SourceChannel: r.SourceChannel,
			OwnerUserID: r.OwnerUserID, Tags: r.Tags,
		})
	}
	username, _ := ctx.Get("username")
	operator, _ := username.(string)
	result, err := c.imports.Import(ctx.Request.Context(), c.scope(ctx), rows, operator)
	if err != nil {
		response.ErrorFromDB(ctx, err, "导入客户失败")
		return
	}
	response.Success(ctx, result, "导入完成")
}

// Export GET /api/manage/crm/export
func (c *CRMController) Export(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	ownerID, _ := strconv.ParseUint(ctx.Query("owner_user_id"), 10, 32)
	username, _ := ctx.Get("username")
	operator, _ := username.(string)
	result, err := c.imports.ExportByQuery(ctx.Request.Context(), c.scope(ctx),
		ctx.Query("keyword"), ctx.Query("industry"), uint(ownerID), operator)
	if err != nil {
		response.ErrorFromDB(ctx, err, "导出客户失败")
		return
	}
	if ctx.Query("format") != "csv" {
		response.Success(ctx, result, "ok")
		return
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"id", "name", "phone", "email", "industry", "source_account_id", "owner_user_id", "tags"})
	for _, row := range result.Rows {
		_ = w.Write([]string{row.ID, row.Name, row.Phone, row.Email, row.Industry,
			row.SourceAccountID, row.OwnerUserID, row.Tags})
	}
	w.Flush()
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// View360 GET /api/manage/crm/360/:id
func (c *CRMController) View360(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	view, err := c.crm360.View(ctx.Request.Context(), c.scope(ctx), ctx.Param("id"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询 360 视图失败")
		return
	}
	response.Success(ctx, view, "ok")
}

// CrossIndustrySearch GET /api/manage/crm/cross-industry
func (c *CRMController) CrossIndustrySearch(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	result, err := c.crm360.CrossIndustrySearch(ctx.Request.Context(), tid, c.scope(ctx),
		ctx.Query("keyword"), ctx.Query("industry"), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "跨账号检索失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// AIAnalyze POST /api/manage/crm/ai/analyze
func (c *CRMController) AIAnalyze(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	customerID := ctx.Query("customer_id")
	if customerID == "" {
		var body struct {
			CustomerID string `json:"customer_id"`
		}
		_ = ctx.ShouldBindJSON(&body)
		customerID = body.CustomerID
	}
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "customer_id 不能为空")
		return
	}
	result, err := c.ai.Analyze(ctx.Request.Context(), c.scope(ctx), customerID)
	if err != nil {
		if errors.Is(err, service.ErrAIDispatcherUnavailable) {
			response.Error(ctx, http.StatusServiceUnavailable, "AI 服务暂不可用")
			return
		}
		response.ErrorFromDB(ctx, err, "AI 分析失败")
		return
	}
	response.Success(ctx, result, "ok")
}

// CreateContact POST /api/manage/crm/contacts
func (c *CRMController) CreateContact(ctx *gin.Context) {
	c.saveEntity(ctx, 1)
}

// CreateSupplier POST /api/manage/crm/suppliers
func (c *CRMController) CreateSupplier(ctx *gin.Context) {
	c.saveEntity(ctx, 2)
}

// CreateServiceProvider POST /api/manage/crm/service-providers
func (c *CRMController) CreateServiceProvider(ctx *gin.Context) {
	c.saveEntity(ctx, 3)
}

func (c *CRMController) saveEntity(ctx *gin.Context, kind int) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMEntitySaveRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	payload := service.CRMEntitySave{
		Name: in.Name, CustomerID: in.CustomerID, ChannelAccountID: in.ChannelAccountID,
		ContactInfo: in.ContactInfo, Role: in.Role, Remark: in.Remark,
		Source: in.Source, SourceRef: in.SourceRef,
	}
	var row any
	var err error
	switch kind {
	case 1:
		row, err = c.customers.CreateContact(ctx.Request.Context(), c.scope(ctx), payload)
	case 2:
		row, err = c.customers.CreateSupplier(ctx.Request.Context(), c.scope(ctx), payload)
	default:
		row, err = c.customers.CreateServiceProvider(ctx.Request.Context(), c.scope(ctx), payload)
	}
	if err != nil {
		response.ErrorFromDB(ctx, err, "保存失败")
		return
	}
	response.Success(ctx, row, "保存成功")
}

// ListContacts GET /api/manage/crm/contacts
func (c *CRMController) ListContacts(ctx *gin.Context) { c.listEntities(ctx, 1) }

// ListSuppliers GET /api/manage/crm/suppliers
func (c *CRMController) ListSuppliers(ctx *gin.Context) { c.listEntities(ctx, 2) }

// ListServiceProviders GET /api/manage/crm/service-providers
func (c *CRMController) ListServiceProviders(ctx *gin.Context) { c.listEntities(ctx, 3) }

func (c *CRMController) listEntities(ctx *gin.Context, kind int) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	customerID := ctx.Query("customer_id")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	rows, total, err := c.customers.ListEntities(ctx.Request.Context(), c.scope(ctx), kind, customerID, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "读取失败")
		return
	}
	response.SuccessWithList(ctx, rows, total)
}

// UpdateContact PUT /api/manage/crm/contacts/:id
func (c *CRMController) UpdateContact(ctx *gin.Context) { c.updateEntity(ctx, 1) }

// UpdateSupplier PUT /api/manage/crm/suppliers/:id
func (c *CRMController) UpdateSupplier(ctx *gin.Context) { c.updateEntity(ctx, 2) }

// UpdateServiceProvider PUT /api/manage/crm/service-providers/:id
func (c *CRMController) UpdateServiceProvider(ctx *gin.Context) { c.updateEntity(ctx, 3) }

func (c *CRMController) updateEntity(ctx *gin.Context, kind int) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "实体 ID 错误")
		return
	}
	var in dto.CRMEntitySaveRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	payload := service.CRMEntitySave{
		Name: in.Name, CustomerID: in.CustomerID, ChannelAccountID: in.ChannelAccountID,
		ContactInfo: in.ContactInfo, Role: in.Role, Remark: in.Remark,
		Source: in.Source, SourceRef: in.SourceRef,
	}
	row, err := c.customers.UpdateEntity(ctx.Request.Context(), c.scope(ctx), kind, id, payload)
	if err != nil {
		response.ErrorFromDB(ctx, err, "更新失败")
		return
	}
	response.Success(ctx, row, "更新成功")
}

// DeleteContact DELETE /api/manage/crm/contacts/:id
func (c *CRMController) DeleteContact(ctx *gin.Context) { c.deleteEntity(ctx, 1) }

// DeleteSupplier DELETE /api/manage/crm/suppliers/:id
func (c *CRMController) DeleteSupplier(ctx *gin.Context) { c.deleteEntity(ctx, 2) }

// DeleteServiceProvider DELETE /api/manage/crm/service-providers/:id
func (c *CRMController) DeleteServiceProvider(ctx *gin.Context) { c.deleteEntity(ctx, 3) }

func (c *CRMController) deleteEntity(ctx *gin.Context, kind int) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "实体 ID 错误")
		return
	}
	if err := c.customers.DeleteEntity(ctx.Request.Context(), c.scope(ctx), kind, id); err != nil {
		response.ErrorFromDB(ctx, err, "删除失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}

// ArchiveClue POST /api/manage/crm/clue/archive
// US2 T041 在归档成功后调用；线索模块内部不在本任务范围。
func (c *CRMController) ArchiveClue(ctx *gin.Context) {
	if _, ok := channelTenantID(ctx); !ok {
		return
	}
	var in dto.CRMArchiveClueRequest
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	result, err := c.archive.ArchiveClueAndLinkCustomer(ctx.Request.Context(), service.ArchiveClueInput{
		ClueID: in.ClueID, CustomerID: in.CustomerID,
		Identifiers: identity.Identifiers{
			Phone: in.Phone, Email: in.Email, WechatOpenID: in.WechatOpenID,
			DouyinOpenID: in.DouyinOpenID, XiaohongshuID: in.XiaohongshuID,
		},
		Name: in.Name, Industry: in.Industry, SourceAccountID: in.SourceAccountID,
		SourceChannel: in.SourceChannel, OwnerUserID: in.OwnerUserID,
		Summary: in.Summary, EvidenceJSON: in.EvidenceJSON,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "归档关联客户失败")
		return
	}
	response.Success(ctx, result, "归档关联完成")
}
