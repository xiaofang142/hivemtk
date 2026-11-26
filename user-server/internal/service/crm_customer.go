package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// 补缺实体种类（与 Controller 的 kind 入参一致）。
const (
	CRMEntityContact         = 1
	CRMEntitySupplier        = 2
	CRMEntityServiceProvider = 3
)

// CRMCustomerSave 客户新增/编辑入参（Service 层，Controller DTO 在此收敛）。
type CRMCustomerSave struct {
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	SourceChannel   string `json:"source_channel"`
	OwnerUserID     uint   `json:"owner_user_id"`
	Tags            string `json:"tags"`
}

// CRMEntitySave 联系人/供应商/服务商共用入参。
type CRMEntitySave struct {
	Name             string `json:"name"`
	CustomerID       string `json:"customer_id"`
	ChannelAccountID *uint  `json:"channel_account_id,omitempty"`
	ContactInfo      string `json:"contact_info"`
	Role             string `json:"role"`
	Remark           string `json:"remark"`
	Source           string `json:"source"`
	SourceRef        string `json:"source_ref"`
}

// CRMCustomerService 客户主数据 CRUD + 补缺实体写入（五层中的 Service 层）。
// 所有写操作先经数据权限校验，避免通过编辑接口绕过账号/行业隔离。
type CRMCustomerService struct {
	permission *CustomerPermissionService
	customers  repository.CustomerRepository
	contacts   repository.ContactRepository
	suppliers  repository.SupplierRepository
	providers  repository.ServiceProviderRepository
	auditRepo  repository.OperationLogRepository
	accounts   repository.ChannelAccountRepository
}

func NewCRMCustomerService() *CRMCustomerService {
	return &CRMCustomerService{
		permission: NewCustomerPermissionService(),
		customers:  repository.NewCustomerRepository(),
		contacts:   repository.NewContactRepository(),
		suppliers:  repository.NewSupplierRepository(),
		providers:  repository.NewServiceProviderRepository(),
		auditRepo:  repository.NewOperationLogRepository(),
		accounts:   repository.NewChannelAccountRepository(),
	}
}

// List 按权限 + 查询条件读取客户列表（Controller 不接触 repository 层）。
func (s *CRMCustomerService) List(ctx context.Context, scope CustomerDataScope, keyword, industry string, ownerUserID uint, page, pageSize int) ([]*model.Customer, int64, error) {
	filter := repository.CustomerScopeFilter{
		Keyword: keyword, ExcludeMerged: true, Page: page, PageSize: pageSize,
	}
	if industry != "" {
		filter.Industries = []string{industry}
	}
	if ownerUserID > 0 {
		filter.OwnerUserID = ownerUserID
	}
	return s.permission.ListScoped(ctx, scope, filter)
}

// Create 新建客户。权限按最终写入值校验，避免空值绕过账号/行业/负责人边界。
func (s *CRMCustomerService) Create(ctx context.Context, scope CustomerDataScope, in CRMCustomerSave) (*model.Customer, error) {
	if !s.permission.AllowsWrite(scope, in.SourceAccountID, in.Industry, in.OwnerUserID) {
		return nil, ErrCRMCustomerForbidden
	}
	cust := &model.Customer{
		Name: in.Name, Phone: in.Phone, Email: in.Email, Industry: in.Industry,
		SourceAccountID: in.SourceAccountID, SourceChannel: in.SourceChannel,
		Tags: firstNonEmptyCRMValue(in.Tags, "[]"), ChurnRisk: "low",
	}
	if in.OwnerUserID > 0 {
		cust.OwnerUserID = &in.OwnerUserID
	}
	if err := s.customers.Create(ctx, cust); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, "customer_create", cust.ID, scope.UserID, "")
	return cust, nil
}

// Update 编辑客户（先做权限校验，再改字段）。
// 校验使用归一后的最终写入值：显式清空账号/行业/负责人也必须过权限闸。
func (s *CRMCustomerService) Update(ctx context.Context, scope CustomerDataScope, id string, in CRMCustomerSave) (*model.Customer, error) {
	cust, err := s.permission.GetByIDScoped(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	final := s.finalCustomerState(cust, in)
	if !s.permission.AllowsWrite(scope, final.SourceAccountID, final.Industry, final.OwnerUserID) {
		return nil, ErrCRMCustomerForbidden
	}
	cust.Name = final.Name
	cust.Phone = final.Phone
	cust.Email = final.Email
	cust.Industry = final.Industry
	cust.SourceAccountID = final.SourceAccountID
	cust.SourceChannel = final.SourceChannel
	if final.OwnerUserID > 0 {
		cust.OwnerUserID = &final.OwnerUserID
	} else {
		cust.OwnerUserID = nil
	}
	cust.Tags = final.Tags
	if err := s.customers.Update(ctx, cust); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, "customer_update", cust.ID, scope.UserID, "")
	return cust, nil
}

