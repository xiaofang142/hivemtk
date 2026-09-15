package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupRagMetricsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.RagQueryLog{},
		&model.RagMetricsDaily{},
	)
}

// TestRagMetrics_NewService 测试服务构造
func TestRagMetrics_NewService(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	if svc == nil {
		t.Fatal("Expected non-nil service")
	}
	if svc.repo == nil {
		t.Error("Expected non-nil repository")
	}
	if svc.queue == nil {
		t.Error("Expected non-nil queue")
	}
	if cap(svc.queue) != RagMetricsBatchSize {
		t.Errorf("Expected queue cap=%d, got %d", RagMetricsBatchSize, cap(svc.queue))
	}
}

// TestRagMetrics_NewService_NilDB 测试 nil db 构造（用于纯计算场景）
func TestRagMetrics_NewService_NilDB(t *testing.T) {
	svc := NewRagMetricsService(nil)
	if svc == nil {
		t.Fatal("Expected non-nil service even with nil db")
	}
	svc.RecordQuery(context.Background(), &RecordQueryRequest{Query: "test"})
	_, err := svc.GetRecallMetrics(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Error("Expected error for nil db")
	}
}

// TestRagMetrics_NewService_NilReceiver 测试 nil receiver 防御
func TestRagMetrics_NewService_NilReceiver(t *testing.T) {
	var svc *RagMetricsService
	svc.RecordQuery(context.Background(), &RecordQueryRequest{Query: "x"})
	svc.RecordQuery(context.Background(), nil)
	_, err := svc.GetRecallMetrics(context.Background(), time.Now(), time.Now())
	if err == nil {
		t.Error("Expected error for nil receiver")
	}
	_, err = svc.GetLowRecallQueries(context.Background(), 0, 0)
	if err == nil {
		t.Error("Expected error for nil receiver")
	}
}

// TestRagMetrics_RecordQuerySync_Basic 测试同步写入基础
func TestRagMetrics_RecordQuerySync_Basic(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	req := &RecordQueryRequest{
		Query:           "如何申请退款",
		SessionID:       "sess-001",
		ProductID:       "100",
		RetrievedDocIDs: []string{"d1", "d2", "d3"},
		RelevantDocIDs:  []string{"d1", "d2"},
		Latency:         150 * time.Millisecond,
		TopK:            5,
		Source:          "hybrid",
	}
	if err := svc.RecordQuerySync(context.Background(), req); err != nil {
		t.Fatalf("RecordQuerySync failed: %v", err)
	}

	var log model.RagQueryLog
	if err := db.First(&log).Error; err != nil {
		t.Fatalf("query log not found: %v", err)
	}
	if log.Query != "如何申请退款" {
		t.Errorf("Expected query='如何申请退款', got %q", log.Query)
	}
	if log.RetrievedCount != 3 {
		t.Errorf("Expected retrieved_count=3, got %d", log.RetrievedCount)
	}
	if log.RelevantCount != 2 {
		t.Errorf("Expected relevant_count=2, got %d", log.RelevantCount)
	}
	if log.HitCount != 2 {
		t.Errorf("Expected hit_count=2, got %d", log.HitCount)
	}
	if absFloat(log.Precision-2.0/3.0) > 1e-4 {
		t.Errorf("Expected precision=%.4f, got %.4f", 2.0/3.0, log.Precision)
	}
	if absFloat(log.Recall-1.0) > 1e-4 {
		t.Errorf("Expected recall=1.0, got %.4f", log.Recall)
	}
	if log.LatencyMs != 150 {
		t.Errorf("Expected latency_ms=150, got %d", log.LatencyMs)
	}
	if log.Source != "hybrid" {
		t.Errorf("Expected source=hybrid, got %s", log.Source)
	}
}

// TestRagMetrics_RecordQuerySync_TopKDefault 测试 TopK 默认值
func TestRagMetrics_RecordQuerySync_TopKDefault(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	req := &RecordQueryRequest{
		Query:           "test",
		RetrievedDocIDs: []string{"a"},
		RelevantDocIDs:  []string{"a"},
		Latency:         10 * time.Millisecond,
	}
	if err := svc.RecordQuerySync(context.Background(), req); err != nil {
		t.Fatalf("RecordQuerySync failed: %v", err)
	}

	var log model.RagQueryLog
	if err := db.First(&log).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if log.TopK != 5 {
		t.Errorf("Expected default TopK=5, got %d", log.TopK)
	}
	if log.Source != "hybrid" {
		t.Errorf("Expected default source=hybrid, got %s", log.Source)
	}
}

