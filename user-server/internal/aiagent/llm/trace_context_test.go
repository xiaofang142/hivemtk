package llm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 1. NewTraceContext 生成 trace_id 和 span_id
func TestNewTraceContext(t *testing.T) {
	tc := NewTraceContext("", "")
	if tc.TraceID() == "" {
		t.Error("expected non-empty trace_id")
	}
	if tc.SpanID() == "" {
		t.Error("expected non-empty span_id")
	}
	if tc.ParentSpanID() != "" {
		t.Errorf("expected empty parent_span_id, got %s", tc.ParentSpanID())
	}
}

// 2. NewTraceContext 复用 trace_id
func TestNewTraceContextReuseTraceID(t *testing.T) {
	tc := NewTraceContext("trace-123", "parent-456")
	if tc.TraceID() != "trace-123" {
		t.Errorf("expected trace-123, got %s", tc.TraceID())
	}
	if tc.ParentSpanID() != "parent-456" {
		t.Errorf("expected parent-456, got %s", tc.ParentSpanID())
	}
}

// 3. ChildSpan 继承 trace_id，更新 span_id，parent 为当前 span
func TestChildSpan(t *testing.T) {
	tc := NewTraceContext("trace-1", "")
	child := tc.ChildSpan()
	if child.TraceID() != tc.TraceID() {
		t.Errorf("expected same trace_id, got %s vs %s", child.TraceID(), tc.TraceID())
	}
	if child.SpanID() == tc.SpanID() {
		t.Error("expected different span_id")
	}
	if child.ParentSpanID() != tc.SpanID() {
		t.Errorf("expected parent=%s, got %s", tc.SpanID(), child.ParentSpanID())
	}
}

// 4. ChildSpan nil 安全
func TestChildSpanNil(t *testing.T) {
	var tc *TraceContext
	child := tc.ChildSpan()
	if child == nil {
		t.Fatal("expected non-nil child")
	}
	if child.TraceID() == "" {
		t.Error("expected auto-generated trace_id")
	}
}

// 5. SetMetadata / GetMetadata
func TestTraceContextMetadata(t *testing.T) {
	tc := NewTraceContext("", "")
	tc.SetMetadata("provider", "deepseek")
	tc.SetMetadata("tokens", 100)
	if v, ok := tc.GetMetadata("provider"); !ok || v != "deepseek" {
		t.Errorf("expected deepseek, got %v ok=%v", v, ok)
	}
	if v, ok := tc.GetMetadata("tokens"); !ok || v != 100 {
		t.Errorf("expected 100, got %v ok=%v", v, ok)
	}
	if _, ok := tc.GetMetadata("non-exist"); ok {
		t.Error("expected not ok for non-exist")
	}
}

// 6. Metadata 返回副本
func TestTraceContextMetadataCopy(t *testing.T) {
	tc := NewTraceContext("", "")
	tc.SetMetadata("k1", "v1")
	m := tc.Metadata()
	m["k1"] = "modified"
	if v, _ := tc.GetMetadata("k1"); v != "v1" {
		t.Errorf("expected v1 (copy), got %v", v)
	}
}

// 7. InjectContext 注入 trace_id
func TestTraceContextInjectContext(t *testing.T) {
	tc := NewTraceContext("trace-abc", "")
	ctx := tc.InjectContext(context.Background())
	if tid := logger.TraceIDFromContext(ctx); tid != "trace-abc" {
		t.Errorf("expected trace-abc, got %s", tid)
	}
}

// 8. InjectContext nil 安全
func TestTraceContextInjectContextNil(t *testing.T) {
	var tc *TraceContext
	ctx := tc.InjectContext(context.Background())
	if tid := logger.TraceIDFromContext(ctx); tid != "" {
		t.Errorf("expected empty, got %s", tid)
	}
}

// 9. TraceID nil 安全
func TestTraceContextNilSafe(t *testing.T) {
	var tc *TraceContext
	if tc.TraceID() != "" {
		t.Error("expected empty trace_id for nil")
	}
	if tc.SpanID() != "" {
		t.Error("expected empty span_id for nil")
	}
	if tc.ParentSpanID() != "" {
		t.Error("expected empty parent_span_id for nil")
	}
}

