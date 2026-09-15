package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// RagRecallMonitorConstants
const (
	RagRecallMonitorDefaultInterval = 5 * time.Minute
	RagRecallMonitorDefaultWindow   = 1 * time.Hour
	RagRecallMonitorTableName       = "rag_recall_monitor_snapshots"
)

// RagRecallMetricsSummary 召回率指标汇总
type RagRecallMetricsSummary struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`

	TotalQueries int64 `json:"total_queries"`

	TopKHitRate   float64 `json:"top_k_hit_rate"`
	TopOneHitRate float64 `json:"top_1_hit_rate"`
	AvgRecall     float64 `json:"avg_recall"`
	AvgPrecision  float64 `json:"avg_precision"`
	AvgSimilarity float64 `json:"avg_similarity"`

	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P95LatencyMs int64   `json:"p95_latency_ms"`

	ZeroHitCount   int64 `json:"zero_hit_count"`
	LowRecallCount int64 `json:"low_recall_count"`
}

// RagRecallMonitorService RAG 召回率监控服务
type RagRecallMonitorService struct {
	repo repository.RagRecallMonitorRepository

	mu       sync.Mutex
	started  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	interval time.Duration
	window   time.Duration

	lastSnapshot *RagRecallMetricsSummary
	lastAt       time.Time
}

// NewRagRecallMonitorService 创建 RAG 召回率监控服务
//
// interval / window <= 0 时使用默认值；db 为 nil 时 repo 为 nil，
// 查询/写库类方法返回错误
func NewRagRecallMonitorService(db *gorm.DB, interval, window time.Duration) *RagRecallMonitorService {
	if interval <= 0 {
		interval = RagRecallMonitorDefaultInterval
	}
	if window <= 0 {
		window = RagRecallMonitorDefaultWindow
	}
	return &RagRecallMonitorService{
		repo:     repository.NewRagRecallMonitorRepository(db),
		stopCh:   make(chan struct{}),
		interval: interval,
		window:   window,
	}
}

// Start 启动后台定时采集
func (s *RagRecallMonitorService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run(ctx)
}

// Stop 停止后台采集
func (s *RagRecallMonitorService) Stop(ctx context.Context) {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()
}

func (s *RagRecallMonitorService) run(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
			if _, err := s.CollectAndStore(ctx, time.Now().Add(-s.window), time.Now()); err != nil {
				cancel()
				continue
			}
			cancel()
		}
	}
}

// Collect 采集指定窗口的指标（不写库）
func (s *RagRecallMonitorService) Collect(ctx context.Context, start, end time.Time) (*RagRecallMetricsSummary, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("service or repository is nil")
	}
	if end.Before(start) {
		return nil, errors.New("end before start")
	}

	summary := &RagRecallMetricsSummary{
		WindowStart: start,
		WindowEnd:   end,
	}

	row, err := s.repo.AggregateRecallLogs(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("query recall aggregate: %w", err)
	}

	summary.TotalQueries = row.Total
	summary.AvgRecall = row.AvgRecall
	summary.AvgPrecision = row.AvgPrecision
	summary.AvgLatencyMs = row.AvgLatency
	summary.AvgSimilarity = row.AvgSimilarity
	summary.ZeroHitCount = row.ZeroHit
	summary.LowRecallCount = row.LowRecall

	if row.Total > 0 {
		summary.TopKHitRate = float64(row.Total-row.ZeroHit) / float64(row.Total)
		summary.TopOneHitRate = float64(row.Top1Hit) / float64(row.Total)
	}

	if row.Total > 0 {
		p95Offset := int(float64(row.Total) * 0.95)
		if p95Offset >= int(row.Total) {
			p95Offset = int(row.Total) - 1
		}
		if p95Offset < 0 {
			p95Offset = 0
		}
		if p95, err := s.repo.PluckP95Latency(ctx, start, end, p95Offset); err == nil {
			summary.P95LatencyMs = p95
		}
	}

	return summary, nil
}

// CollectAndStore 采集指标并写入监控表
func (s *RagRecallMonitorService) CollectAndStore(ctx context.Context, start, end time.Time) (*RagRecallMetricsSummary, error) {
	summary, err := s.Collect(ctx, start, end)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.lastSnapshot = summary
	s.lastAt = time.Now()
	s.mu.Unlock()

	if s.repo != nil {
		payload, _ := json.Marshal(summary)
		row := map[string]any{
			"window_start":     summary.WindowStart,
			"window_end":       summary.WindowEnd,
			"total_queries":    summary.TotalQueries,
			"top_k_hit_rate":   summary.TopKHitRate,
			"top_1_hit_rate":   summary.TopOneHitRate,
			"avg_recall":       summary.AvgRecall,
			"avg_precision":    summary.AvgPrecision,
			"avg_similarity":   summary.AvgSimilarity,
			"avg_latency_ms":   summary.AvgLatencyMs,
			"p95_latency_ms":   summary.P95LatencyMs,
			"zero_hit_count":   summary.ZeroHitCount,
			"low_recall_count": summary.LowRecallCount,
			"payload":          string(payload),
			"created_at":       time.Now(),
		}
		if err := s.repo.CreateSnapshot(ctx, row); err != nil {
			return summary, fmt.Errorf("store snapshot: %w", err)
		}
	}

	return summary, nil
}

// GetLatestSnapshot 获取最近一次采集的快照（内存中）
func (s *RagRecallMonitorService) GetLatestSnapshot(ctx context.Context) (*RagRecallMetricsSummary, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSnapshot, s.lastAt
}

// ListSnapshots 列出最近 N 条监控快照
func (s *RagRecallMonitorService) ListSnapshots(ctx context.Context, limit int) ([]map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("service or repository is nil")
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := s.repo.ListSnapshots(ctx, limit)
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, _ := rows[i]["window_start"].(time.Time)
		aj, _ := rows[j]["window_start"].(time.Time)
		return ai.Before(aj)
	})
	return rows, nil
}

// EnsureSchema 确保监控表存在（AutoMigrate 风格的轻量 DDL）
//
// 设计：监控表不是核心业务表，使用 CreateTableIfNotExists 模式，
// 部署初始化时调用一次即可。
func (s *RagRecallMonitorService) EnsureSchema(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return errors.New("service or repository is nil")
	}
	return s.repo.EnsureSchema(ctx)
}
