package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SearchQuerySummary GSC 窗口汇总。
type SearchQuerySummary struct {
	Clicks      int64   `json:"clicks"`
	Impressions int64   `json:"impressions"`
	CTR         float64 `json:"ctr"`
	Position    float64 `json:"position"`
	RowCount    int64   `json:"rows" gorm:"column:row_count"`
}

// SearchQueryRow 查询维度明细。
type SearchQueryRow struct {
	Query       string  `json:"query"`
	Page        string  `json:"page"`
	Clicks      int64   `json:"clicks"`
	Impressions int64   `json:"impressions"`
	CTR         float64 `json:"ctr"`
	Position    float64 `json:"position"`
}

// GeoSearchQueryRepository GSC 性能数据仓储。
type GeoSearchQueryRepository interface {
	// UpsertBatch 幂等导入：(tenant_id,row_key) 冲突时更新指标。
	UpsertBatch(ctx context.Context, rows []*model.GeoSearchQuery) (int, error)
	Summary(ctx context.Context, from, to time.Time) (SearchQuerySummary, error)
	TopQueries(ctx context.Context, from, to time.Time, limit int) ([]SearchQueryRow, error)
}

type geoSearchQueryRepo struct{ db *gorm.DB }

func NewGeoSearchQueryRepository() GeoSearchQueryRepository {
	return &geoSearchQueryRepo{db: _db.GetDB()}
}

func NewGeoSearchQueryRepositoryWithDB(db *gorm.DB) GeoSearchQueryRepository {
	return &geoSearchQueryRepo{db: db}
}

func (r *geoSearchQueryRepo) UpsertBatch(ctx context.Context, rows []*model.GeoSearchQuery) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "row_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"stat_date", "query", "page", "clicks", "impressions", "ctr", "position", "source", "updated_at",
		}),
	}).CreateInBatches(rows, 200).Error
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (r *geoSearchQueryRepo) Summary(ctx context.Context, from, to time.Time) (SearchQuerySummary, error) {
	var out SearchQuerySummary
	err := r.db.WithContext(ctx).Model(&model.GeoSearchQuery{}).
		Select(`COALESCE(SUM(clicks),0) AS clicks,
			COALESCE(SUM(impressions),0) AS impressions,
			COALESCE(SUM(clicks)::float / NULLIF(SUM(impressions),0),0) AS ctr,
			COALESCE(AVG(position),0) AS position,
			COUNT(*) AS row_count`).
		Where("stat_date >= ? AND stat_date <= ?", from, to).
		Scan(&out).Error
	return out, err
}

func (r *geoSearchQueryRepo) TopQueries(ctx context.Context, from, to time.Time, limit int) ([]SearchQueryRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	var out []SearchQueryRow
	err := r.db.WithContext(ctx).Model(&model.GeoSearchQuery{}).
		Select(`query,
			MIN(page) AS page,
			COALESCE(SUM(clicks),0) AS clicks,
			COALESCE(SUM(impressions),0) AS impressions,
			COALESCE(SUM(clicks)::float / NULLIF(SUM(impressions),0),0) AS ctr,
			COALESCE(AVG(position),0) AS position`).
		Where("stat_date >= ? AND stat_date <= ?", from, to).
		Group("query").
		Order("clicks DESC, query ASC").
		Limit(limit).
		Scan(&out).Error
	return out, err
}
