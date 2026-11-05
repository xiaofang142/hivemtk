package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoPushRecordRepository 蜘蛛推送记录仓储（append-only）
type GeoPushRecordRepository interface {
	BatchCreate(ctx context.Context, records []*model.GeoPushRecord) error
	// GoogleStats 返回窗口内 google 推送的成功/失败条数（按记录累加，非批次数）。
	GoogleStats(ctx context.Context, since time.Time) (success, fail int64, err error)
}

type geoPushRecordRepo struct{ db *gorm.DB }

func NewGeoPushRecordRepository() GeoPushRecordRepository {
	return &geoPushRecordRepo{db: _db.GetDB()}
}

// NewGeoPushRecordRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoPushRecordRepositoryWithDB(db *gorm.DB) GeoPushRecordRepository {
	return &geoPushRecordRepo{db: db}
}

// BatchCreate 一次推送的多条结果落库（按 200 行分批，避免单条 INSERT 过大）。
// tenant_id 由隔离 callback 逐行按 ctx 填充；on 模式下 ctx 未绑租户直接报错。
func (r *geoPushRecordRepo) BatchCreate(ctx context.Context, records []*model.GeoPushRecord) error {
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(records, 200).Error
}

func (r *geoPushRecordRepo) GoogleStats(ctx context.Context, since time.Time) (success, fail int64, err error) {
	var out struct {
		Success int64
		Fail    int64
	}
	err = r.db.WithContext(ctx).Model(&model.GeoPushRecord{}).
		Select("COALESCE(SUM(success_count),0) AS success, COALESCE(SUM(fail_count),0) AS fail").
		Where("platform = ? AND pushed_at >= ?", "google", since).
		Scan(&out).Error
	if err != nil {
		return 0, 0, err
	}
	return out.Success, out.Fail, nil
}
