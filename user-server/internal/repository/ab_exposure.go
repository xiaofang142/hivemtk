package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// AbExposure AB 曝光/转化记录（表 ab_exposures）。
// service 层禁止直接持有 gorm.DB，持久化统一收敛到 repository。
//
// EC-28 / M28（营销实验/控制组/归因与证据，不污染控制组）不变量：
//   - 本表是「实验曝光/转化证据」，不是营销受众名单；control 变体仅用于对照统计，
//     绝不能据此向 control 变体追加营销派发（控制组不新增营销派发）；
//   - 身份合并前的曝光记录按 BR04 保留并标「污染」，合并后不静默抹除历史曝光；
//   - 实时撤回/撤权必须覆盖冻结快照：已落库的曝光证据不可被后续重跑改写；
//   - 重复触发/换渠道只能追加新曝光行，不能突破压力上限或把 control 组计入 treatment；
//   - 归因/漏斗仅消费真实回执与已付订单净额，曝光/点击不冒充因果增长（EC-31）。
type AbExposure struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint       `gorm:"primarykey" json:"id"`
	ExperimentID string     `gorm:"size:64;index:idx_ab_exp_customer,priority:1" json:"experiment_id"`
	CustomerID   string     `gorm:"size:64;index:idx_ab_exp_customer,priority:2" json:"customer_id"`
	Variant      string     `gorm:"size:16" json:"variant"`
	SessionID    string     `gorm:"size:64" json:"session_id"`
	ExposedAt    time.Time  `json:"exposed_at"`
	ConvertedAt  *time.Time `json:"converted_at"`
}

func (AbExposure) TableName() string { return "ab_exposures" }

// ABExposureRepository AB 曝光记录仓库
type ABExposureRepository struct {
	db *gorm.DB
}

func NewABExposureRepositoryWithDB(db *gorm.DB) *ABExposureRepository {
	return &ABExposureRepository{db: db}
}

func (r *ABExposureRepository) Create(ctx context.Context, e *AbExposure) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *ABExposureRepository) MarkConversion(ctx context.Context, expID, customerID string, at time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&AbExposure{}).
		Where("experiment_id = ? AND customer_id = ? AND converted_at IS NULL", expID, customerID).
		Update("converted_at", at).Error
}

func (r *ABExposureRepository) ListSince(ctx context.Context, expID string, since time.Time) ([]AbExposure, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var rows []AbExposure
	err := r.db.WithContext(ctx).
		Where("experiment_id = ? AND exposed_at >= ?", expID, since).
		Find(&rows).Error
	return rows, err
}
