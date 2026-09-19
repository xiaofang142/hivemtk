package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/timeutil"

	"gorm.io/gorm"
)

// GeoQueryChainRepository 查询思维链仓储
type GeoQueryChainRepository interface {
	Append(ctx context.Context, chain *model.GeoQueryChain) error
	ListByChain(ctx context.Context, chainID string) ([]*model.GeoQueryChain, error)
	CountByChain(ctx context.Context, chainID string) (int64, error)
	// ListByOneID 列出已绑定该客户(OneID)的思维链行（inbox 回填定位用）
	ListByOneID(ctx context.Context, oneID string) ([]*model.GeoQueryChain, error)
	// CountToday 统计当日新增行数（探针日预算护栏；按 created_at 而非 chain_id）
	CountToday(ctx context.Context) (int64, error)
}

type geoQueryChainRepository struct{ db *gorm.DB }

func NewGeoQueryChainRepository(db *gorm.DB) GeoQueryChainRepository {
	return &geoQueryChainRepository{db: db}
}

func (r *geoQueryChainRepository) Append(ctx context.Context, chain *model.GeoQueryChain) error {
	return r.db.WithContext(ctx).Create(chain).Error
}

func (r *geoQueryChainRepository) ListByChain(ctx context.Context, chainID string) ([]*model.GeoQueryChain, error) {
	var rows []*model.GeoQueryChain
	err := r.db.WithContext(ctx).
		Where("chain_id = ?", chainID).
		Order("seq ASC").
		Find(&rows).Error
	return rows, err
}

func (r *geoQueryChainRepository) CountByChain(ctx context.Context, chainID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoQueryChain{}).
		Where("chain_id = ?", chainID).Count(&n).Error
	return n, err
}

func (r *geoQueryChainRepository) ListByOneID(ctx context.Context, oneID string) ([]*model.GeoQueryChain, error) {
	var rows []*model.GeoQueryChain
	err := r.db.WithContext(ctx).
		Where("one_id = ? AND one_id != ''", oneID).
		Order("created_at DESC").Limit(50).
		Find(&rows).Error
	return rows, err
}

// CountToday 统计业务日（CST）当日新增行数。
//
// 边界必须是业务日首：这是探针日预算护栏，原先用宿主机时区的日期串，
// UTC 容器在 16:00–23:59（=CST 次日 00:00–07:59）之间算出的日子比业务日**晚一天**，
// 于是窗口起点前移 24h、把昨日的行计进今日 ⇒ 预算被虚高的计数提前判满，探针无故停摆。
func (r *geoQueryChainRepository) CountToday(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoQueryChain{}).
		Where("created_at >= ?", timeutil.StartOfBusinessDay(time.Now())).
		Count(&n).Error
	return n, err
}

// GeoContentTaskRepository 补位任务仓储
type GeoContentTaskRepository interface {
	Create(ctx context.Context, task *model.GeoContentTask) error
	ListPending(ctx context.Context, limit int) ([]*model.GeoContentTask, error)
	MarkDone(ctx context.Context, id string) error
	// CountByStatus 按状态统计（L4 报表漏斗用）
	CountByStatus(ctx context.Context, status string) (int64, error)
}

type geoContentTaskRepository struct{ db *gorm.DB }

func NewGeoContentTaskRepository(db *gorm.DB) GeoContentTaskRepository {
	return &geoContentTaskRepository{db: db}
}

func (r *geoContentTaskRepository) Create(ctx context.Context, task *model.GeoContentTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *geoContentTaskRepository) ListPending(ctx context.Context, limit int) ([]*model.GeoContentTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []*model.GeoContentTask
	err := r.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *geoContentTaskRepository) MarkDone(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&model.GeoContentTask{}).
		Where("id = ?", id).Update("status", "done").Error
}

func (r *geoContentTaskRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoContentTask{}).
		Where("status = ?", status).Count(&n).Error
	return n, err
}
