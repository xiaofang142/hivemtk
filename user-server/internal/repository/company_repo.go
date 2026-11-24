package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type CompanyRepository interface {
	Create(ctx context.Context, company *model.Company) error
	Update(ctx context.Context, company *model.Company) error
	GetByID(ctx context.Context, id int64) (*model.Company, error)
	List(ctx context.Context, keyword string, page, pageSize int) ([]*model.Company, int64, error)
	Delete(ctx context.Context, id int64) error
	UpsertRelation(ctx context.Context, rel *model.CustomerCompanyRelation) error
	ListRelationsByCustomer(ctx context.Context, customerID string) ([]*model.CustomerCompanyRelation, error)
	ListRelationsByCompany(ctx context.Context, companyID int64) ([]*model.CustomerCompanyRelation, error)
}

type companyRepository struct{}

func NewCompanyRepository() CompanyRepository { return &companyRepository{} }

func (r *companyRepository) db(ctx context.Context) *gorm.DB {
	return dbFromCtx(ctx)
}

func (r *companyRepository) Create(ctx context.Context, company *model.Company) error {
	if company == nil {
		return errors.New("company is nil")
	}
	if company.TenantID == 0 {
		company.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(company).Error
}

func (r *companyRepository) Update(ctx context.Context, company *model.Company) error {
	if company == nil || company.ID == 0 {
		return errors.New("invalid company")
	}
	tid := tenant.WriteTenantID(ctx)
	return r.db(ctx).Model(&model.Company{}).
		Where("id = ? AND tenant_id = ?", company.ID, tid).
		Updates(company).Error
}

func (r *companyRepository) GetByID(ctx context.Context, id int64) (*model.Company, error) {
	tid := tenant.WriteTenantID(ctx)
	var c model.Company
	err := r.db(ctx).Where("id = ? AND tenant_id = ?", id, tid).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *companyRepository) List(ctx context.Context, keyword string, page, pageSize int) ([]*model.Company, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	tid := tenant.WriteTenantID(ctx)
	q := r.db(ctx).Model(&model.Company{}).Where("tenant_id = ?", tid)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name ILIKE ? OR domain ILIKE ? OR industry ILIKE ?", like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.Company
	if err := q.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *companyRepository) Delete(ctx context.Context, id int64) error {
	tid := tenant.WriteTenantID(ctx)
	return r.db(ctx).Where("id = ? AND tenant_id = ?", id, tid).Delete(&model.Company{}).Error
}

func (r *companyRepository) UpsertRelation(ctx context.Context, rel *model.CustomerCompanyRelation) error {
	if rel == nil {
		return errors.New("relation is nil")
	}
	if rel.TenantID == 0 {
		rel.TenantID = tenant.WriteTenantID(ctx)
	}
	// D-02: is_primary=true 时清理同客户其他 primary
	return r.db(ctx).Transaction(func(tx *gorm.DB) error {
		if rel.IsPrimary {
			if err := tx.Model(&model.CustomerCompanyRelation{}).
				Where("tenant_id = ? AND customer_id = ? AND is_primary = ? AND deleted_at IS NULL", rel.TenantID, rel.CustomerID, true).
				Updates(map[string]any{"is_primary": false}).Error; err != nil {
				return err
			}
			// 双写兼容缓存
			if err := tx.Model(&model.Customer{}).
				Where("tenant_id = ? AND id = ?", rel.TenantID, rel.CustomerID).
				Update("company_id", rel.CompanyID).Error; err != nil {
				return err
			}
		}
		var existing model.CustomerCompanyRelation
		err := tx.Where("tenant_id=? AND customer_id=? AND company_id=? AND relation_type=? AND deleted_at IS NULL",
			rel.TenantID, rel.CustomerID, rel.CompanyID, rel.RelationType).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(rel).Error
		}
		if err != nil {
			return err
		}
		rel.ID = existing.ID
		return tx.Model(&existing).Updates(rel).Error
	})
}

func (r *companyRepository) ListRelationsByCustomer(ctx context.Context, customerID string) ([]*model.CustomerCompanyRelation, error) {
	tid := tenant.WriteTenantID(ctx)
	var items []*model.CustomerCompanyRelation
	err := r.db(ctx).Where("tenant_id = ? AND customer_id = ? AND deleted_at IS NULL", tid, customerID).
		Order("is_primary DESC, updated_at DESC").Find(&items).Error
	return items, err
}

func (r *companyRepository) ListRelationsByCompany(ctx context.Context, companyID int64) ([]*model.CustomerCompanyRelation, error) {
	tid := tenant.WriteTenantID(ctx)
	var items []*model.CustomerCompanyRelation
	err := r.db(ctx).Where("tenant_id = ? AND company_id = ? AND deleted_at IS NULL", tid, companyID).
		Order("is_primary DESC, updated_at DESC").Find(&items).Error
	return items, err
}
