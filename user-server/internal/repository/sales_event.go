// Package repository 销售事件流数据访问层（H2 技术债修复）。
//
// 严格遵循五层架构：Service 层不直接访问 DB，一律经由本仓库。
package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// SalesEventRepository 销售事件仓库接口
type SalesEventRepository interface {
	Create(ctx context.Context, ev *model.SalesEvent) error
	// ListByType 按事件类型查询；ownerID 为空表示不限销售，since 为零值表示不限起始时间
	ListByType(ctx context.Context, eventType, ownerID string, sinceUnix int64) ([]*model.SalesEvent, error)
	// ListByOpportunityID T-P8-01 首个生产读口：按 opportunity_id 取全链路事件，
	// occurred_at ASC、id ASC（同秒并列由自增 id 破序，链序 = 写入序）。
	ListByOpportunityID(ctx context.Context, opportunityID string) ([]*model.SalesEvent, error)
}

type salesEventRepo struct {
	db *gorm.DB
}

// NewSalesEventRepository 创建销售事件仓库实例
func NewSalesEventRepository() SalesEventRepository {
	return &salesEventRepo{db: _db.GetDB()}
}

// NewSalesEventRepositoryWithDB 创建指定数据库连接的销售事件仓库实例（用于测试）
func NewSalesEventRepositoryWithDB(db *gorm.DB) SalesEventRepository {
	return &salesEventRepo{db: db}
}

func (r *salesEventRepo) Create(ctx context.Context, ev *model.SalesEvent) error {
	return r.db.Create(ev).Error
}

func (r *salesEventRepo) ListByOpportunityID(ctx context.Context, opportunityID string) ([]*model.SalesEvent, error) {
	list := make([]*model.SalesEvent, 0)
	if err := r.db.WithContext(ctx).Model(&model.SalesEvent{}).
		Where("opportunity_id = ?", opportunityID).
		Order("occurred_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *salesEventRepo) ListByType(ctx context.Context, eventType, ownerID string, sinceUnix int64) ([]*model.SalesEvent, error) {
	list := make([]*model.SalesEvent, 0)
	q := r.db.WithContext(ctx).Model(&model.SalesEvent{}).Where("event_type = ?", eventType)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	if sinceUnix > 0 {
		q = q.Where("occurred_at >= to_timestamp(?)", sinceUnix)
	}
	if err := q.Order("occurred_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
