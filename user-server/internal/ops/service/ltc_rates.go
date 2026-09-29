package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
)

// LtcRatesService LTC 三率聚合层（T-P8-05，LTC-29/AC4）。
//
// 实时聚合、不落快照：看板每次打开都按当前库里三张表重算，
// 口径见 opsmodel.LtcRates 的注释（与 T-P8-06 SLO 文档同一版）。
type LtcRatesService struct {
	repo *opsrepo.LtcRatesRepository
	now  func() time.Time
}

// NewLtcRatesService 创建 LTC 三率服务（生产装配：全局连接 + 真实时间）。
func NewLtcRatesService() *LtcRatesService {
	return &LtcRatesService{repo: opsrepo.NewLtcRatesRepository(nil), now: time.Now}
}

// WithDB 换库（单测注入 testutil 的库）。返回自身，链式。
func (s *LtcRatesService) WithDB(db *gorm.DB) *LtcRatesService {
	if s == nil {
		return nil
	}
	s.repo = opsrepo.NewLtcRatesRepository(db)
	return s
}

// GetLtcRates 算三率。任一取数报错都整体返回 err（吞错回 0 的错法见
// conversion_funnel_opportunity_test.go 的教训：0 与"真的没有"逐字节相同）。
func (s *LtcRatesService) GetLtcRates(ctx context.Context) (*opsmodel.LtcRates, error) {
	byStatus, err := s.repo.CountOpportunitiesByStatus(ctx)
	if err != nil {
		return nil, err
	}
	billTotal, err := s.repo.SumBillsAmount(ctx)
	if err != nil {
		return nil, err
	}
	paymentTotal, err := s.repo.SumPaymentsAmount(ctx)
	if err != nil {
		return nil, err
	}
	openCount, err := s.repo.CountOpenOpportunities(ctx)
	if err != nil {
		return nil, err
	}
	overdueCount, err := s.repo.CountOverdueOpportunities(ctx, s.now())
	if err != nil {
		return nil, err
	}

	closed := float64(byStatus[sysmodel.OpportunityStatusWon] + byStatus[sysmodel.OpportunityStatusLost])
	total := float64(byStatus[sysmodel.OpportunityStatusOpen] +
		byStatus[sysmodel.OpportunityStatusWon] +
		byStatus[sysmodel.OpportunityStatusLost] +
		byStatus[sysmodel.OpportunityStatusCancelled])
	return &opsmodel.LtcRates{
		Closure:    rateOf(closed, total),
		Collection: rateOf(paymentTotal, billTotal),
		Overdue:    rateOf(float64(overdueCount), float64(openCount)),
	}, nil
}

// rateOf 除法守一处：分母为 0 时比率为 0（分子分母照实返，对账不断）。
func rateOf(numerator, denominator float64) opsmodel.RateValue {
	rate := 0.0
	if denominator != 0 {
		rate = numerator / denominator
	}
	return opsmodel.RateValue{Rate: rate, Numerator: numerator, Denominator: denominator}
}
