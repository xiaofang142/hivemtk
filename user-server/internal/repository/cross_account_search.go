package repository

import (
	"context"

	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// CrossAccountSearchRow 是跨账号检索的轻量行。
type CrossAccountSearchRow struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Phone           string `json:"phone,omitempty"`
	Email           string `json:"email,omitempty"`
	SourceChannel   string `json:"source_channel"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	CompanyID       *int64 `json:"company_id"`
}

type CrossAccountSearchRepository interface {
	SearchCustomers(ctx context.Context, tid uint, keyword, industry string, allowedAccountIDs []string, allAccounts bool, page, pageSize int) ([]CrossAccountSearchRow, int64, error)
}

type crossAccountSearchRepo struct {
	db *gorm.DB
}

func NewCrossAccountSearchRepository() CrossAccountSearchRepository {
	return &crossAccountSearchRepo{db: db.GetDB()}
}

func NewCrossAccountSearchRepositoryWithDB(dbh *gorm.DB) CrossAccountSearchRepository {
	return &crossAccountSearchRepo{db: dbh}
}

func (r *crossAccountSearchRepo) SearchCustomers(ctx context.Context, tid uint, keyword, industry string, allowedAccountIDs []string, allAccounts bool, page, pageSize int) ([]CrossAccountSearchRow, int64, error) {
	if !allAccounts && len(allowedAccountIDs) == 0 {
		return []CrossAccountSearchRow{}, 0, nil
	}
	q := r.db.WithContext(ctx).Table("customers").
		Select("id, name, phone, email, source_channel, source_account_id, company_id").
		Where("tenant_id = ? AND deleted_at IS NULL", tid)
	if !allAccounts {
		q = q.Where("source_account_id IN ?", allowedAccountIDs)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name ILIKE ? OR phone ILIKE ? OR email ILIKE ?", like, like, like)
	}
	// industry 字段不在当前 customers 表结构中；保留参数以兼容 API，暂不下推 SQL 过滤。
	_ = industry
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []CrossAccountSearchRow
	if err := q.Order("updated_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}