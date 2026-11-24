package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
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
}

func NewCRMCustomerService() *CRMCustomerService {
	return &CRMCustomerService{
		permission: NewCustomerPermissionService(),
		customers:  repository.NewCustomerRepository(),
		contacts:   repository.NewContactRepository(),
		suppliers:  repository.NewSupplierRepository(),
		providers:  repository.NewServiceProviderRepository(),
		auditRepo:  repository.NewOperationLogRepository(),
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

// Create 新建客户。
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
func (s *CRMCustomerService) Update(ctx context.Context, scope CustomerDataScope, id string, in CRMCustomerSave) (*model.Customer, error) {
	cust, err := s.permission.GetByIDScoped(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if !s.permission.AllowsWrite(scope, firstNonEmptyCRMValue(in.SourceAccountID, cust.SourceAccountID),
		firstNonEmptyCRMValue(in.Industry, cust.Industry), custOwnerID(in.OwnerUserID, cust)) {
		return nil, ErrCRMCustomerForbidden
	}
	cust.Name = in.Name
	cust.Phone = in.Phone
	cust.Email = in.Email
	cust.Industry = in.Industry
	cust.SourceAccountID = in.SourceAccountID
	cust.SourceChannel = in.SourceChannel
	if in.OwnerUserID > 0 {
		cust.OwnerUserID = &in.OwnerUserID
	}
	if in.Tags != "" {
		cust.Tags = in.Tags
	}
	if err := s.customers.Update(ctx, cust); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, "customer_update", cust.ID, scope.UserID, "")
	return cust, nil
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

// CreateContact 新建联系人（客户必须在权限范围内）。
func (s *CRMCustomerService) CreateContact(ctx context.Context, scope CustomerDataScope, in CRMEntitySave) (*model.Contact, error) {
	if in.Name == "" {
		return nil, errors.New("联系人名称不能为空")
	}
	if _, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID); err != nil {
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
	if _, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID); err != nil {
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
	if _, err := s.permission.GetByIDScoped(ctx, scope, in.CustomerID); err != nil {
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

// entityCustomerScoped 校验当前用户对实体所挂客户的读取权限（fail-closed）。
func (s *CRMCustomerService) entityCustomerScoped(ctx context.Context, scope CustomerDataScope, customerID string) error {
	_, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	return err
}

// ListEntities 按客户 + 权限读取补缺实体（联系人/供应商/服务商）。
func (s *CRMCustomerService) ListEntities(ctx context.Context, scope CustomerDataScope, kind int, customerID string, page, pageSize int) (any, int64, error) {
	if err := s.entityCustomerScoped(ctx, scope, customerID); err != nil {
		return nil, 0, err
	}
	switch kind {
	case CRMEntityContact:
		return s.contacts.ListByCustomer(ctx, customerID, page, pageSize)
	case CRMEntitySupplier:
		return s.suppliers.ListByCustomer(ctx, customerID, page, pageSize)
	case CRMEntityServiceProvider:
		return s.providers.ListByCustomer(ctx, customerID, page, pageSize)
	default:
		return nil, 0, errors.New("未知实体类型")
	}
}

// UpdateEntity 编辑补缺实体；同时校验旧客户与新客户的数据权限，避免越权改挂。
func (s *CRMCustomerService) UpdateEntity(ctx context.Context, scope CustomerDataScope, kind int, id int64, in CRMEntitySave) (any, error) {
	var oldCustomerID string
	switch kind {
	case CRMEntityContact:
		row, err := s.contacts.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID = row.CustomerID
	case CRMEntitySupplier:
		row, err := s.suppliers.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID = row.CustomerID
	case CRMEntityServiceProvider:
		row, err := s.providers.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, errors.New("实体不存在")
		}
		oldCustomerID = row.CustomerID
	default:
		return nil, errors.New("未知实体类型")
	}
	if err := s.entityCustomerScoped(ctx, scope, oldCustomerID); err != nil {
		return nil, err
	}
	newCustomerID := in.CustomerID
	if newCustomerID == "" {
		newCustomerID = oldCustomerID
	}
	if newCustomerID != oldCustomerID {
		if err := s.entityCustomerScoped(ctx, scope, newCustomerID); err != nil {
			return nil, err
		}
	}
	switch kind {
	case CRMEntityContact:
		row, _ := s.contacts.GetByID(ctx, id)
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
		row, _ := s.suppliers.GetByID(ctx, id)
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
		row, _ := s.providers.GetByID(ctx, id)
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
	case CRMEntitySupplier:
		row, err := s.suppliers.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("实体不存在")
		}
		customerID, kindName = row.CustomerID, "supplier"
	case CRMEntityServiceProvider:
		row, err := s.providers.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("实体不存在")
		}
		customerID, kindName = row.CustomerID, "service_provider"
	default:
		return errors.New("未知实体类型")
	}
	if err := s.entityCustomerScoped(ctx, scope, customerID); err != nil {
		return err
	}
	var err error
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
func (s *CustomerPermissionService) AllowsWrite(scope CustomerDataScope, accountID, industry string, ownerID uint) bool {
	if scope.AllAccounts {
		return true
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) > 0 && !containsCRMString(scope.AccountIDs, accountID) {
		return false
	}
	if len(scope.Industries) > 0 && !containsCRMString(scope.Industries, industry) {
		return false
	}
	if scope.OnlyOwned && ownerID > 0 && ownerID != scope.UserID {
		return false
	}
	if scope.OwnerUserID > 0 && ownerID > 0 && ownerID != scope.OwnerUserID {
		return false
	}
	return true
}

// DescribeScope 供 Controller 输出当前用户的权限范围（便于前端收敛选项）。
func (s *CustomerPermissionService) DescribeScope(scope CustomerDataScope) string {
	raw, _ := json.Marshal(map[string]any{
		"all_accounts": scope.AllAccounts, "restricted": scope.RestrictedAccounts,
		"account_count": len(scope.AccountIDs), "industries": scope.Industries,
		"owner_user_id": scope.OwnerUserID, "only_owned": scope.OnlyOwned,
	})
	return fmt.Sprintf("%s", raw)
}