// TestRagMetrics_RecordQuerySync_NilReq 测试 nil request 防御
func TestRagMetrics_RecordQuerySync_NilReq(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	err := svc.RecordQuerySync(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil req")
	}
}

// TestRagMetrics_GetRecallMetrics_Empty 测试空数据集
func TestRagMetrics_GetRecallMetrics_Empty(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	now := time.Now()
	m, err := svc.GetRecallMetrics(context.Background(), now.Add(-time.Hour), now)
	if err != nil {
		t.Fatalf("GetRecallMetrics failed: %v", err)
	}
	if m.TotalQueries != 0 {
		t.Errorf("Expected total=0, got %d", m.TotalQueries)
	}
	if m.AvgRecall != 0 {
		t.Errorf("Expected avg_recall=0, got %f", m.AvgRecall)
	}
	if m.P99LatencyMs != 0 {
		t.Errorf("Expected p99=0 for empty, got %d", m.P99LatencyMs)
	}
}

// TestRagMetrics_GetRecallMetrics_Aggregation 测试聚合计算（含）
func TestRagMetrics_GetRecallMetrics_Aggregation(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	now := time.Now()

	for i := 0; i < 10; i++ {
		req := &RecordQueryRequest{
			Query:           fmt.Sprintf("q%d", i),
			RetrievedDocIDs: []string{fmt.Sprintf("d%d", i)},
			RelevantDocIDs:  []string{},
			Latency:         time.Duration(10*(i+1)) * time.Millisecond,
			TopK:            5,
		}
		if err := svc.RecordQuerySync(context.Background(), req); err != nil {
			t.Fatalf("RecordQuerySync %d failed: %v", i, err)
		}
	}

	m, err := svc.GetRecallMetrics(context.Background(), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecallMetrics failed: %v", err)
	}
	if m.TotalQueries != 10 {
		t.Errorf("Expected total=10, got %d", m.TotalQueries)
	}
	if m.AvgRecall != 0 {
		t.Errorf("Expected avg_recall=0, got %f", m.AvgRecall)
	}
	if m.P99LatencyMs != 100 {
		t.Errorf("Expected p99=100, got %d", m.P99LatencyMs)
	}
	if m.ZeroHitCount != 0 {
		t.Errorf("Expected zero_hit=0, got %d", m.ZeroHitCount)
	}
	if m.LowRecallCount != 10 {
		t.Errorf("Expected low_recall=10, got %d", m.LowRecallCount)
	}
}

// TestRagMetrics_GetRecallMetrics_EndBeforeStart 测试 end < start 错误
func TestRagMetrics_GetRecallMetrics_EndBeforeStart(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	now := time.Now()
	_, err := svc.GetRecallMetrics(context.Background(), now, now.Add(-time.Hour))
	if err == nil {
		t.Error("Expected error for end before start")
	}
}

// TestRagMetrics_GetRecallMetrics_ZeroHit 测试 zero_hit 统计
func TestRagMetrics_GetRecallMetrics_ZeroHit(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	now := time.Now()

	for i := 0; i < 5; i++ {
		req := &RecordQueryRequest{
			Query:   fmt.Sprintf("q%d", i),
			Latency: 10 * time.Millisecond,
			TopK:    5,
		}
		if i < 3 {
			req.RetrievedDocIDs = []string{}
			req.RelevantDocIDs = []string{}
		} else {
			req.RetrievedDocIDs = []string{"x"}
			req.RelevantDocIDs = []string{"x"}
		}
		if err := svc.RecordQuerySync(context.Background(), req); err != nil {
			t.Fatalf("RecordQuerySync %d failed: %v", i, err)
		}
	}

	m, err := svc.GetRecallMetrics(context.Background(), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecallMetrics failed: %v", err)
	}
	if m.TotalQueries != 5 {
		t.Errorf("Expected total=5, got %d", m.TotalQueries)
	}
	if m.ZeroHitCount != 3 {
		t.Errorf("Expected zero_hit=3, got %d", m.ZeroHitCount)
	}
}

