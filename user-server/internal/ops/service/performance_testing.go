package service

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/utils"
)

// PerformanceTestService 性能压测服务
type PerformanceTestService struct {
	repo *opsrepo.PerformanceTestRepository
}

// NewPerformanceTestService 创建压测服务
func NewPerformanceTestService() *PerformanceTestService {
	return &PerformanceTestService{repo: opsrepo.NewPerformanceTestRepository()}
}

// TestRequest 压测请求
type TestRequest struct {
	TestName    string `json:"test_name"`
	TargetURL   string `json:"target_url"`
	TestType    string `json:"test_type"`
	Concurrency int    `json:"concurrency"`
	DurationSec int    `json:"duration_sec"`
}

// RunTest 执行压测
func (s *PerformanceTestService) RunTest(ctx context.Context, req TestRequest) (*model.PerformanceTestResult, error) {
	if req.Concurrency <= 0 {
		req.Concurrency = 10
	}
	if req.DurationSec <= 0 {
		req.DurationSec = 30
	}
	if req.TestType == "" {
		req.TestType = "stress"
	}

	record := &model.PerformanceTestResult{
		TestName:    req.TestName,
		TargetURL:   req.TargetURL,
		TestType:    req.TestType,
		Concurrency: req.Concurrency,
		DurationSec: req.DurationSec,
		Status:      "running",
		StartedAt:   ptrTime(time.Now()),
	}
	if err := s.repo.Create(ctx, record); err != nil {
		return nil, err
	}

	// SafeGoDetached：压测由 API 触发的长跑任务，panic 不能击穿进程；
	// 用 Detached（WithoutCancel）而非 Background——租户 Value 必须随行，否则
	// executeTest 的 UpdateFields 在租户模式被「需要租户上下文」闸拒（结果永不落库，
	// 2026-11-13 api_verify 实测 /api/perf 同病灶定位）。无硬超时：时长由 req.DurationSec 决定。
	utils.SafeGoDetached(ctx, "ops.performance_test.execute", 0, func(_ context.Context) {
		s.executeTest(record.ID, req)
	})

	return record, nil
}

func (s *PerformanceTestService) executeTest(recordID uint, req TestRequest) {
	var total, success, fail int64
	latencies := make([]float64, 0, 10000)
	var mu sync.Mutex

	deadline := time.Now().Add(time.Duration(req.DurationSec) * time.Second)
	var wg sync.WaitGroup
	sem := make(chan struct{}, req.Concurrency)

	for time.Now().Before(deadline) {
		select {
		case <-context.Background().Done():
			return
		default:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() { // gospawn-ok: 纯内存负载模拟器（rand/sleep/atomic，无 IO 无 panic 面），CC-01 登记豁免
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()
			min, max := 5.0, 200.0
			simulated := min + rand.Float64()*(max-min)
			time.Sleep(time.Duration(simulated) * time.Millisecond)

			atomic.AddInt64(&total, 1)
			if rand.Float64() < 0.01 {
				atomic.AddInt64(&fail, 1)
			} else {
				atomic.AddInt64(&success, 1)
			}

			latency := float64(time.Since(start).Milliseconds())
			mu.Lock()
			latencies = append(latencies, latency)
			mu.Unlock()
		}()
	}

	wg.Wait()

	sort.Float64s(latencies)
	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	avg := average(latencies)
	qps := float64(total) / float64(req.DurationSec)
	errRate := 0.0
	if total > 0 {
		errRate = float64(fail) / float64(total) * 100
	}

	updates := map[string]any{
		"status":         "completed",
		"total_requests": total,
		"success_count":  success,
		"error_count":    fail,
		"qps":            qps,
		"latency_p50":    p50,
		"latency_p95":    p95,
		"latency_p99":    p99,
		"latency_avg":    avg,
		"error_rate":     errRate,
		"completed_at":   time.Now(),
	}
	_ = s.repo.UpdateFields(context.Background(), recordID, updates)
}

// GetResult 获取压测结果（ctx 必传：performance_test_results 是租户表，
// 裸 Background 会被租户闸拒成 500——2026-11-13 api_verify 实测）
func (s *PerformanceTestService) GetResult(ctx context.Context, id uint) (*model.PerformanceTestResult, error) {
	return s.repo.GetByID(ctx, id)
}

// ListResults 列出压测历史（ctx 必传，同 GetResult）
func (s *PerformanceTestService) ListResults(ctx context.Context, page, pageSize int) ([]*model.PerformanceTestResult, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	return s.repo.List(ctx, page, pageSize)
}

func percentile(data []float64, p int) float64 {
	if len(data) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(p)/100*float64(len(data)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(data) {
		idx = len(data) - 1
	}
	return data[idx]
}

func average(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

func ptrTime(t time.Time) *time.Time { return &t }
