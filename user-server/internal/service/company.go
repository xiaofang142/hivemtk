package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type CompanyService struct {
	repo       repository.CompanyRepository
	permission *CustomerPermissionService
}

func NewCompanyService(repo repository.CompanyRepository) *CompanyService {
	return &CompanyService{repo: repo, permission: NewCustomerPermissionService()}
}

func NewCompanyServiceDefault() *CompanyService {
	return NewCompanyService(repository.NewCompanyRepository())
}

func (s *CompanyService) Create(ctx context.Context, c *model.Company) error {
	return s.CreateScoped(ctx, CustomerDataScope{AllAccounts: true}, c)
}

func (s *CompanyService) CreateScoped(ctx context.Context, scope CustomerDataScope, c *model.Company) error {
	if c == nil || strings.TrimSpace(c.Name) == "" {
		return errors.New("公司名称不能为空")
	}
	tid := tenant.WriteTenantID(ctx)
	if tid == 0 {
		if t, ok := tenant.FromContext(ctx); ok {
			tid = t
		}
	}
	if tid > 0 {
		if c.TenantID != 0 && c.TenantID != tid {
			return ErrCRMCustomerForbidden
		}
		c.TenantID = tid
	}
	if !scope.AllAccounts {
		if c.Industry != "" && len(scope.Industries) > 0 && !containsCRMString(scope.Industries, c.Industry) {
			return ErrCRMCustomerForbidden
		}
		if c.OwnerUserID != nil {
			if scope.OnlyOwned && *c.OwnerUserID != scope.UserID {
				return ErrCRMCustomerForbidden
			}
			if scope.OwnerUserID > 0 && *c.OwnerUserID != scope.OwnerUserID {
				return ErrCRMCustomerForbidden
			}
		}
	}
	return s.repo.Create(ctx, c)
}

func (s *CompanyService) Update(ctx context.Context, c *model.Company) error {
	if c == nil || c.ID == 0 {
		return errors.New("无效公司ID")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("公司名称不能为空")
	}
	return s.repo.Update(ctx, c)
}

func (s *CompanyService) GetByID(ctx context.Context, id int64) (*model.Company, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *CompanyService) List(ctx context.Context, keyword string, page, pageSize int) ([]*model.Company, int64, error) {
	return s.repo.List(ctx, keyword, page, pageSize)
}

func (s *CompanyService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *CompanyService) UpsertRelation(ctx context.Context, rel *model.CustomerCompanyRelation) error {
	if rel == nil || rel.CustomerID == "" || rel.CompanyID == 0 {
		return errors.New("客户公司关系参数无效")
	}
	if rel.RelationType == "" {
		rel.RelationType = "primary"
	}
	return s.repo.UpsertRelation(ctx, rel)
}

func (s *CompanyService) ListRelationsByCustomer(ctx context.Context, customerID string) ([]*model.CustomerCompanyRelation, error) {
	if strings.TrimSpace(customerID) == "" {
		return nil, errors.New("customer_id 不能为空")
	}
	return s.repo.ListRelationsByCustomer(ctx, customerID)
}

func (s *CompanyService) ListRelationsByCompany(ctx context.Context, companyID int64) ([]*model.CustomerCompanyRelation, error) {
	if companyID <= 0 {
		return nil, errors.New("company_id 无效")
	}
	return s.repo.ListRelationsByCompany(ctx, companyID)
}

// companyVisibleIDs 计算当前 scope 可见的公司 ID 集合。
// 公司本身没有账号字段，可见性由「与其关联的客户是否在员工数据范围内」决定；
// 管理员保留全租户访问，不使用关联关系反向收敛管理员。
func (s *CompanyService) companyVisibleIDs(ctx context.Context, scope CustomerDataScope) (map[int64]bool, error) {
	visible := make(map[int64]bool)
	if scope.AllAccounts {
		return nil, nil // nil 表示不按集合过滤
	}
	if s.permission == nil {
		return nil, ErrCRMCustomerForbidden
	}
	customers, _, err := s.permission.ListScoped(ctx, scope, repository.CustomerScopeFilter{Page: 1, PageSize: 10000})
	if err != nil {
		return nil, err
	}
	for _, c := range customers {
		rels, err := s.repo.ListRelationsByCustomer(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			visible[rel.CompanyID] = true
		}
	}
	return visible, nil
}

// companyAllowed 判断公司是否对当前 scope 可见。
func (s *CompanyService) companyAllowed(scope CustomerDataScope, c *model.Company, visible map[int64]bool) bool {
	if c == nil {
		return false
	}
	if scope.AllAccounts {
		return true
	}
	if visible != nil && !visible[c.ID] {
		return false
	}
	if scope.OnlyOwned && (c.OwnerUserID == nil || *c.OwnerUserID != scope.UserID) {
		return false
	}
	if scope.OwnerUserID > 0 && (c.OwnerUserID == nil || *c.OwnerUserID != scope.OwnerUserID) {
		return false
	}
	return true
}

