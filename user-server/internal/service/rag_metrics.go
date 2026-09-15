package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	RagMetricsAggregationInterval = 5 * time.Minute
	RagMetricsBatchSize           = 100
	RagMetricsFlushInterval       = 2 * time.Second
	RagMetricsLowRecallDefault    = 0.3
)

// RagMetricsService RAG 召回率监控服务
type RagMetricsService struct {
	repo repository.RagMetricsRepository

	mu      sync.Mutex
	queue   []*model.RagQueryLog
	flushCh chan struct{}
	stopCh  chan struct{}
	wg      sync.WaitGroup
	started bool
}

// NewRagMetricsService 创建召回率监控服务
//
// db 为 nil 时（repo 为 nil）仅可调用计算类方法（buildQueryLog），
// 查询/写库类方法返回错误，RecordQuery 降级为 no-op
func NewRagMetricsService(db *gorm.DB) *RagMetricsService {
	s := &RagMetricsService{
		repo:    repository.NewRagMetricsRepository(db),
		queue:   make([]*model.RagQueryLog, 0, RagMetricsBatchSize),
		flushCh: make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
	}
	return s
}

// Start 启动后台异步写入循环（goroutine）
//
// 必须在主进程启动时调用一次；Stop 时优雅退出
func (s *RagMetricsService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.flushLoop(ctx)
}

// Stop 停止后台 goroutine，刷写剩余日志
func (s *RagMetricsService) Stop(ctx context.Context) {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	s.mu.Unlock()

	close(s.stopCh)
	select {
	case s.flushCh <- struct{}{}:
	default:
	}
	s.wg.Wait()
	_ = s.flush(ctx)
}

func (s *RagMetricsService) flushLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(RagMetricsFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			_ = s.flush(ctx)
			return
		case <-ticker.C:
			_ = s.flush(ctx)
		case <-s.flushCh:
			_ = s.flush(ctx)
		}
	}
}

// RecordQueryRequest 记录查询请求
type RecordQueryRequest struct {
	Query           string
	SessionID       string
	ProductID       string
	RetrievedDocIDs []string
	RelevantDocIDs  []string
	Latency         time.Duration
	TopK            int
	Source          string

	Top1DocID     string
	HitInTop1     bool
	TopSimilarity float64
}

// RecordQuery 异步记录一次检索（不阻塞调用方）
//
// 计算 precision/recall 并入队；实际写库由后台 goroutine 批量执行
func (s *RagMetricsService) RecordQuery(ctx context.Context, req *RecordQueryRequest) {
	if s == nil || req == nil {
		return
	}
	if s.repo == nil {
		return
	}
	log := s.buildQueryLog(ctx, req)
	s.mu.Lock()
	s.queue = append(s.queue, log)
	shouldFlush := len(s.queue) >= RagMetricsBatchSize
	s.mu.Unlock()
	if shouldFlush {
		select {
		case s.flushCh <- struct{}{}:
		default:
		}
	}
}

// RecordQuerySync 同步记录一次检索（测试用，保证写库完成）
func (s *RagMetricsService) RecordQuerySync(ctx context.Context, req *RecordQueryRequest) error {
	if s == nil || req == nil || s.repo == nil {
		return fmt.Errorf("service or repository is nil")
	}
	log := s.buildQueryLog(ctx, req)
	return s.repo.CreateQueryLog(ctx, log)
}