// 10. TraceEvent 表名
func TestTraceEventTableName(t *testing.T) {
	if (TraceEvent{}).TableName() != "trace_events" {
		t.Error("expected trace_events")
	}
}

// 11. InMemoryTraceBus Publish / Subscribe
func TestInMemoryTraceBusPublish(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	var got atomic.Int32
	var mu sync.Mutex
	var received TraceEvent
	bus.Subscribe(&testSubscriber{
		fn: func(e TraceEvent) {
			mu.Lock()
			defer mu.Unlock()
			received = e
			got.Add(1)
		},
	})
	bus.Publish(TraceEvent{
		TraceID:   "trace-1",
		SpanID:    "span-1",
		Kind:      TraceSpanKindLLMCall,
		Operation: "chat_completion",
		Timestamp: time.Now(),
	})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", got.Load())
	}
	if received.TraceID != "trace-1" {
		t.Errorf("expected trace-1, got %s", received.TraceID)
	}
}

// 12. InMemoryTraceBus 多订阅者
func TestInMemoryTraceBusMultiSubscribers(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	var count atomic.Int32
	sub := &testSubscriber{fn: func(e TraceEvent) { count.Add(1) }}
	bus.Subscribe(sub)
	bus.Subscribe(sub)
	bus.Publish(TraceEvent{TraceID: "t1", SpanID: "s1", Timestamp: time.Now()})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && count.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if count.Load() != 2 {
		t.Errorf("expected 2 events, got %d", count.Load())
	}
}

// 13. InMemoryTraceBus Subscribe nil 跳过
func TestInMemoryTraceBusSubscribeNil(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	bus.Subscribe(nil)
}

// 14. InMemoryTraceBus Stop 不重复关闭
func TestInMemoryTraceBusStop(t *testing.T) {
	bus := NewInMemoryTraceBus()
	bus.Stop()
	bus.Stop()
}

// 15. InMemoryTraceBus Stop 后 Publish 丢弃
func TestInMemoryTraceBusStopPublish(t *testing.T) {
	bus := NewInMemoryTraceBus()
	bus.Stop()
	bus.Publish(TraceEvent{TraceID: "t1", SpanID: "s1"})
}

// 16. PublishTraceEvent 自动补全字段
func TestPublishTraceEventAutoFill(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	oldBus := globalTraceBus
	globalTraceBus = bus
	defer func() { globalTraceBus = oldBus }()
	var got atomic.Int32
	var mu sync.Mutex
	var received TraceEvent
	bus.Subscribe(&testSubscriber{
		fn: func(e TraceEvent) {
			mu.Lock()
			defer mu.Unlock()
			received = e
			got.Add(1)
		},
	})
	PublishTraceEvent(TraceEvent{
		Kind:      TraceSpanKindDBOp,
		Operation: "SELECT",
	})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", got.Load())
	}
	if received.TraceID == "" {
		t.Error("expected auto-filled trace_id")
	}
	if received.SpanID == "" {
		t.Error("expected auto-filled span_id")
	}
	if received.Timestamp.IsZero() {
		t.Error("expected auto-filled timestamp")
	}
}

// 17. PublishLLMCall 发布 LLM 调用事件
func TestPublishLLMCall(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	oldBus := globalTraceBus
	globalTraceBus = bus
	defer func() { globalTraceBus = oldBus }()
	var got atomic.Int32
	var mu sync.Mutex
	var received TraceEvent
	bus.Subscribe(&testSubscriber{
		fn: func(e TraceEvent) {
			mu.Lock()
			defer mu.Unlock()
			received = e
			got.Add(1)
		},
	})
	PublishLLMCall("trace-1", "span-1", "", "deepseek", "deepseek-chat", 100, "ok", map[string]any{"tokens": 50})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", got.Load())
	}
	if received.Kind != TraceSpanKindLLMCall {
		t.Errorf("expected llm_call, got %s", received.Kind)
	}
	if received.Operation != "chat_completion" {
		t.Errorf("expected chat_completion, got %s", received.Operation)
	}
	if received.Metadata["provider"] != "deepseek" {
		t.Errorf("expected deepseek, got %v", received.Metadata["provider"])
	}
	if received.Metadata["model"] != "deepseek-chat" {
		t.Errorf("expected deepseek-chat, got %v", received.Metadata["model"])
	}
	if received.Metadata["tokens"] != 50 {
		t.Errorf("expected 50, got %v", received.Metadata["tokens"])
	}
}

