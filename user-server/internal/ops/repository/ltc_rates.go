package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// LtcRatesRepository LTC 三率取数层（T-P8-05）。
//
// 只做四件事：按状态数商机、加总应收（剔作废）、加总回款（只认 confirmed）、
// 数逾期商机。比率的除法在 service 层（分母为 0 的形状那里判）。
type LtcRatesRepository struct {
	db *gorm.DB
}

// NewLtcRatesRepository 创建 LTC 三率仓储。db 传 nil 时走全局连接（生产装配用法）；
// 单测传 testutil 的库（可注入，T-P4-06 同一口径）。
func NewLtcRatesRepository(db *gorm.DB) *LtcRatesRepository {
	if db == nil {
		db = _db.GetDB()
	}
	return &LtcRatesRepository{db: db}
}

// CountOpportunitiesByStatus 按 status 分组数商机行（opportunities 表无软删列，全表即全量）。
func (r *LtcRatesRepository) CountOpportunitiesByStatus(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&sysmodel.Opportunity{}).
		Select("status, COUNT(*) AS count").
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, v := range rows {
		out[v.Status] = v.Count
	}
	return out, nil
}

// SumBillsAmount 应收总额：voided（误建作废）的不该收，分母剔除。
func (r *LtcRatesRepository) SumBillsAmount(ctx context.Context) (float64, error) {
	var total float64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Bill{}).
		Where("status <> ?", sysmodel.BillStatusVoided).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&total).Error
	return total, err
}

// SumPaymentsAmount 回款总额：只认 PaymentStatusesCounted（今天只有 confirmed），
// reversed（冲销）的不算数。
func (r *LtcRatesRepository) SumPaymentsAmount(ctx context.Context) (float64, error) {
	var total float64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Payment{}).
		Where("status IN ?", sysmodel.PaymentStatusesCounted).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&total).Error
	return total, err
}

// CountOpenOpportunities open 商机数（逾期率分母）。
func (r *LtcRatesRepository) CountOpenOpportunities(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Opportunity{}).
		Where("status = ?", sysmodel.OpportunityStatusOpen).
		Count(&n).Error
	return n, err
}

// CountOverdueOpportunities 逾期商机数：open 且定了关单日、关单日已过。
// 没定关单日的（NULL）不算逾期——"没承诺"不等于"失约"。
func (r *LtcRatesRepository) CountOverdueOpportunities(ctx context.Context, now time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.Opportunity{}).
		Where("status = ?", sysmodel.OpportunityStatusOpen).
		Where("expected_close_at IS NOT NULL").
		Where("expected_close_at < ?", now).
		Count(&n).Error
	return n, err
}