func (s *RagMetricsService) buildQueryLog(ctx context.Context, req *RecordQueryRequest) *model.RagQueryLog {
	retrievedSet := toStringSet(req.RetrievedDocIDs)
	relevantSet := toStringSet(req.RelevantDocIDs)
	hit := 0
	for id := range retrievedSet {
		if _, ok := relevantSet[id]; ok {
			hit++
		}
	}
	precision := 0.0
	if len(retrievedSet) > 0 {
		precision = float64(hit) / float64(len(retrievedSet))
	}
	recall := 0.0
	if len(relevantSet) > 0 {
		recall = float64(hit) / float64(len(relevantSet))
	}
	latencyMs := int64(req.Latency / time.Millisecond)
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	source := req.Source
	if source == "" {
		source = "hybrid"
	}
	return &model.RagQueryLog{
		Query:           req.Query,
		QueryHash:       hashQueryShort(req.Query),
		SessionID:       req.SessionID,
		ProductID:       req.ProductID,
		RetrievedDocIDs: toJSONString(req.RetrievedDocIDs),
		RelevantDocIDs:  toJSONString(req.RelevantDocIDs),
		RetrievedCount:  len(retrievedSet),
		RelevantCount:   len(relevantSet),
		HitCount:        hit,
		Top1DocID:       req.Top1DocID,
		HitInTop1:       req.HitInTop1,
		TopSimilarity:   req.TopSimilarity,
		Precision:       precision,
		Recall:          recall,
		LatencyMs:       latencyMs,
		TopK:            topK,
		Source:          source,
		CreatedAt:       time.Now(),
	}
}

func (s *RagMetricsService) flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.queue
	s.queue = make([]*model.RagQueryLog, 0, RagMetricsBatchSize)
	s.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.RagMetricsTimeout)
	defer cancel()
	if err := s.repo.CreateQueryLogsInBatches(ctx, batch, 50); err != nil {
		logger.Errorf("[RagMetrics] flush batch failed (%d logs): %v", len(batch), err)
		return err
	}
	return nil
}

// RecallMetrics 召回指标聚合结果
type RecallMetrics struct {
	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	TotalQueries   int64     `json:"total_queries"`
	AvgRecall      float64   `json:"avg_recall"`
	AvgPrecision   float64   `json:"avg_precision"`
	AvgLatencyMs   float64   `json:"avg_latency_ms"`
	P99LatencyMs   int64     `json:"p99_latency_ms"`
	ZeroHitCount   int64     `json:"zero_hit_count"`
	LowRecallCount int64     `json:"low_recall_count"`
}

// GetRecallMetrics 查询时间窗口内的召回指标
func (s *RagMetricsService) GetRecallMetrics(ctx context.Context, start, end time.Time) (*RecallMetrics, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end before start")
	}

	var metrics RecallMetrics
	metrics.WindowStart = start
	metrics.WindowEnd = end

	row, err := s.repo.AggregateQueryLogs(ctx, start, end, RagMetricsLowRecallDefault)
	if err != nil {
		return nil, fmt.Errorf("query recall metrics: %w", err)
	}
	metrics.TotalQueries = row.Total
	metrics.AvgRecall = row.AvgRecall
	metrics.AvgPrecision = row.AvgPrecision
	metrics.AvgLatencyMs = row.AvgLatency
	metrics.ZeroHitCount = row.ZeroHit
	metrics.LowRecallCount = row.LowRecall

	if row.Total > 0 {
		p99Offset := int(float64(row.Total) * 0.99)
		if p99Offset >= int(row.Total) {
			p99Offset = int(row.Total) - 1
		}
		if p99Offset < 0 {
			p99Offset = 0
		}
		p99Latency, err := s.repo.PluckP99Latency(ctx, start, end, p99Offset)
		if err != nil {
			logger.Errorf("[RagMetrics] query p99 latency failed: %v", err)
		} else {
			metrics.P99LatencyMs = p99Latency
		}
	}

	return &metrics, nil
}

