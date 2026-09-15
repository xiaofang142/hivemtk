package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/churn"
)

// ChurnScoreService 周批流失评分
type ChurnScoreService struct {
	repo *repository.ChurnScoreRepository

	statsFn func(ctx context.Context) ([]ChurnCustomerStats, error)

	horizonDays float64
}

// ChurnCustomerStats 单客户购买时间戳聚合（中间态，单位秒时间戳）
type ChurnCustomerStats struct {
	CustomerKey string
	PurchaseAts []time.Time
}

// NewChurnScoreService 构造
func NewChurnScoreService(db *gorm.DB) *ChurnScoreService {
	return &ChurnScoreService{
		repo:        repository.NewChurnScoreRepository(db),
		statsFn:     defaultChurnStatsQuery,
		horizonDays: 30,
	}
}

// ComputeAll 全量重算并 upsert。返回写入行数。
func (s *ChurnScoreService) ComputeAll(ctx context.Context) (int, error) {
	statsList, err := s.statsFn(ctx)
	if err != nil {
		return 0, fmt.Errorf("churn 订单聚合查询失败: %w", err)
	}
	if len(statsList) == 0 {
		logger.Info("[ChurnScore] 无订单数据，本轮空跑（等订单数据接入后自动生效）")
		return 0, nil
	}

	type row struct {
		key string
		cs  churn.CustomerStats
	}
	rows := make([]row, 0, len(statsList))
	for _, st := range statsList {
		if len(st.PurchaseAts) == 0 {
			continue
		}
		sorted := append([]time.Time(nil), st.PurchaseAts...)
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && sorted[j].Before(sorted[j-1]); j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
		first, last := sorted[0], sorted[len(sorted)-1]
		T := time.Since(first).Hours() / 24
		if T <= 0 {
			continue
		}
		cs := churn.CustomerStats{X: float64(len(sorted) - 1), T: T}
		if cs.X > 0 {
			cs.Tx = last.Sub(first).Hours() / 24
		}
		rows = append(rows, row{key: st.CustomerKey, cs: cs})
	}
	if len(rows) == 0 {
		return 0, nil
	}

	stats := make([]churn.CustomerStats, len(rows))
	for i := range rows {
		stats[i] = rows[i].cs
	}
	fitRes := churn.Fit(churn.FitInput{Stats: stats})
	paramsJSON, _ := json.Marshal(fitRes.Params)

	out := make([]model.ChurnScore, 0, len(rows))
	now := time.Now()
	for i := range rows {
		pAlive := churn.PAlive(fitRes.Params, rows[i].cs.X, rows[i].cs.Tx, rows[i].cs.T)
		e30 := churn.ConditionalExpectedPurchases(fitRes.Params, rows[i].cs.X, rows[i].cs.Tx, rows[i].cs.T, s.horizonDays)
		out = append(out, model.ChurnScore{
			CustomerKey:          rows[i].key,
			X:                    int(rows[i].cs.X),
			Tx:                   rows[i].cs.Tx,
			TObs:                 rows[i].cs.T,
			PAlive:               pAlive,
			ExpectedPurchases30d: e30,
			Params:               string(paramsJSON),
			StatsCount:           len(stats),
			ComputedAt:           now,
		})
	}
	if err := s.repo.UpsertBatch(ctx, out); err != nil {
		return 0, fmt.Errorf("churn_scores upsert 失败: %w", err)
	}
	logger.Infof("[ChurnScore] 周批完成: 客户 %d, params={r:%.4f α:%.4f a:%.4f b:%.4f} converged=%v",
		len(out), fitRes.Params.R, fitRes.Params.Alpha, fitRes.Params.A, fitRes.Params.B, fitRes.Converged)
	return len(out), nil
}

func defaultChurnStatsQuery(ctx context.Context) ([]ChurnCustomerStats, error) {
	return nil, nil
}

// ChurnScoreCron 周批调度（每周一 05:00 CST，避开 RFM 04:00 高峰）
type ChurnScoreCron struct {
	svc *ChurnScoreService

	stop      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
}

func NewChurnScoreCron(svc *ChurnScoreService) *ChurnScoreCron {
	return &ChurnScoreCron{svc: svc, stop: make(chan struct{})}
}

func (c *ChurnScoreCron) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		c.wg.Add(1)
		go c.loop(ctx)
		logger.Info("[ChurnScoreCron] 已启动（每周一 05:00 CST 全量重算 BG/NBD 流失评分）")
	})
}

func (c *ChurnScoreCron) Stop(_ context.Context) {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	c.wg.Wait()
}

func (c *ChurnScoreCron) loop(ctx context.Context) {
	defer c.wg.Done()
	cst := time.FixedZone("CST", 8*3600)
	for {
		now := time.Now().In(cst)

		next := now.Add(24 * time.Hour)
		for next.Weekday() != time.Monday {
			next = next.Add(24 * time.Hour)
		}
		next = time.Date(next.Year(), next.Month(), next.Day(), 5, 0, 0, 0, cst)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-c.stop:
			timer.Stop()
			return
		case <-timer.C:
			if _, err := c.svc.ComputeAll(ctx); err != nil {
				logger.Errorf("[ChurnScoreCron] 周批失败: %v", err)
			}
		}
	}
}