// TestRagMetrics_GetLowRecallQueries_Basic 测试低召回样本查询
func TestRagMetrics_GetLowRecallQueries_Basic(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	queries := []struct {
		retrieved []string
		relevant  []string
	}{
		{[]string{"a", "b"}, []string{"a", "b"}},
		{[]string{"a"}, []string{"a", "b", "c", "d"}},
		{[]string{"a"}, []string{}},
	}
	for i, q := range queries {
		req := &RecordQueryRequest{
			Query:           fmt.Sprintf("q%d", i),
			RetrievedDocIDs: q.retrieved,
			RelevantDocIDs:  q.relevant,
			Latency:         10 * time.Millisecond,
		}
		if err := svc.RecordQuerySync(context.Background(), req); err != nil {
			t.Fatalf("RecordQuerySync %d failed: %v", i, err)
		}
	}

	rows, err := svc.GetLowRecallQueries(context.Background(), 0.3, 100)
	if err != nil {
		t.Fatalf("GetLowRecallQueries failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Expected 1 low recall row, got %d", len(rows))
	}
	if rows[0].Query != "q1" {
		t.Errorf("Expected query=q1, got %q", rows[0].Query)
	}
	if absFloat(rows[0].Recall-0.25) > 1e-4 {
		t.Errorf("Expected recall=0.25, got %f", rows[0].Recall)
	}
}

// TestRagMetrics_GetLowRecallQueries_DefaultThreshold 测试默认阈值
func TestRagMetrics_GetLowRecallQueries_DefaultThreshold(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	req := &RecordQueryRequest{
		Query:           "test",
		RetrievedDocIDs: []string{"a"},
		RelevantDocIDs:  []string{"a", "b", "c", "d", "e"},
		Latency:         5 * time.Millisecond,
	}
	if err := svc.RecordQuerySync(context.Background(), req); err != nil {
		t.Fatalf("RecordQuerySync failed: %v", err)
	}

	rows, err := svc.GetLowRecallQueries(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetLowRecallQueries failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("Expected 1 row with default threshold, got %d", len(rows))
	}
}

