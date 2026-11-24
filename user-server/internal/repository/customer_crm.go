package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// CustomerScopeFilter CRM 列表/导出的统一过滤条件。
// 账号范围、可见行业、负责人均为 AND 叠加；账号范围由数据权限服务解析后传入。
type CustomerScopeFilter struct {
	SourceAccountIDs   []string
	RestrictedAccounts bool // true 且 SourceAccountIDs 为空 => 拒绝全部（fail-closed）
	Industries         []string
	OwnerUserID        uint
	Keyword            string
	ExcludeMerged      bool
	Page               int
	PageSize           int
}

// CRMCustomerRepository 仅承载 US1 新增的按数据权限范围读取客户的能力。
// 不改动既有 CustomerRepository 接口，避免影响其大量测试桩实现。
type CRMCustomerRepository interface {
	ListScoped(ctx context.Context, filter CustomerScopeFilter) ([]*model.Customer, int64, error)
	GetByIDScoped(ctx context.Context, id string) (*model.Customer, error)
}

type crmCustomerRepository struct{}

func NewCRMCustomerRepository() CRMCustomerRepository { return &crmCustomerRepository{} }

func (r *crmCustomerRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func applyCustomerScope(q *gorm.DB, f CustomerScopeFilter) *gorm.DB {
	if f.RestrictedAccounts {
		if len(f.SourceAccountIDs) == 0 {
			return q.Where("1 = 0")
		}
		q = q.Where("source_account_id IN ?", f.SourceAccountIDs)
	} else if len(f.SourceAccountIDs) > 0 {
		q = q.Where("source_account_id IN ?", f.SourceAccountIDs)
	}
	if len(f.Industries) > 0 {
		q = q.Where("industry IN ?", f.Industries)
	}
	if f.OwnerUserID > 0 {
		q = q.Where("owner_user_id = ?", f.OwnerUserID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("name ILIKE ? OR phone ILIKE ? OR email ILIKE ?", like, like, like)
	}
	if f.ExcludeMerged {
		q = q.Where("(merged_into_id IS NULL OR merged_into_id = '')")
	}
	return q
}

func (r *crmCustomerRepository) ListScoped(ctx context.Context, f CustomerScopeFilter) ([]*model.Customer, int64, error) {
	page, pageSize := f.Page, f.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	q := r.db(ctx).Model(&model.Customer{}).Where("tenant_id = ?", tenant.WriteTenantID(ctx))
	q = applyCustomerScope(q, f)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.Customer
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (r *crmCustomerRepository) GetByIDScoped(ctx context.Context, id string) (*model.Customer, error) {
	if id == "" {
		return nil, errors.New("customer id 不能为空")
	}
	var row model.Customer
	err := r.db(ctx).Where("tenant_id = ? AND id = ?", tenant.WriteTenantID(ctx), id).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}