// finalCustomerState 归一客户最终写入状态，供权限校验与落库共用同一份值。
// 输入字段的零值区分「未提交」（沿用旧值）与「明确清空」（按空串落库）。
func (s *CRMCustomerService) finalCustomerState(cust *model.Customer, in CRMCustomerSave) CRMCustomerSave {
	out := in
	if in.Name == "" {
		out.Name = cust.Name
	}
	if in.Phone == "" {
		out.Phone = cust.Phone
	}
	if in.Email == "" {
		out.Email = cust.Email
	}
	if in.Industry == "" {
		out.Industry = cust.Industry
	}
	if in.SourceAccountID == "" {
		out.SourceAccountID = cust.SourceAccountID
	}
	if in.SourceChannel == "" {
		out.SourceChannel = cust.SourceChannel
	}
	if in.Tags == "" {
		out.Tags = cust.Tags
	}
	out.OwnerUserID = custOwnerID(in.OwnerUserID, cust)
	return out
}

// Delete 删除客户（同样先做权限校验）。
func (s *CRMCustomerService) Delete(ctx context.Context, scope CustomerDataScope, id string) error {
	cust, err := s.permission.GetByIDScoped(ctx, scope, id)
	if err != nil {
		return err
	}
	if err := s.customers.Delete(ctx, cust.ID); err != nil {
		return err
	}
	s.writeAudit(ctx, "customer_delete", cust.ID, scope.UserID, "")
	return nil
}

func custOwnerID(input uint, c *model.Customer) uint {
	if input > 0 {
		return input
	}
	if c.OwnerUserID != nil {
		return *c.OwnerUserID
	}
	return 0
}

// validateEntityAccount 校验实体新增/改挂的渠道账号：必须真实存在、属于当前租户且在操作者范围内。
// 账号以稳定数字 ID 校验，仍须满足目标客户的行业/负责人条件。
func (s *CRMCustomerService) validateEntityAccount(ctx context.Context, scope CustomerDataScope, customer *model.Customer, accountID *uint) error {
	if accountID == nil {
		return nil // 未挂账号的旧实体继承已验证的客户范围。
	}
	if customer == nil || *accountID == 0 {
		return ErrCRMCustomerForbidden
	}
	if s.accounts == nil || !s.accounts.Available() {
		return errors.New("渠道账号存储不可用")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		tid = tenant.WriteTenantID(ctx)
	}
	acc, err := s.accounts.GetByID(ctx, tid, *accountID)
	if err != nil || acc == nil || acc.TenantID != tid {
		return ErrCRMCustomerForbidden
	}
	candidate := *customer
	candidate.SourceAccountID = strconv.FormatUint(uint64(*accountID), 10)
	if s.permission.CanAccess(scope, &candidate) {
		return nil
	}
	return ErrCRMCustomerForbidden
}

// CreateContact 新建联系人（客户必须在权限范围内）。
func (s *CRMCustomerService) CreateContact(ctx context.Context, scope CustomerDataScope, in CRMEntitySave) (*model.Contact, error) {
	if in.Name == "" {
		return nil, errors.New("联系人名称不能为空")
	}
	cust, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID)
	if err != nil {
		return nil, err
	}
	if err := s.validateEntityAccount(ctx, scope, cust, in.ChannelAccountID); err != nil {
		return nil, err
	}
	row := &model.Contact{
		Name: in.Name, CustomerID: in.CustomerID, ChannelAccountID: in.ChannelAccountID,
		ContactInfo: firstNonEmptyCRMValue(in.ContactInfo, "{}"), Role: in.Role, Remark: in.Remark,
		Source: in.Source, SourceRef: in.SourceRef,
	}
	if err := s.contacts.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// CreateSupplier 新建供应商。
func (s *CRMCustomerService) CreateSupplier(ctx context.Context, scope CustomerDataScope, in CRMEntitySave) (*model.Supplier, error) {
	if in.Name == "" {
		return nil, errors.New("供应商名称不能为空")
	}
	cust, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID)
	if err != nil {
		return nil, err
	}
	if err := s.validateEntityAccount(ctx, scope, cust, in.ChannelAccountID); err != nil {
		return nil, err
	}
	row := &model.Supplier{
		Name: in.Name, CustomerID: in.CustomerID, ChannelAccountID: in.ChannelAccountID,
		ContactInfo: firstNonEmptyCRMValue(in.ContactInfo, "{}"), Role: in.Role, Remark: in.Remark,
		Source: in.Source, SourceRef: in.SourceRef,
	}
	if err := s.suppliers.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// CreateServiceProvider 新建服务商。