// TestRagMetrics_GetLowRecallQueries_LimitClamp 测试 limit 边界
func TestRagMetrics_GetLowRecallQueries_LimitClamp(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	for i := 0; i < 5; i++ {
		req := &RecordQueryRequest{
			Query:           fmt.Sprintf("q%d", i),
			RetrievedDocIDs: []string{"a", "b"},
			RelevantDocIDs:  []string{},
			Latency:         5 * time.Millisecond,
		}
		_ = svc.RecordQuerySync(context.Background(), req)
	}
	rows, err := svc.GetLowRecallQueries(context.Background(), 0.5, 0)
	if err != nil {
		t.Fatalf("GetLowRecallQueries failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("Expected 0 rows (all filtered by relevant=0), got %d", len(rows))
	}
}

// TestRagMetrics_AggregateWindow_Idempotent 测试幂等：重复调用应更新而非新建
func TestRagMetrics_AggregateWindow_Idempotent(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	end := start.Add(5 * time.Minute)

	d1, err := svc.AggregateWindow(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateWindow 1 failed: %v", err)
	}
	if d1.TotalQueries != 0 {
		t.Errorf("Expected total=0, got %d", d1.TotalQueries)
	}

	req := &RecordQueryRequest{
		Query:           "test",
		RetrievedDocIDs: []string{"a"},
		RelevantDocIDs:  []string{"a"},
		Latency:         10 * time.Millisecond,
	}
	log := svc.buildQueryLog(context.Background(), req)
	log.CreatedAt = start.Add(time.Minute)
	if err := db.Create(log).Error; err != nil {
		t.Fatalf("create log failed: %v", err)
	}

	d2, err := svc.AggregateWindow(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateWindow 2 failed: %v", err)
	}
	if d2.TotalQueries != 1 {
		t.Errorf("Expected total=1, got %d", d2.TotalQueries)
	}
	if d2.ID != d1.ID {
		t.Errorf("Expected same ID (idempotent update), got d1=%d d2=%d", d1.ID, d2.ID)
	}

	var count int64
	db.Model(&model.RagMetricsDaily{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 daily row, got %d", count)
	}
}

// TestRagMetrics_AggregateWindow_NilDB 测试 nil db 防御
func TestRagMetrics_AggregateWindow_NilDB(t *testing.T) {
	svc := NewRagMetricsService(nil)
	_, err := svc.AggregateWindow(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Error("Expected error for nil db")
	}
}

// TestRagMetrics_AggregateLastWindow 测试最近窗口聚合
func TestRagMetrics_AggregateLastWindow(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	now := time.Now()
	req := &RecordQueryRequest{
		Query:           "x",
		RetrievedDocIDs: []string{"a"},
		RelevantDocIDs:  []string{"a"},
		Latency:         5 * time.Millisecond,
	}
	log := svc.buildQueryLog(context.Background(), req)
	log.CreatedAt = now.Add(-time.Minute)
	if err := db.Create(log).Error; err != nil {
		t.Fatalf("create log failed: %v", err)
	}

	d, err := svc.AggregateLastWindow(context.Background())
	if err != nil {
		t.Fatalf("AggregateLastWindow failed: %v", err)
	}
	if d.WindowEnd.After(d.WindowStart) == false {
		t.Error("Expected WindowEnd > WindowStart")
	}
	if d.WindowEnd.Sub(d.WindowStart) != RagMetricsAggregationInterval {
		t.Errorf("Expected window duration=%v, got %v", RagMetricsAggregationInterval, d.WindowEnd.Sub(d.WindowStart))
	}
}

// TestRagMetrics_GetLatestMetrics 测试最新指标查询（升序返回）
func TestRagMetrics_GetLatestMetrics(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	base := time.Now().Truncate(time.Minute)
	for i := 0; i < 3; i++ {
		d := &model.RagMetricsDaily{
			WindowStart:  base.Add(time.Duration(i) * 5 * time.Minute),
			WindowEnd:    base.Add(time.Duration(i+1) * 5 * time.Minute),
			TotalQueries: int64(i),
			AvgRecall:    float64(i) * 0.1,
			CreatedAt:    time.Now(),
		}
		if err := db.Create(d).Error; err != nil {
			t.Fatalf("create daily %d failed: %v", i, err)
		}
	}

	rows, err := svc.GetLatestMetrics(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetLatestMetrics failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("Expected 3 rows, got %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].WindowStart.Before(rows[i-1].WindowStart) {
			t.Errorf("Expected ascending order, row %d before row %d", i, i-1)
		}
	}
}

// TestRagMetrics_GetLatestMetrics_LimitClamp 测试 limit 边界
func TestRagMetrics_GetLatestMetrics_LimitClamp(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	rows, err := svc.GetLatestMetrics(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetLatestMetrics failed: %v", err)
	}
	if rows == nil {
		t.Error("Expected non-nil slice")
	}
	if len(rows) != 0 {
		t.Errorf("Expected 0 rows, got %d", len(rows))
	}
}

// TestRagMetrics_StartStop 测试 Start/Stop 后台 goroutine
func TestRagMetrics_StartStop(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)

	svc.Start(context.Background())
	svc.Start(context.Background())

	for i := 0; i < 3; i++ {
		svc.RecordQuery(context.Background(), &RecordQueryRequest{
			Query:           fmt.Sprintf("async-%d", i),
			RetrievedDocIDs: []string{"a"},
			RelevantDocIDs:  []string{"a"},
			Latency:         5 * time.Millisecond,
		})
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		db.Model(&model.RagQueryLog{}).Count(&count)
		if count >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	var count int64
	db.Model(&model.RagQueryLog{}).Count(&count)
	if count < 3 {
		t.Errorf("Expected ≥3 logs flushed, got %d", count)
	}

	svc.Stop(context.Background())
	svc.Stop(context.Background())
}

// TestRagMetrics_FlushBatchTrigger 测试批量阈值触发 flush
func TestRagMetrics_FlushBatchTrigger(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	svc.Start(context.Background())
	defer svc.Stop(context.Background())

	for i := 0; i < RagMetricsBatchSize+5; i++ {
		svc.RecordQuery(context.Background(), &RecordQueryRequest{
			Query:           fmt.Sprintf("batch-%d", i),
			RetrievedDocIDs: []string{"a"},
			RelevantDocIDs:  []string{"a"},
			Latency:         1 * time.Millisecond,
		})
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		db.Model(&model.RagQueryLog{}).Count(&count)
		if count >= RagMetricsBatchSize {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var count int64
	db.Model(&model.RagQueryLog{}).Count(&count)
	if count < RagMetricsBatchSize {
		t.Errorf("Expected ≥%d logs flushed, got %d", RagMetricsBatchSize, count)
	}
}

// TestRagMetrics_Cron_StartStop 测试 cron 启停
func TestRagMetrics_Cron_StartStop(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	cron := NewRagMetricsCron(svc)
	if cron == nil {
		t.Fatal("Expected non-nil cron")
	}

	cron.Start(context.Background())
	cron.Stop(context.Background())
}

// TestRagMetrics_BuildQueryLog_Calc 测试 buildQueryLog 计算
func TestRagMetrics_BuildQueryLog_Calc(t *testing.T) {
	svc := NewRagMetricsService(nil)
	cases := []struct {
		name      string
		retrieved []string
		relevant  []string
		wantHit   int
		wantPrec  float64
		wantRec   float64
	}{
		{"全部命中", []string{"a", "b"}, []string{"a", "b"}, 2, 1.0, 1.0},
		{"部分命中", []string{"a", "b", "c"}, []string{"b", "d"}, 1, 1.0 / 3.0, 1.0 / 2.0},
		{"零命中", []string{"a"}, []string{"b"}, 0, 0.0, 0.0},
		{"空检索", []string{}, []string{"a"}, 0, 0.0, 0.0},
		{"空相关", []string{"a"}, []string{}, 0, 0.0, 0.0},
		{"双重空", []string{}, []string{}, 0, 0.0, 0.0},
		{"重复ID去重", []string{"a", "a", "b"}, []string{"a"}, 1, 1.0 / 2.0, 1.0},
		{"含空字符串", []string{"a", "", "b"}, []string{"a", ""}, 1, 1.0 / 2.0, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &RecordQueryRequest{
				Query:           c.name,
				RetrievedDocIDs: c.retrieved,
				RelevantDocIDs:  c.relevant,
				Latency:         10 * time.Millisecond,
			}
			log := svc.buildQueryLog(context.Background(), req)
			if log.HitCount != c.wantHit {
				t.Errorf("Hit: want %d, got %d", c.wantHit, log.HitCount)
			}
			if absFloat(log.Precision-c.wantPrec) > 1e-4 {
				t.Errorf("Precision: want %.4f, got %.4f", c.wantPrec, log.Precision)
			}
			if absFloat(log.Recall-c.wantRec) > 1e-4 {
				t.Errorf("Recall: want %.4f, got %.4f", c.wantRec, log.Recall)
			}
		})
	}
}

// TestRagMetrics_ToStringSet 测试 toStringSet 去重
func TestRagMetrics_ToStringSet(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want int
	}{
		{"空切片", []string{}, 0},
		{"无重复", []string{"a", "b", "c"}, 3},
		{"有重复", []string{"a", "a", "b", "b"}, 2},
		{"含空字符串", []string{"a", "", "b", ""}, 2},
		{"全空字符串", []string{"", "", ""}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := toStringSet(c.in)
			if len(set) != c.want {
				t.Errorf("want size=%d, got %d", c.want, len(set))
			}
		})
	}
}

// TestRagMetrics_ToJSONString 测试 toJSONString 序列化
func TestRagMetrics_ToJSONString(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"空切片", []string{}, "[]"},
		{"单个", []string{"a"}, `["a"]`},
		{"多个", []string{"a", "b"}, `["a","b"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toJSONString(c.in)
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}

// TestRagMetrics_HashQueryShort 测试 hashQueryShort 一致性
func TestRagMetrics_HashQueryShort(t *testing.T) {
	q1 := "如何申请退款"
	q2 := "如何申请退款"
	q3 := "如何申请退款 "

	h1 := hashQueryShort(q1)
	h2 := hashQueryShort(q2)
	h3 := hashQueryShort(q3)

	if h1 != h2 {
		t.Errorf("Expected same hash for same query: %s vs %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("Expected different hash for different query (trailing space)")
	}
	if len(h1) != 16 {
		t.Errorf("Expected 16-char hash, got %d", len(h1))
	}
}

// TestRagMetrics_ConcurrentRecordQuery 测试并发 RecordQuery 不应 panic 或丢数据
func TestRagMetrics_ConcurrentRecordQuery(t *testing.T) {
	db := setupRagMetricsTestDB(t)
	svc := NewRagMetricsService(db)
	svc.Start(context.Background())
	defer svc.Stop(context.Background())

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			svc.RecordQuery(context.Background(), &RecordQueryRequest{
				Query:           fmt.Sprintf("concurrent-%d", i),
				RetrievedDocIDs: []string{"a"},
				RelevantDocIDs:  []string{"a"},
				Latency:         1 * time.Millisecond,
			})
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		db.Model(&model.RagQueryLog{}).Count(&count)
		if count >= N {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var count int64
	db.Model(&model.RagQueryLog{}).Count(&count)
	if count < N {
		t.Errorf("Expected ≥%d logs flushed, got %d", N, count)
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