// LowRecallQuery 低召回样本
type LowRecallQuery struct {
	ID             int64     `json:"id"`
	Query          string    `json:"query"`
	SessionID      string    `json:"session_id"`
	Recall         float64   `json:"recall"`
	Precision      float64   `json:"precision"`
	LatencyMs      int64     `json:"latency_ms"`
	RetrievedCount int       `json:"retrieved_count"`
	RelevantCount  int       `json:"relevant_count"`
	HitCount       int       `json:"hit_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// GetLowRecallQueries 查询召回率低于阈值的样本（用于调优）
//
// threshold ≤ 0 时用默认 0.3
// limit ≤ 0 或 > 1000 时用 100
// 按 created_at DESC 排序（最近的优先）
func (s *RagMetricsService) GetLowRecallQueries(ctx context.Context, threshold float64, limit int) ([]LowRecallQuery, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if threshold <= 0 {
		threshold = RagMetricsLowRecallDefault
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	logs, err := s.repo.FindLowRecallQueryLogs(ctx, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("query low recall: %w", err)
	}
	rows := make([]LowRecallQuery, 0, len(logs))
	for i := range logs {
		l := &logs[i]
		rows = append(rows, LowRecallQuery{
			ID:             l.ID,
			Query:          l.Query,
			SessionID:      l.SessionID,
			Recall:         l.Recall,
			Precision:      l.Precision,
			LatencyMs:      l.LatencyMs,
			RetrievedCount: l.RetrievedCount,
			RelevantCount:  l.RelevantCount,
			HitCount:       l.HitCount,
			CreatedAt:      l.CreatedAt,
		})
	}
	return rows, nil
}

// AggregateWindow 把 rag_query_logs 聚合到 rag_metrics_daily
//
// 由 cron 每 5 分钟调用；也可手动调用补跑
// 幂等：同一 window_start 已存在记录则更新
func (s *RagMetricsService) AggregateWindow(ctx context.Context, windowStart, windowEnd time.Time) (*model.RagMetricsDaily, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	metrics, err := s.GetRecallMetrics(ctx, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	daily := &model.RagMetricsDaily{
		WindowStart:    windowStart,
		WindowEnd:      windowEnd,
		TotalQueries:   metrics.TotalQueries,
		AvgRecall:      metrics.AvgRecall,
		AvgPrecision:   metrics.AvgPrecision,
		AvgLatencyMs:   metrics.AvgLatencyMs,
		P99LatencyMs:   metrics.P99LatencyMs,
		ZeroHitCount:   metrics.ZeroHitCount,
		LowRecallCount: metrics.LowRecallCount,
		CreatedAt:      time.Now(),
	}
	existing, err := s.repo.FindDailyByWindow(ctx, windowStart, windowEnd)
	if err == nil {
		daily.ID = existing.ID
		if err := s.repo.SaveDaily(ctx, daily); err != nil {
			return nil, fmt.Errorf("update rag_metrics_daily: %w", err)
		}
		return daily, nil
	}
	if !repository.IsRecordNotFound(err) {
		return nil, fmt.Errorf("query existing rag_metrics_daily: %w", err)
	}
	if err := s.repo.CreateDaily(ctx, daily); err != nil {
		return nil, fmt.Errorf("create rag_metrics_daily: %w", err)
	}
	return daily, nil
}

// AggregateLastWindow 聚合最近一个 5 分钟窗口
//
// 用于 cron 调用：windowEnd = now, windowStart = now - 5min
func (s *RagMetricsService) AggregateLastWindow(ctx context.Context) (*model.RagMetricsDaily, error) {
	end := time.Now().Truncate(RagMetricsAggregationInterval)
	start := end.Add(-RagMetricsAggregationInterval)
	return s.AggregateWindow(ctx, start, end)
}

// GetLatestMetrics 获取最近 N 个聚合窗口（用于趋势图）
func (s *RagMetricsService) GetLatestMetrics(ctx context.Context, limit int) ([]model.RagMetricsDaily, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if limit <= 0 || limit > 1000 {
		limit = 20
	}
	rows, err := s.repo.FindLatestDailies(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("query latest metrics: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].WindowStart.Before(rows[j].WindowStart)
	})
	return rows, nil
}

func toStringSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	return set
}

func toJSONString(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func hashQueryShort(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])[:16]
}

// RagMetricsCron 召回指标聚合定时任务
type RagMetricsCron struct {
	svc    *RagMetricsService
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewRagMetricsCron 创建 cron
func NewRagMetricsCron(svc *RagMetricsService) *RagMetricsCron {
	return &RagMetricsCron{svc: svc, stopCh: make(chan struct{})}
}

// Start 启动 cron
func (c *RagMetricsCron) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.run(ctx)
}

// Stop 停止 cron
func (c *RagMetricsCron) Stop(ctx context.Context) {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *RagMetricsCron) run(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(RagMetricsAggregationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
			_, err := c.svc.AggregateLastWindow(ctx)
			if err != nil {
				logger.Errorf("[RagMetricsCron] aggregate last window failed: %v", err)
			}
			cancel()
		}
	}
}
