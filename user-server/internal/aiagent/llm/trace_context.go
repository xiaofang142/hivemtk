package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 2026-09-16 审计 DB-07：TraceEvent 此前从未登记建表，
// 而 trace_sink.go 有 `s.db.Table("trace_events").Create(rows)` 写入。
//
// 为什么写在这里而不是 internal/pkg/db/migrate.go：本包 import 了 internal/pkg/db，
// 反向 import 会成环，故走 RegisterExtraModels。
func init() { _db.RegisterExtraModels(&TraceEvent{}) }

// TraceSpanKind Span 类型
type TraceSpanKind string

const (
	TraceSpanKindLLMCall   TraceSpanKind = "llm_call"
	TraceSpanKindToolCall  TraceSpanKind = "tool_call"
	TraceSpanKindDBOp      TraceSpanKind = "db_op"
	TraceSpanKindLog       TraceSpanKind = "log"
	TraceSpanKindRAGQuery  TraceSpanKind = "rag_query"
	TraceSpanKindAgent     TraceSpanKind = "agent"
	TraceSpanKindWebSocket TraceSpanKind = "websocket"
)

// TraceEvent 全链路追踪事件（对应 trace_events 表）
type TraceEvent struct {
	ID           uint64         `json:"id" gorm:"primaryKey;autoIncrement"`
	TraceID      string         `json:"trace_id" gorm:"type:varchar(64);not null;index"`
	SpanID       string         `json:"span_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	ParentSpanID string         `json:"parent_span_id,omitempty" gorm:"type:varchar(64);index"`
	Kind         TraceSpanKind  `json:"kind" gorm:"type:varchar(32);not null;index"`
	Service      string         `json:"service" gorm:"type:varchar(64);not null"`
	Operation    string         `json:"operation" gorm:"type:varchar(128);not null"`
	DurationMs   int64          `json:"duration_ms" gorm:"default:0"`
	Status       string         `json:"status" gorm:"type:varchar(16);default:'ok'"`
	Metadata     map[string]any `json:"metadata,omitempty" gorm:"type:text;serializer:json"`
	Timestamp    time.Time      `json:"timestamp" gorm:"index"`
}

// TableName GORM 表名
func (TraceEvent) TableName() string { return "trace_events" }

// TraceContext 全链路追踪上下文
type TraceContext struct {
	traceID      string
	spanID       string
	parentSpanID string
	mu           sync.RWMutex
	metadata     map[string]any
}

// NewTraceContext 创建新的追踪上下文
// traceID 为空时生成 UUIDv7（按时间排序，便于时序查询）
func NewTraceContext(traceID, parentSpanID string) *TraceContext {
	if traceID == "" {
		traceID = generateTraceID()
	}
	return &TraceContext{
		traceID:      traceID,
		spanID:       generateSpanID(),
		parentSpanID: parentSpanID,
		metadata:     make(map[string]any),
	}
}

// TraceID 返回 trace_id
func (tc *TraceContext) TraceID() string {
	if tc == nil {
		return ""
	}
	return tc.traceID
}

// SpanID 返回 span_id
func (tc *TraceContext) SpanID() string {
	if tc == nil {
		return ""
	}
	return tc.spanID
}

// ParentSpanID 返回 parent_span_id
func (tc *TraceContext) ParentSpanID() string {
	if tc == nil {
		return ""
	}
	return tc.parentSpanID
}

// SetMetadata 设置元数据
func (tc *TraceContext) SetMetadata(key string, value any) {
	if tc == nil {
		return
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.metadata[key] = value
}

// GetMetadata 获取元数据
func (tc *TraceContext) GetMetadata(key string) (any, bool) {
	if tc == nil {
		return nil, false
	}
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	v, ok := tc.metadata[key]
	return v, ok
}

// Metadata 返回元数据副本
func (tc *TraceContext) Metadata() map[string]any {
	if tc == nil {
		return nil
	}
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	out := make(map[string]any, len(tc.metadata))
	for k, v := range tc.metadata {
		out[k] = v
	}
	return out
}

// SetSpanIDFor 显式设置本 span_id（用于 trace middleware 注入 W3C 子 span）。
// 防御：tc 为 nil 时不 panic。
func (tc *TraceContext) SetSpanIDFor(spanID string) {
	if tc == nil {
		return
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.spanID = spanID
}

// GenerateSpanIDStatic 包级便捷函数：生成新 span_id（16 hex）。
// 用于 trace middleware 等需要同步获取 span_id 而不构造完整 TraceContext 的场景。
func GenerateSpanIDStatic() string {
	return generateSpanID()
}

// GenerateTraceIDStatic 包级便捷函数：生成新 trace_id（32 hex，W3C 规范）。
// 用于 trace middleware 等需要同步获取 trace_id 而不构造完整 TraceContext 的场景。
func GenerateTraceIDStatic() string {
	return generateTraceID()
}

// ChildSpan 创建子 span（trace_id 不变，parent_span_id = 当前 span_id）
func (tc *TraceContext) ChildSpan() *TraceContext {
	if tc == nil {
		return NewTraceContext("", "")
	}
	return &TraceContext{
		traceID:      tc.traceID,
		spanID:       generateSpanID(),
		parentSpanID: tc.spanID,
		metadata:     make(map[string]any),
	}
}

// InjectContext 将 trace_id 注入 context（与 logger.WithTraceID 兼容）
func (tc *TraceContext) InjectContext(ctx context.Context) context.Context {
	if tc == nil || ctx == nil {
		return ctx
	}
	return logger.WithTraceID(ctx, tc.traceID)
}

func generateTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {

		return uuid.New().String()
	}
	return hex.EncodeToString(b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {

		id := uuid.New().String()
		return id[:8] + id[9:13]
	}
	return hex.EncodeToString(b)
}

// TraceEventPublisher 追踪事件发布器接口
type TraceEventPublisher interface {
	Publish(event TraceEvent)
}

// TraceEventSubscriber 追踪事件订阅器接口
type TraceEventSubscriber interface {
	OnEvent(event TraceEvent)
}

// InMemoryTraceBus 进程内追踪事件总线（默认实现）
// 支持多订阅者，事件缓冲区 1024，背压时丢弃事件并记日志
type InMemoryTraceBus struct {
	mu          sync.RWMutex
	subscribers []TraceEventSubscriber
	eventQueue  chan TraceEvent
	stopped     atomic.Bool
}

// NewInMemoryTraceBus 创建进程内追踪事件总线
func NewInMemoryTraceBus() *InMemoryTraceBus {
	bus := &InMemoryTraceBus{
		eventQueue: make(chan TraceEvent, 1024),
	}
	go bus.dispatch()
	return bus
}

// Subscribe 订阅追踪事件
func (b *InMemoryTraceBus) Subscribe(sub TraceEventSubscriber) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, sub)
}

// Publish 发布事件（非阻塞，缓冲区满时丢弃）
func (b *InMemoryTraceBus) Publish(event TraceEvent) {
	if b.stopped.Load() {
		return
	}
	select {
	case b.eventQueue <- event:
	default:
		logger.Warnf("[TraceBus] event queue full, dropping event trace_id=%s span_id=%s", event.TraceID, event.SpanID)
	}
}

// Stop 停止事件总线
func (b *InMemoryTraceBus) Stop() {
	if b.stopped.CompareAndSwap(false, true) {
		close(b.eventQueue)
	}
}

func (b *InMemoryTraceBus) dispatch() {
	for event := range b.eventQueue {
		b.mu.RLock()
		subs := make([]TraceEventSubscriber, len(b.subscribers))
		copy(subs, b.subscribers)
		b.mu.RUnlock()
		for _, sub := range subs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[TraceBus] subscriber panic: %v", r)
					}
				}()
				sub.OnEvent(event)
			}()
		}
	}
}

// TraceRecorder 追踪事件记录器接口
type TraceRecorder interface {
	Record(event TraceEvent) error
}

var (
	globalTraceBus     *InMemoryTraceBus
	globalTraceBusOnce sync.Once
)

// InitGlobalTraceBus 初始化全局追踪事件总线
func InitGlobalTraceBus() *InMemoryTraceBus {
	globalTraceBusOnce.Do(func() {
		globalTraceBus = NewInMemoryTraceBus()
	})
	return globalTraceBus
}

// GetGlobalTraceBus 获取全局追踪事件总线
func GetGlobalTraceBus() *InMemoryTraceBus {
	if globalTraceBus == nil {
		return InitGlobalTraceBus()
	}
	return globalTraceBus
}

// PublishTraceEvent 发布追踪事件（便捷方法）
// 自动设置 timestamp；trace_id/span_id 缺失时自动生成
func PublishTraceEvent(event TraceEvent) {
	if event.TraceID == "" {
		event.TraceID = generateTraceID()
	}
	if event.SpanID == "" {
		event.SpanID = generateSpanID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	bus := GetGlobalTraceBus()
	if bus != nil {
		bus.Publish(event)
	}
}

// PublishLLMCall 发布 LLM 调用事件
func PublishLLMCall(traceID, spanID, parentSpanID, provider, model string, durationMs int64, status string, metadata map[string]any) {
	PublishTraceEvent(TraceEvent{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Kind:         TraceSpanKindLLMCall,
		Service:      "llm",
		Operation:    "chat_completion",
		DurationMs:   durationMs,
		Status:       status,
		Metadata: mergeMetadata(metadata, map[string]any{
			"provider": provider,
			"model":    model,
		}),
	})
}

// PublishToolCall 发布工具调用事件
func PublishToolCall(traceID, spanID, parentSpanID, toolName string, durationMs int64, status string, metadata map[string]any) {
	PublishTraceEvent(TraceEvent{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Kind:         TraceSpanKindToolCall,
		Service:      "tool",
		Operation:    toolName,
		DurationMs:   durationMs,
		Status:       status,
		Metadata:     metadata,
	})
}

// PublishDBOp 发布 DB 操作事件
func PublishDBOp(traceID, spanID, parentSpanID, operation string, durationMs int64, status string, metadata map[string]any) {
	PublishTraceEvent(TraceEvent{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Kind:         TraceSpanKindDBOp,
		Service:      "db",
		Operation:    operation,
		DurationMs:   durationMs,
		Status:       status,
		Metadata:     metadata,
	})
}

func mergeMetadata(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// MarshalMetadata 序列化 metadata 为 JSON 字符串（便于持久化）
func MarshalMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return "{}"
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// UnmarshalMetadata 反序列化 metadata
func UnmarshalMetadata(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}
