package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/pkg/tenant"
)

type CompanyService struct {
	repo repository.CompanyRepository
}

func NewCompanyService(repo repository.CompanyRepository) *CompanyService {
	return &CompanyService{repo: repo}
}

func NewCompanyServiceDefault() *CompanyService {
	return NewCompanyService(repository.NewCompanyRepository())
}

func (s *CompanyService) Create(ctx context.Context, c *model.Company) error {
	if c == nil || strings.TrimSpace(c.Name) == "" {
		return errors.New("公司名称不能为空")
	}
	if c.TenantID == 0 {
		c.TenantID = tenant.WriteTenantID(ctx)
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