func (s *CRMCustomerService) CreateServiceProvider(ctx context.Context, scope CustomerDataScope, in CRMEntitySave) (*model.ServiceProvider, error) {
	if in.Name == "" {
		return nil, errors.New("服务商名称不能为空")
	}
	cust, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID)
	if err != nil {
		return nil, err
	}
	if err := s.validateEntityAccount(ctx, scope, cust, in.ChannelAccountID); err != nil {
		return nil, err
	}
	row := &model.ServiceProvider{
		Name: in.Name, CustomerID: in.CustomerID, ChannelAccountID: in.ChannelAccountID,
		ContactInfo: firstNonEmptyCRMValue(in.ContactInfo, "{}"), Role: in.Role, Remark: in.Remark,
		Source: in.Source, SourceRef: in.SourceRef,
	}
	if err := s.providers.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// ListEntities 按客户 + 权限读取补缺实体（联系人/供应商/服务商）。
func (s *CRMCustomerService) ListEntities(ctx context.Context, scope CustomerDataScope, kind int, customerID string, page, pageSize int) (any, int64, error) {
	customer, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	if err != nil {
		return nil, 0, err
	}
	filter := s.permission.EntityAccountFilter(scope, customer)
	switch kind {
	case CRMEntityContact:
		return s.contacts.ListByCustomer(ctx, customerID, page, pageSize, filter)
	case CRMEntitySupplier:
		return s.suppliers.ListByCustomer(ctx, customerID, page, pageSize, filter)
	case CRMEntityServiceProvider:
		return s.providers.ListByCustomer(ctx, customerID, page, pageSize, filter)
	default:
		return nil, 0, errors.New("未知实体类型")
	}
}

// UpdateEntity 编辑补缺实体；同时校验旧客户与新客户的数据权限，避免越权改挂。
func (s *CRMCustomerService) UpdateEntity(ctx context.Context, scope CustomerDataScope, kind int, id int64, in CRMEntitySave) (any, error) {
	var oldCustomerID string
	var oldAccountID *uint
	var contact *model.Contact
	var supplier *model.Supplier
	var provider *model.ServiceProvider
	switch kind {
	case CRMEntityContact:
		row, err := s.contacts.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID, oldAccountID, contact = row.CustomerID, row.ChannelAccountID, row
	case CRMEntitySupplier:
		row, err := s.suppliers.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID, oldAccountID, supplier = row.CustomerID, row.ChannelAccountID, row
	case CRMEntityServiceProvider:
		row, err := s.providers.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID, oldAccountID, provider = row.CustomerID, row.ChannelAccountID, row
	default:
		return nil, errors.New("未知实体类型")
	}
	oldCustomer, err := s.permission.GetByIDScoped(ctx, scope, oldCustomerID)
	if err != nil {
		return nil, err
	}
	if err := s.validateEntityAccount(ctx, scope, oldCustomer, oldAccountID); err != nil {
		return nil, err
	}
	newCustomerID := in.CustomerID
	if newCustomerID == "" {
		newCustomerID = oldCustomerID
	}
	targetCustomer, err := s.permission.GetByIDScoped(ctx, scope, newCustomerID)
	if err != nil {
		return nil, err
	}
	finalAccountID := in.ChannelAccountID
	if finalAccountID == nil {
		finalAccountID = oldAccountID
	}
	if err := s.validateEntityAccount(ctx, scope, targetCustomer, finalAccountID); err != nil {
		return nil, err
	}
	switch kind {
	case CRMEntityContact:
		row := contact
		row.Name = in.Name
		row.CustomerID = newCustomerID
		if in.ChannelAccountID != nil {
			row.ChannelAccountID = in.ChannelAccountID
		}
		row.ContactInfo = firstNonEmptyCRMValue(in.ContactInfo, row.ContactInfo)
		row.Role = in.Role
		row.Remark = in.Remark
		row.Source = firstNonEmptyCRMValue(in.Source, row.Source)
		row.SourceRef = firstNonEmptyCRMValue(in.SourceRef, row.SourceRef)
		if err := s.contacts.Update(ctx, row); err != nil {
			return nil, err
		}
		s.writeAudit(ctx, "crm_entity_update", newCustomerID, scope.UserID, fmt.Sprintf("contact:%d", id))
		return row, nil
	case CRMEntitySupplier:
		row := supplier
		row.Name = in.Name
		row.CustomerID = newCustomerID
		if in.ChannelAccountID != nil {
			row.ChannelAccountID = in.ChannelAccountID
		}
		row.ContactInfo = firstNonEmptyCRMValue(in.ContactInfo, row.ContactInfo)
		row.Role = in.Role
		row.Remark = in.Remark
		row.Source = firstNonEmptyCRMValue(in.Source, row.Source)
		row.SourceRef = firstNonEmptyCRMValue(in.SourceRef, row.SourceRef)
		if err := s.suppliers.Update(ctx, row); err != nil {
			return nil, err
		}
		s.writeAudit(ctx, "crm_entity_update", newCustomerID, scope.UserID, fmt.Sprintf("supplier:%d", id))
		return row, nil
	default:
		row := provider
		row.Name = in.Name
		row.CustomerID = newCustomerID
		if in.ChannelAccountID != nil {
			row.ChannelAccountID = in.ChannelAccountID
		}
		row.ContactInfo = firstNonEmptyCRMValue(in.ContactInfo, row.ContactInfo)
		row.Role = in.Role
		row.Remark = in.Remark
		row.Source = firstNonEmptyCRMValue(in.Source, row.Source)
		row.SourceRef = firstNonEmptyCRMValue(in.SourceRef, row.SourceRef)
		if err := s.providers.Update(ctx, row); err != nil {
			return nil, err
		}
		s.writeAudit(ctx, "crm_entity_update", newCustomerID, scope.UserID, fmt.Sprintf("service_provider:%d", id))
		return row, nil
	}
}