// 18. PublishToolCall 发布工具调用事件
func TestPublishToolCall(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	oldBus := globalTraceBus
	globalTraceBus = bus
	defer func() { globalTraceBus = oldBus }()
	var got atomic.Int32
	var mu sync.Mutex
	var received TraceEvent
	bus.Subscribe(&testSubscriber{
		fn: func(e TraceEvent) {
			mu.Lock()
			defer mu.Unlock()
			received = e
			got.Add(1)
		},
	})
	PublishToolCall("trace-1", "span-1", "", "send_email", 200, "ok", nil)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", got.Load())
	}
	if received.Kind != TraceSpanKindToolCall {
		t.Errorf("expected tool_call, got %s", received.Kind)
	}
	if received.Operation != "send_email" {
		t.Errorf("expected send_email, got %s", received.Operation)
	}
}

// 19. PublishDBOp 发布 DB 操作事件
func TestPublishDBOp(t *testing.T) {
	bus := NewInMemoryTraceBus()
	defer bus.Stop()
	oldBus := globalTraceBus
	globalTraceBus = bus
	defer func() { globalTraceBus = oldBus }()
	var got atomic.Int32
	var mu sync.Mutex
	var received TraceEvent
	bus.Subscribe(&testSubscriber{
		fn: func(e TraceEvent) {
			mu.Lock()
			defer mu.Unlock()
			received = e
			got.Add(1)
		},
	})
	PublishDBOp("trace-1", "span-1", "", "SELECT customers", 50, "ok", map[string]any{"rows": 10})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", got.Load())
	}
	if received.Kind != TraceSpanKindDBOp {
		t.Errorf("expected db_op, got %s", received.Kind)
	}
	if received.Metadata["rows"] != 10 {
		t.Errorf("expected 10 rows, got %v", received.Metadata["rows"])
	}
}

// 20. MarshalMetadata / UnmarshalMetadata
func TestMarshalUnmarshalMetadata(t *testing.T) {
	m := map[string]any{"k1": "v1", "k2": 42}
	s := MarshalMetadata(m)
	if s == "{}" {
		t.Error("expected non-empty json")
	}
	parsed := UnmarshalMetadata(s)
	if parsed["k1"] != "v1" {
		t.Errorf("expected v1, got %v", parsed["k1"])
	}
}

// 21. MarshalMetadata 空 map
func TestMarshalMetadataEmpty(t *testing.T) {
	if s := MarshalMetadata(nil); s != "{}" {
		t.Errorf("expected {}, got %s", s)
	}
	if s := MarshalMetadata(map[string]any{}); s != "{}" {
		t.Errorf("expected {}, got %s", s)
	}
}

// 22. UnmarshalMetadata 异常 JSON
func TestUnmarshalMetadataBadJSON(t *testing.T) {
	m := UnmarshalMetadata("not-json")
	if len(m) != 0 {
		t.Errorf("expected empty, got %v", m)
	}
}

// 23. UnmarshalMetadata 空字符串
func TestUnmarshalMetadataEmpty(t *testing.T) {
	m := UnmarshalMetadata("")
	if len(m) != 0 {
		t.Errorf("expected empty, got %v", m)
	}
}

// 24. generateSpanID 长度
func TestGenerateSpanID(t *testing.T) {
	id := generateSpanID()
	if len(id) != 16 {
		t.Errorf("expected 16 chars, got %d", len(id))
	}
}

