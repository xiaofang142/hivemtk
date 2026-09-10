package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MsgHourlyDelta 一个 (bucket, merchant, platform) 维度上的增量计数。
// 由 service 层从 message_hub 原始行内存聚合而来，repo 负责原子 upsert。
type MsgHourlyDelta struct {
	HourBucket   time.Time
	MerchantID   uint
	Platform     string
	SessionCount int64
	AICount      int64
	HumanCount   int64
	MessageCount int64
}

// MessageHubSummaryRepository message_hub 小时汇总（msg_hourly_summary）仓库接口。
//
// 五层架构归属：L3 仓库层，封装全部 SQL；service 不直接持有 *gorm.DB。
type MessageHubSummaryRepository interface {
	// LoadWatermark 读取水位线；不存在时返回 0（无错误）。
	LoadWatermark(ctx context.Context, source string) (int64, error)
	// UpsertIncrementBatch 在单事务内完成：
	//  1. 按 PK (bucket+merchant+platform) 增量 upsert（计数累加）；
	//  2. 推进 watermark 至 newWatermark。
	// 同事务推进保证「不丢不重」：进程崩溃/重跑均幂等。
	UpsertIncrementBatch(ctx context.Context, source string, newWatermark int64, deltas []MsgHourlyDelta) error
	// QuerySince 查询 bucket >= since 的 summary 行（驾驶舱双读之主路径）。
	QuerySince(ctx context.Context, since time.Time) ([]model.MsgHourlySummary, error)
	// LatestBucket 返回最新 hour_bucket；表空时返回 nil。
	LatestBucket(ctx context.Context) (*time.Time, error)
	// LatestUpdate 返回最近一次聚合刷新时间 MAX(updated_at)；表空返回 nil。
	// 用于 X-8 陈旧判定（hour_bucket 是小时粒度，不能反映聚合任务新鲜度）。
	LatestUpdate(ctx context.Context) (*time.Time, error)
	// LoadBatchSince 按水位线分批拉取原始消息（id > since，升序，limit 上限）。
	// 供聚合服务消费；返回空 slice 表示已无新数据。
	LoadBatchSince(ctx context.Context, since int64, limit int) ([]model.MessageHub, error)
}

type msgHourlySummaryRepo struct {
	db *gorm.DB
}

func NewMessageHubSummaryRepository(db *gorm.DB) MessageHubSummaryRepository {
	return &msgHourlySummaryRepo{db: db}
}

func (r *msgHourlySummaryRepo) LoadWatermark(ctx context.Context, source string) (int64, error) {
	if r.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var wm model.AggregationWatermark
	err := r.db.WithContext(ctx).
		Where("source = ?", source).
		First(&wm).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return wm.LastEventID, nil
}

const upsertIncrementSQL = `
	INSERT INTO msg_hourly_summary
		(hour_bucket, merchant_id, platform, session_count, ai_count, human_count, message_count, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, NOW())
	ON CONFLICT (hour_bucket, merchant_id, platform) DO UPDATE SET
		session_count = msg_hourly_summary.session_count + EXCLUDED.session_count,
		ai_count      = msg_hourly_summary.ai_count + EXCLUDED.ai_count,
		human_count   = msg_hourly_summary.human_count + EXCLUDED.human_count,
		message_count = msg_hourly_summary.message_count + EXCLUDED.message_count,
		updated_at    = NOW()`

func (r *msgHourlySummaryRepo) UpsertIncrementBatch(ctx context.Context, source string, newWatermark int64, deltas []MsgHourlyDelta) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	if len(deltas) == 0 {
		return r.db.WithContext(ctx).Model(&model.AggregationWatermark{}).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source"}},
				DoUpdates: clause.Assignments(map[string]any{"last_event_id": newWatermark, "updated_at": time.Now()}),
			}).
			Create(&model.AggregationWatermark{Source: source, LastEventID: newWatermark}).Error
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range deltas {
			if err := tx.Exec(upsertIncrementSQL,
				d.HourBucket, d.MerchantID, d.Platform,
				d.SessionCount, d.AICount, d.HumanCount, d.MessageCount,
			).Error; err != nil {
				return err
			}
		}

		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source"}},
			DoUpdates: clause.Assignments(map[string]any{"last_event_id": newWatermark, "updated_at": time.Now()}),
		}).Create(&model.AggregationWatermark{Source: source, LastEventID: newWatermark}).Error
	})
}

func (r *msgHourlySummaryRepo) QuerySince(ctx context.Context, since time.Time) ([]model.MsgHourlySummary, error) {
	rows := make([]model.MsgHourlySummary, 0)
	err := r.db.WithContext(ctx).
		Where("hour_bucket >= ?", since).
		Order("hour_bucket ASC").
		Find(&rows).Error
	return rows, err
}

func (r *msgHourlySummaryRepo) LatestBucket(ctx context.Context) (*time.Time, error) {
	var row struct {
		Latest *time.Time
	}
	err := r.db.WithContext(ctx).
		Model(&model.MsgHourlySummary{}).
		Select("MAX(hour_bucket) AS latest").
		Scan(&row).Error
	return row.Latest, err
}

func (r *msgHourlySummaryRepo) LatestUpdate(ctx context.Context) (*time.Time, error) {
	var row struct {
		Latest *time.Time
	}
	err := r.db.WithContext(ctx).
		Model(&model.MsgHourlySummary{}).
		Select("MAX(updated_at) AS latest").
		Scan(&row).Error
	return row.Latest, err
}

func (r *msgHourlySummaryRepo) LoadBatchSince(ctx context.Context, since int64, limit int) ([]model.MessageHub, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if limit <= 0 {
		limit = 50000
	}
	rows := make([]model.MessageHub, 0, limit)
	err := r.db.WithContext(ctx).
		Model(&model.MessageHub{}).
		Where("id > ?", since).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
