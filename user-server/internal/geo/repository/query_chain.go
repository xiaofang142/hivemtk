package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"
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
	// BindOneIDToChain 把客户 OneID 回填到该链尚未绑定 OneID 的行（线索捕获后的归因关联）
	BindOneIDToChain(ctx context.Context, chainID, oneID string) (int64, error)
	// ListSince 列出近 N 天的思维链行（内容类型归因读侧；created_at 窗口）。
	ListSince(ctx context.Context, since time.Time) ([]*model.GeoQueryChain, error)
}

type geoQueryChainRepository struct{ db *gorm.DB }

// NewGeoQueryChainRepository 默认构造（全局句柄，仓库既有惯例；AV-04c 批次3 补齐，
// 供不持 *gorm.DB 的 service 装配点使用，句柄解析留在仓储层）。
func NewGeoQueryChainRepository() GeoQueryChainRepository {
	return &geoQueryChainRepository{db: _db.GetDB()}
}

// NewGeoQueryChainRepositoryWithDB 创建指定数据库连接的实例（用于测试/装配注入）。
func NewGeoQueryChainRepositoryWithDB(db *gorm.DB) GeoQueryChainRepository {
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
// 边界必须是业务日首：这是探针日预算护栏，若用宿主机时区的日期串，
// UTC 容器上算出的日子会比业务日晚一天，把昨日行计进今日，
// 预算被虚高的计数提前判满、探针无故停摆。
func (r *geoQueryChainRepository) CountToday(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoQueryChain{}).
		Where("created_at >= ?", timeutil.StartOfBusinessDay(time.Now())).
		Count(&n).Error
	return n, err
}

func (r *geoQueryChainRepository) BindOneIDToChain(ctx context.Context, chainID, oneID string) (int64, error) {
	if chainID == "" || oneID == "" {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.GeoQueryChain{}).
		Where("chain_id = ? AND (one_id = '' OR one_id IS NULL)", chainID).
		Update("one_id", oneID)
	return res.RowsAffected, res.Error
}

// ListSince 列出 created_at >= since 的思维链行（内容类型归因读侧）。
// 上限 50000 防全表进内存（窗口最大 90 天，正常探针量远低于此）。
func (r *geoQueryChainRepository) ListSince(ctx context.Context, since time.Time) ([]*model.GeoQueryChain, error) {
	var rows []*model.GeoQueryChain
	err := r.db.WithContext(ctx).
		Where("created_at >= ?", since).
		Order("created_at ASC").Limit(50000).Find(&rows).Error
	return rows, err
}

// GeoContentTaskRepository 补位任务仓储
type GeoContentTaskRepository interface {
	Create(ctx context.Context, task *model.GeoContentTask) error
	ListPending(ctx context.Context, limit int) ([]*model.GeoContentTask, error)
	MarkDone(ctx context.Context, id string) error
	// CountByStatus 按状态统计（报表漏斗用）
	CountByStatus(ctx context.Context, status string) (int64, error)
}

type geoContentTaskRepository struct{ db *gorm.DB }

// NewGeoContentTaskRepository 默认构造（全局句柄，仓库既有惯例；AV-04c 批次3 补齐）。
func NewGeoContentTaskRepository() GeoContentTaskRepository {
	return &geoContentTaskRepository{db: _db.GetDB()}
}

// NewGeoContentTaskRepositoryWithDB 创建指定数据库连接的实例（用于测试/装配注入）。
func NewGeoContentTaskRepositoryWithDB(db *gorm.DB) GeoContentTaskRepository {
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

// MarkDone 标记任务完成；0 行说明任务不存在（含已软删/跨租户），不能当作成功。
func (r *geoContentTaskRepository) MarkDone(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Model(&model.GeoContentTask{}).
		Where("id = ?", id).Update("status", "done")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoContentTaskRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoContentTask{}).
		Where("status = ?", status).Count(&n).Error
	return n, err
}
