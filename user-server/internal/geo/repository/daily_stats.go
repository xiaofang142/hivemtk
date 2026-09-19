package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"

	"gorm.io/gorm"
)

// GeoDailyStatRepository 每日聚合统计仓储（UPSERT 语义）
//
// 唯一键由表上的 idx_date_engine_intent 实现 (stat_date, engine, intent)。
// FirstOrCreate + Assign 冲突时更新。
type GeoDailyStatRepository interface {
	Upsert(ctx context.Context, stat *model.GeoDailyStat) error
	BatchUpsert(ctx context.Context, stats []*model.GeoDailyStat) error
	List(ctx context.Context, engine, funnelStage, startDate, endDate string, page, limit int) ([]*model.GeoDailyStat, int64, error)
	GetTrend(ctx context.Context, engine, funnelStage, intent string, days int) ([]*model.GeoDailyStat, error)
	DeleteBefore(ctx context.Context, before time.Time) error
}

type geoDailyStatRepo struct{ db *gorm.DB }

func NewGeoDailyStatRepository() GeoDailyStatRepository {
	return &geoDailyStatRepo{db: _db.GetDB()}
}
func NewGeoDailyStatRepositoryWithDB(db *gorm.DB) GeoDailyStatRepository {
	return &geoDailyStatRepo{db: db}
}

func (r *geoDailyStatRepo) Upsert(ctx context.Context, stat *model.GeoDailyStat) error {
	return r.db.WithContext(ctx).
		Where("stat_date = ? AND engine = ? AND intent = ?", stat.Date, stat.Engine, stat.Intent).
		Assign(model.GeoDailyStat{
			FunnelStage:              stat.FunnelStage,
			BrandMentionedCount:      stat.BrandMentionedCount,
			CompetitorMentionedCount: stat.CompetitorMentionedCount,
			CitationCount:            stat.CitationCount,
			NegativeCount:            stat.NegativeCount,
			ProbeCount:               stat.ProbeCount,
		}).
		FirstOrCreate(stat).Error
}

func (r *geoDailyStatRepo) BatchUpsert(ctx context.Context, stats []*model.GeoDailyStat) error {
	for _, s := range stats {
		if err := r.Upsert(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (r *geoDailyStatRepo) List(ctx context.Context, engine, funnelStage, startDate, endDate string, page, limit int) ([]*model.GeoDailyStat, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []*model.GeoDailyStat
	var total int64
	q := r.db.WithContext(ctx).Model(&model.GeoDailyStat{})
	if engine != "" {
		q = q.Where("engine = ?", engine)
	}
	if funnelStage != "" {
		q = q.Where("funnel_stage = ?", funnelStage)
	}
	if startDate != "" {
		q = q.Where("stat_date >= ?", startDate)
	}
	if endDate != "" {
		q = q.Where("stat_date <= ?", endDate)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Offset((page - 1) * limit).Limit(limit).Order("stat_date DESC").Find(&list).Error
	return list, total, err
}

func (r *geoDailyStatRepo) GetTrend(ctx context.Context, engine, funnelStage, intent string, days int) ([]*model.GeoDailyStat, error) {
	if days <= 0 {
		days = 30
	}
	// stat_date 是业务日口径的字符串键（由 scheduler 的 aggregateDailyStats 写入），
	// 窗口首也必须是业务日，否则 UTC 宿主上"近 N 天"会少/多一天。
	since := timeutil.BusinessDate(time.Now().AddDate(0, 0, -days))
	var list []*model.GeoDailyStat
	q := r.db.WithContext(ctx).Where("stat_date >= ?", since)
	if engine != "" {
		q = q.Where("engine = ?", engine)
	}
	if funnelStage != "" {
		q = q.Where("funnel_stage = ?", funnelStage)
	}
	if intent != "" {
		q = q.Where("intent = ?", intent)
	}
	err := q.Order("stat_date ASC").Find(&list).Error
	return list, err
}

func (r *geoDailyStatRepo) DeleteBefore(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).Where("stat_date < ?", timeutil.BusinessDate(before)).Delete(&model.GeoDailyStat{}).Error
}
