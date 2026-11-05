package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/reporting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GeoDailyStatRepository 每日聚合统计仓储（UPSERT 语义）
//
// 唯一键由表上的 idx_date_engine_intent 实现 (stat_date, engine, intent)。
// FirstOrCreate + Assign 冲突时更新。
type GeoDailyStatRepository interface {
	Upsert(ctx context.Context, stat *model.GeoDailyStat) error
	BatchUpsert(ctx context.Context, stats []*model.GeoDailyStat) error
	// IncrementByEngine 按 (stat_date, engine, "mixed", "mixed") 累加引用数
	IncrementByEngine(ctx context.Context, statDate, engine string, citations, brandMentions int) error
	// SetCitationByEngine 按 (stat_date, engine, "mixed", "mixed") 覆盖引用数为快照值。
	// 重复触发 verify-all 时必须用覆盖，不能用累加——聚合源是「全量已引用行」，
	// 累加会让同一天的引用数随刷新次数翻倍。
	SetCitationByEngine(ctx context.Context, statDate, engine string, citations int) error
	List(ctx context.Context, engine, funnelStage, startDate, endDate string, page, limit int) ([]*model.GeoDailyStat, int64, error)
	GetTrend(ctx context.Context, engine, funnelStage, intent string, days int) ([]*model.GeoDailyStat, error)
}

type geoDailyStatRepo struct{ db *gorm.DB }

func NewGeoDailyStatRepository() GeoDailyStatRepository {
	return &geoDailyStatRepo{db: _db.GetDB()}
}
func NewGeoDailyStatRepositoryWithDB(db *gorm.DB) GeoDailyStatRepository {
	return &geoDailyStatRepo{db: db}
}

func (r *geoDailyStatRepo) Upsert(ctx context.Context, stat *model.GeoDailyStat) error {
	// 不用 FirstOrCreate：SELECT 后 INSERT 在并发下会撞唯一键（23505）。
	// 冲突目标与表上的唯一键一致（Normalize 后的 (tenant_id, stat_date, engine, intent)）；
	// tenant_id 由隔离 callback 在 INSERT 前填好。
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "stat_date"}, {Name: "engine"}, {Name: "intent"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"funnel_stage", "brand_mentioned_count", "competitor_mentioned_count",
			"citation_count", "negative_count", "probe_count",
		}),
	}).Create(stat).Error
}

func (r *geoDailyStatRepo) IncrementByEngine(ctx context.Context, statDate, engine string, citations, brandMentions int) error {
	stat := &model.GeoDailyStat{
		Date:                statDate,
		Engine:              engine,
		Intent:              "mixed",
		FunnelStage:         "mixed",
		CitationCount:       citations,
		BrandMentionedCount: brandMentions,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "stat_date"}, {Name: "engine"}, {Name: "intent"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"citation_count":        gorm.Expr("geo_daily_stats.citation_count + EXCLUDED.citation_count"),
			"brand_mentioned_count": gorm.Expr("geo_daily_stats.brand_mentioned_count + EXCLUDED.brand_mentioned_count"),
		}),
	}).Create(stat).Error
}

func (r *geoDailyStatRepo) SetCitationByEngine(ctx context.Context, statDate, engine string, citations int) error {
	stat := &model.GeoDailyStat{
		Date:          statDate,
		Engine:        engine,
		Intent:        "mixed",
		FunnelStage:   "mixed",
		CitationCount: citations,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "stat_date"}, {Name: "engine"}, {Name: "intent"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"citation_count": gorm.Expr("EXCLUDED.citation_count"),
		}),
	}).Create(stat).Error
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
		days = reporting.DefaultWindowDays(ctx)
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
