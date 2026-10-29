package egolite

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// repository_telemetry.go —— S-9 遥测的落库与窗口读取。
//
// 只有两件事：批量写入（入口已过 SanitizeTelemetry，这里不做第二次口径裁决——
// 双写口径必然漂移）、按时间窗拉窗口内样本（聚合在 service 层做，仓库只管取数）。

type EgoTelemetryRepository interface {
	CreateBatch(ctx context.Context, rows []*model.EgoliteTelemetry) error
	ListSince(ctx context.Context, since time.Time) ([]*model.EgoliteTelemetry, error)
}

type egoTelemetryRepository struct {
	db *gorm.DB
}

func NewEgoTelemetryRepositoryWithDB(db *gorm.DB) EgoTelemetryRepository {
	return &egoTelemetryRepository{db: db}
}

func (r *egoTelemetryRepository) CreateBatch(ctx context.Context, rows []*model.EgoliteTelemetry) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

func (r *egoTelemetryRepository) ListSince(ctx context.Context, since time.Time) ([]*model.EgoliteTelemetry, error) {
	var rows []*model.EgoliteTelemetry
	err := r.db.WithContext(ctx).
		Where("at >= ?", since).
		Order("at ASC").
		Limit(telemetryWindowMaxSamples + 1). // +1 让调用方能感知截断，不给静默假象
		Find(&rows).Error
	return rows, err
}