// DeleteEntity 删除补缺实体；先按实体所属客户校验权限，再写操作审计。
func (s *CRMCustomerService) DeleteEntity(ctx context.Context, scope CustomerDataScope, kind int, id int64) error {
	var customerID, kindName string
	var accountID *uint
	switch kind {
	case CRMEntityContact:
		row, err := s.contacts.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("实体不存在")
		}
		customerID, kindName = row.CustomerID, "contact"
		accountID = row.ChannelAccountID
	case CRMEntitySupplier:
		row, err := s.suppliers.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("实体不存在")
		}
		customerID, kindName = row.CustomerID, "supplier"
		accountID = row.ChannelAccountID
	case CRMEntityServiceProvider:
		row, err := s.providers.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("实体不存在")
		}
		customerID, kindName = row.CustomerID, "service_provider"
		accountID = row.ChannelAccountID
	default:
		return errors.New("未知实体类型")
	}
	customer, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	if err != nil {
		return err
	}
	if err := s.validateEntityAccount(ctx, scope, customer, accountID); err != nil {
		return err
	}
	switch kind {
	case CRMEntityContact:
		err = s.contacts.Delete(ctx, id)
	case CRMEntitySupplier:
		err = s.suppliers.Delete(ctx, id)
	default:
		err = s.providers.Delete(ctx, id)
	}
	if err != nil {
		return err
	}
	s.writeAudit(ctx, "crm_entity_delete", customerID, scope.UserID, fmt.Sprintf("%s:%d", kindName, id))
	return nil
}

func (s *CRMCustomerService) writeAudit(ctx context.Context, action, customerID string, userID uint, detail string) {
	if s.auditRepo == nil {
		return
	}
	_ = s.auditRepo.Create(ctx, &model.OperationLog{
		UserID: userID, Action: action, Module: "crm",
		Resource: "customer", ResourceID: customerID, Detail: detail,
	})
}

// AllowsWrite 写权限：账号范围 + 行业范围 + 负责人范围。
// 空授权必须拒绝；owner 限定下 0 值不能绕过归属检查（最终写入值由调用方归一后传入）。
func (s *CustomerPermissionService) AllowsWrite(scope CustomerDataScope, accountID, industry string, ownerID uint) bool {
	candidate := &model.Customer{SourceAccountID: accountID, Industry: industry}
	if ownerID > 0 {
		candidate.OwnerUserID = &ownerID
	}
	return s.CanAccess(scope, candidate)
}

// DescribeScope 供 Controller 输出当前用户的权限范围（便于前端收敛选项）。
func (s *CustomerPermissionService) DescribeScope(scope CustomerDataScope) string {
	raw, _ := json.Marshal(map[string]any{
		"all_accounts": scope.AllAccounts, "restricted": scope.RestrictedAccounts,
		"account_count": len(scope.AccountIDs), "industries": scope.Industries,
		"owner_user_id": scope.OwnerUserID, "only_owned": scope.OnlyOwned,
	})
	return string(raw)
}
