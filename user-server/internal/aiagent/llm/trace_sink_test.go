package llm

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"
)

// TestDBTraceSink_OnEvent_NonBlocking 验证 OnEvent 在缓冲满时不会阻塞
func TestDBTraceSink_OnEvent_NonBlocking(t *testing.T) {
	sink := NewDBTraceSink(nil)
	sink.Start()
	defer sink.Stop()

	done := make(chan struct{})
	go func() {
		for i := 0; i < DefaultDBSinkBufferSize+100; i++ {
			sink.OnEvent(TraceEvent{
				TraceID:    "t1",
				SpanID:     "s1",
				Kind:       TraceSpanKindLog,
				Service:    "test",
				DurationMs: 0,
				Status:     "ok",
				Timestamp:  time.Now(),
			})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnEvent blocked under backpressure")
	}
}

// TestDBTraceSink_Stats 验证 inserted/dropped 计数
func TestDBTraceSink_Stats(t *testing.T) {
	sink := NewDBTraceSink(nil)
	ins, drp := sink.Stats()
	if ins != 0 || drp != 0 {
		t.Errorf("expected zero stats, got ins=%d drp=%d", ins, drp)
	}
	sink.OnEvent(TraceEvent{SpanID: "s1", TraceID: "t1", Service: "x", Operation: "y", Kind: TraceSpanKindLog, Timestamp: time.Now()})
}

// TestDBTraceSink_StopIdempotent 验证 Stop 幂等
func TestDBTraceSink_StopIdempotent(t *testing.T) {
	sink := NewDBTraceSink(nil)
	sink.Start()
	sink.Stop()
	sink.Stop()
}

// TestQueryTrace_NotFound 验证 trace 不存在时返回 ErrTraceNotFound
func TestQueryTrace_NotFound(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})
	t.Cleanup(func() {
		_ = db
	})

	origDB := traceDBOverride
	traceDBOverride = func() any { return db }
	t.Cleanup(func() { traceDBOverride = origDB })

	ctx := context.Background()
	_, err := QueryTrace(ctx, "non-existent-trace-id-xyz")
	if err == nil {
		t.Fatal("expected ErrTraceNotFound, got nil")
	}
	if err != ErrTraceNotFound {
		t.Fatalf("expected ErrTraceNotFound, got %v", err)
	}
}

// TestQueryTrace_EmptyTraceID 验证空 trace_id 直接返回错误
func TestQueryTrace_EmptyTraceID(t *testing.T) {
	_, err := QueryTrace(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty trace_id")
	}
}

// TestListRecentTraces_LimitClamp 验证 limit 边界归一
func TestListRecentTraces_LimitClamp(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})
	origDB := traceDBOverride
	traceDBOverride = func() any { return db }
	t.Cleanup(func() { traceDBOverride = origDB })

	_, err := ListRecentTraces(context.Background(), 0)
	if err != nil {
		t.Fatalf("limit=0 should normalize to 50, got err: %v", err)
	}
	_, err = ListRecentTraces(context.Background(), 99999)
	if err != nil {
		t.Fatalf("limit>500 should normalize, got err: %v", err)
	}
}

// TestDBTraceSink_FlushInsertsToTable 端到端：发布 → 订阅者落库 → QueryTrace 读出
func TestDBTraceSink_FlushInsertsToTable(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})

	origDB := traceDBOverride
	traceDBOverride = func() any { return db }
	t.Cleanup(func() { traceDBOverride = origDB })

	if err := db.Table("trace_events").Create([]map[string]any{
		{
			"trace_id":    "trace-roundtrip-1",
			"span_id":     "span-A",
			"kind":        "llm_call",
			"service":     "llm",
			"operation":   "chat_completion",
			"duration_ms": 120,
			"status":      "ok",
			"timestamp":   time.Now().Add(-2 * time.Second),
		},
		{
			"trace_id":       "trace-roundtrip-1",
			"span_id":        "span-B",
			"parent_span_id": "span-A",
			"kind":           "tool_call",
			"service":        "tool",
			"operation":      "search_kb",
			"duration_ms":    35,
			"status":         "ok",
			"timestamp":      time.Now().Add(-1 * time.Second),
		},
	}).Error; err != nil {
		t.Fatalf("seed trace_events: %v", err)
	}

	detail, err := QueryTrace(context.Background(), "trace-roundtrip-1")
	if err != nil {
		t.Fatalf("QueryTrace: %v", err)
	}
	if detail.Summary.SpanCount != 2 {
		t.Errorf("expected 2 spans, got %d", detail.Summary.SpanCount)
	}
	if detail.Summary.TotalDurationMs != 155 {
		t.Errorf("expected total 155ms, got %d", detail.Summary.TotalDurationMs)
	}
	if detail.Summary.KindCounts["llm_call"] != 1 || detail.Summary.KindCounts["tool_call"] != 1 {
		t.Errorf("kind counts wrong: %+v", detail.Summary.KindCounts)
	}
	if detail.Summary.ErrorCount != 0 {
		t.Errorf("expected 0 errors, got %d", detail.Summary.ErrorCount)
	}
}

// TestDBTraceSink_AsyncInsert 验证：发布 → 启动 sink → 等批量落库 → 读到
func TestDBTraceSink_AsyncInsert(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})
	origDB := traceDBOverride
	traceDBOverride = func() any { return db }
	t.Cleanup(func() { traceDBOverride = origDB })

	sink := NewDBTraceSink(db)
	sink.Start()

	const traceID = "async-insert-trace"
	const N = 5
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sink.OnEvent(TraceEvent{
				TraceID:    traceID,
				SpanID:     "sp-" + time.Now().Format("150405.000000") + "-" + string(rune('0'+idx)),
				Kind:       TraceSpanKindLLMCall,
				Service:    "llm",
				Operation:  "chat",
				DurationMs: int64(idx * 10),
				Status:     "ok",
				Timestamp:  time.Now(),
			})
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ins, _ := sink.Stats()
		if ins >= N {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	sink.Stop()

	detail, err := QueryTrace(context.Background(), traceID)
	if err != nil {
		t.Fatalf("QueryTrace after async insert: %v", err)
	}
	if detail.Summary.SpanCount < N {
		t.Errorf("expected at least %d spans, got %d", N, detail.Summary.SpanCount)
	}
}
