package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// AuditRunQuery 审计历史查询条件。
type AuditRunQuery struct {
	Kind          string
	URL           string
	RubricVersion string
	Page          int
	Limit         int
}

// GeoAuditRepository 审计 run/item 仓储。
//
// run 与 item 必须同事务写入：报告页可以容忍 item 缺失（退化为 categories
// 快照），但 compare 依赖 item，半条 run 会让两次对比静默少项。
type GeoAuditRepository interface {
	Create(ctx context.Context, run *model.GeoAuditRun, items []*model.GeoAuditItem) error
	List(ctx context.Context, q AuditRunQuery) ([]*model.GeoAuditRun, int64, error)
	GetByID(ctx context.Context, id string) (*model.GeoAuditRun, []*model.GeoAuditItem, error)
	// Previous 取同 kind/url/rubric_version 下、早于给定 run 的最近一次。
	// 没有上一次时返回 gorm.ErrRecordNotFound（调用方据此判“无基线”）。
	Previous(ctx context.Context, run *model.GeoAuditRun) (*model.GeoAuditRun, error)
	LatestByURL(ctx context.Context, kind, url, rubricVersion string) (*model.GeoAuditRun, error)
}

type geoAuditRepo struct{ db *gorm.DB }

func NewGeoAuditRepository() GeoAuditRepository {
	return &geoAuditRepo{db: _db.GetDB()}
}

// NewGeoAuditRepositoryWithDB 显式注入 gormDB（路由装配/测试用）。
func NewGeoAuditRepositoryWithDB(db *gorm.DB) GeoAuditRepository {
	return &geoAuditRepo{db: db}
}

func (r *geoAuditRepo) Create(ctx context.Context, run *model.GeoAuditRun, items []*model.GeoAuditItem) error {
	if run == nil {
		return errors.New("审计记录不能为空")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, it := range items {
			it.RunID = run.ID
		}
		return tx.CreateInBatches(items, 200).Error
	})
}

func (r *geoAuditRepo) List(ctx context.Context, q AuditRunQuery) ([]*model.GeoAuditRun, int64, error) {
	page, limit := q.Page, q.Limit
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	dbq := r.db.WithContext(ctx).Model(&model.GeoAuditRun{})
	if q.Kind != "" {
		dbq = dbq.Where("kind = ?", q.Kind)
	}
	if q.URL != "" {
		dbq = dbq.Where("url = ?", q.URL)
	}
	if q.RubricVersion != "" {
		dbq = dbq.Where("rubric_version = ?", q.RubricVersion)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.GeoAuditRun
	err := dbq.Order("created_at DESC, id DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&list).Error
	return list, total, err
}

func (r *geoAuditRepo) GetByID(ctx context.Context, id string) (*model.GeoAuditRun, []*model.GeoAuditItem, error) {
	var run model.GeoAuditRun
	if err := r.db.WithContext(ctx).First(&run, "id = ?", id).Error; err != nil {
		return nil, nil, err
	}
	var items []*model.GeoAuditItem
	if err := r.db.WithContext(ctx).
		Where("run_id = ?", id).
		Order("sort_order ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, nil, err
	}
	return &run, items, nil
}

func (r *geoAuditRepo) Previous(ctx context.Context, run *model.GeoAuditRun) (*model.GeoAuditRun, error) {
	if run == nil {
		return nil, errors.New("基准审计记录不能为空")
	}
	var prev model.GeoAuditRun
	err := r.db.WithContext(ctx).
		Where("kind = ? AND url = ? AND rubric_version = ?", run.Kind, run.URL, run.RubricVersion).
		Where("created_at < ? OR (created_at = ? AND id < ?)", run.CreatedAt, run.CreatedAt, run.ID).
		Order("created_at DESC, id DESC").
		First(&prev).Error
	if err != nil {
		return nil, err
	}
	return &prev, nil
}

func (r *geoAuditRepo) LatestByURL(ctx context.Context, kind, url, rubricVersion string) (*model.GeoAuditRun, error) {
	var run model.GeoAuditRun
	err := r.db.WithContext(ctx).
		Where("kind = ? AND url = ? AND rubric_version = ?", kind, url, rubricVersion).
		Order("created_at DESC, id DESC").
		First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}