// ListScoped 按员工数据范围列出公司（服务端分页，总数不含被过滤行）。
func (s *CompanyService) ListScoped(ctx context.Context, scope CustomerDataScope, keyword string, page, pageSize int) ([]*model.Company, int64, error) {
	visible, err := s.companyVisibleIDs(ctx, scope)
	if err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	all, _, err := s.repo.List(ctx, keyword, 1, 10000)
	if err != nil {
		return nil, 0, err
	}
	allowed := make([]*model.Company, 0, len(all))
	for _, c := range all {
		if s.companyAllowed(scope, c, visible) {
			allowed = append(allowed, c)
		}
	}
	total := int64(len(allowed))
	start := (page - 1) * pageSize
	if start >= len(allowed) {
		return []*model.Company{}, total, nil
	}
	end := start + pageSize
	if end > len(allowed) {
		end = len(allowed)
	}
	return allowed[start:end], total, nil
}

// GetByIDScoped 按员工数据范围读取公司详情。
func (s *CompanyService) GetByIDScoped(ctx context.Context, scope CustomerDataScope, id int64) (*model.Company, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	visible, err := s.companyVisibleIDs(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !s.companyAllowed(scope, c, visible) {
		return nil, ErrCRMCustomerForbidden
	}
	return c, nil
}

// customerScoped 校验当前员工可访问客户（复用客户权限）。
func (s *CompanyService) customerScoped(ctx context.Context, scope CustomerDataScope, customerID string) error {
	if scope.AllAccounts {
		return nil
	}
	if s.permission == nil {
		return ErrCRMCustomerForbidden
	}
	_, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	return err
}

// UpsertRelationScoped 写关系时两端都校验：客户在员工可见范围，公司也在可见范围。
func (s *CompanyService) UpsertRelationScoped(ctx context.Context, scope CustomerDataScope, rel *model.CustomerCompanyRelation) error {
	if rel == nil || rel.CustomerID == "" || rel.CompanyID == 0 {
		return errors.New("客户公司关系参数无效")
	}
	if rel.RelationType == "" {
		rel.RelationType = "primary"
	}
	if err := s.customerScoped(ctx, scope, rel.CustomerID); err != nil {
		return err
	}
	if _, err := s.GetByIDScoped(ctx, scope, rel.CompanyID); err != nil {
		return err
	}
	return s.repo.UpsertRelation(ctx, rel)
}

// ListRelationsByCustomerScoped 按员工范围列出某客户的关系。
func (s *CompanyService) ListRelationsByCustomerScoped(ctx context.Context, scope CustomerDataScope, customerID string) ([]*model.CustomerCompanyRelation, error) {
	if err := s.customerScoped(ctx, scope, customerID); err != nil {
		return nil, err
	}
	rels, err := s.repo.ListRelationsByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	visible, err := s.companyVisibleIDs(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := make([]*model.CustomerCompanyRelation, 0, len(rels))
	for _, rel := range rels {
		if scope.AllAccounts || (visible != nil && visible[rel.CompanyID]) {
			out = append(out, rel)
		}
	}
	return out, nil
}

// ListRelationsByCompanyScoped 按员工范围列出某公司的关系。
func (s *CompanyService) ListRelationsByCompanyScoped(ctx context.Context, scope CustomerDataScope, companyID int64) ([]*model.CustomerCompanyRelation, error) {
	if _, err := s.GetByIDScoped(ctx, scope, companyID); err != nil {
		return nil, err
	}
	rels, err := s.repo.ListRelationsByCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	out := make([]*model.CustomerCompanyRelation, 0, len(rels))
	for _, rel := range rels {
		if scope.AllAccounts || s.customerScoped(ctx, scope, rel.CustomerID) == nil {
			out = append(out, rel)
		}
	}
	return out, nil
}

// Scope 从可信登录用户解析客户数据范围（Controller 只需传 uid，不自行解析权限）。
func (s *CompanyService) Scope(ctx context.Context, userID uint) CustomerDataScope {
	if s.permission == nil {
		s.permission = NewCustomerPermissionService()
	}
	scope, _ := s.permission.Resolve(ctx, userID)
	return scope
}

// UpdateScoped 按员工范围更新公司；范围不足时拒绝。
func (s *CompanyService) UpdateScoped(ctx context.Context, scope CustomerDataScope, c *model.Company) error {
	if _, err := s.GetByIDScoped(ctx, scope, c.ID); err != nil {
		return err
	}
	return s.Update(ctx, c)
}

// DeleteScoped 按员工范围删除公司；范围不足时拒绝。
func (s *CompanyService) DeleteScoped(ctx context.Context, scope CustomerDataScope, id int64) error {
	if _, err := s.GetByIDScoped(ctx, scope, id); err != nil {
		return err
	}
	return s.Delete(ctx, id)
}