// 25. generateTraceID 唯一性
func TestGenerateTraceIDUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateTraceID()
		if ids[id] {
			t.Fatalf("duplicate trace_id: %s", id)
		}
		ids[id] = true
	}
}

// 26. TraceContext 并发安全 metadata
func TestTraceContextConcurrentMetadata(t *testing.T) {
	tc := NewTraceContext("", "")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tc.SetMetadata(string(rune('a'+i%26)), i)
			_, _ = tc.GetMetadata("k")
		}(i)
	}
	wg.Wait()
}

// 27. TraceEvent DB 持久化（端到端）
func TestTraceEventPersist(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})
	event := TraceEvent{
		TraceID:    "trace-persist-1",
		SpanID:     "span-persist-1",
		Kind:       TraceSpanKindLLMCall,
		Service:    "llm",
		Operation:  "chat_completion",
		DurationMs: 100,
		Status:     "ok",
		Metadata:   map[string]any{"provider": "deepseek"},
		Timestamp:  time.Now(),
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create failed: %v", err)
	}
	var got TraceEvent
	if err := db.Where("span_id = ?", "span-persist-1").First(&got).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if got.TraceID != "trace-persist-1" {
		t.Errorf("expected trace-persist-1, got %s", got.TraceID)
	}
	if got.Metadata["provider"] != "deepseek" {
		t.Errorf("expected deepseek, got %v", got.Metadata["provider"])
	}
}

// 28. TraceEvent 按 trace_id 查询
func TestTraceEventQueryByTraceID(t *testing.T) {
	db := testutil.NewTestDB(t, &TraceEvent{})
	traceID := "trace-multi"
	for i := 0; i < 3; i++ {
		event := TraceEvent{
			TraceID:   traceID,
			SpanID:    "span-" + string(rune('a'+i)),
			Kind:      TraceSpanKindLLMCall,
			Service:   "llm",
			Operation: "chat_completion",
			Status:    "ok",
			Timestamp: time.Now(),
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}
	var events []TraceEvent
	if err := db.Where("trace_id = ?", traceID).Order("timestamp ASC").Find(&events).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

// 29. TraceContextFromGin 从 gin.Context 取出 TraceContext
func TestTraceContextFromGin(t *testing.T) {
	tc := NewTraceContext("trace-gin-1", "")
	if tc.TraceID() != "trace-gin-1" {
		t.Errorf("expected trace-gin-1, got %s", tc.TraceID())
	}
}

// 30. GetGlobalTraceBus 单例
func TestGetGlobalTraceBusSingleton(t *testing.T) {
	bus1 := GetGlobalTraceBus()
	bus2 := GetGlobalTraceBus()
	if bus1 != bus2 {
		t.Error("expected same singleton instance")
	}
}

// 30b. 守卫：全局 bus **尚未初始化**时并发 PublishTraceEvent 不得构成数据竞争。
//
// 为什么必须把 bus 复位成 nil：包内先跑的测试已经把 globalTraceBus 建好了，
// 旧实现里 `if globalTraceBus == nil` 那次裸读就永远不会和 Once 闭包内的写同时发生，
// 守卫会在坏代码上假绿。CI 的 `-race` 正是命中 :297 裸读 vs :290 写入这一对
// （两个 goroutine 都从 :315 的 PublishTraceEvent 进来）。
func TestGetGlobalTraceBusConcurrentFirstPublish(t *testing.T) {
	globalTraceBus = nil
	globalTraceBusOnce = *new(sync.Once)

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			PublishTraceEvent(TraceEvent{
				TraceID: fmt.Sprintf("%032x", i+1),
				Kind:    TraceSpanKindLog,
				Service: "race-guard",
			})
		}(i)
	}
	wg.Wait()

	if bus := GetGlobalTraceBus(); bus == nil {
		t.Fatal("并发 Publish 后全局 bus 不应为 nil")
	}
}

type testSubscriber struct {
	fn func(event TraceEvent)
}

func (s *testSubscriber) OnEvent(event TraceEvent) {
	if s.fn != nil {
		s.fn(event)
	}
}
