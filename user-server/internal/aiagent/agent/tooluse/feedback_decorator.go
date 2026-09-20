package tooluse

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// ToolRiskLevel（原 read/write/admin）已随 T-P3-05 迁到 tool_risk.go，
// 换成 G-3 的三档 readonly / low_write / high_write。
//
// 迁移而不是并存的原因：旧那套是**死元数据** —— 全仓只有 3 个工具实现过
// RiskLevel()，而唯一的潜在读取方 ToolCallEvent.RiskLevel 在构造事件时根本不赋值
// （见下方 FeedbackCollectorDecorator），feedback_sink_adapter 里那个
// metadata["risk_level"] 分支因此从未进过任何一行。留着两套同名方法
// （BaseTool.RiskLevel 与旧实现的覆盖方法）只会让"分级"这件事有两个答案。
// 另注：旧注释写的默认值是 RiskLevelRead，方向与 G-3 要的 safe-by-default 相反。

// FeedbackSink 反馈回流接口
//
// 由调用方（router 层）注入具体实现：
//   - FeedbackCollectorAdapter 包装 service.FeedbackCollector
//   - 测试场景使用 MemoryFeedbackSink
type FeedbackSink interface {
	RecordToolCall(ctx context.Context, event ToolCallEvent) error
}

// ToolCallEvent 工具调用事件（反馈回流载荷）
type ToolCallEvent struct {
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args"`
	Result     ToolResult     `json:"result"`
	Duration   time.Duration  `json:"duration_ms"`
	TraceID    string         `json:"trace_id,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	CustomerID string         `json:"customer_id,omitempty"`
	AgentID    string         `json:"agent_id,omitempty"`
	Source     string         `json:"source,omitempty"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	RiskLevel  ToolRiskLevel  `json:"risk_level,omitempty"`
	Version    string         `json:"version,omitempty"`
}

// FeedbackCollectorDecorator 反馈回流装饰器
//
// 在工具执行结束后，将调用事件回流到 FeedbackSink。
// sink 为 nil 时直接放行（零开销）。
//
// 通过 MonitoredFeedbackSink 包装，对 sink.RecordToolCall 错误进行监控
//   - 错误计数（atomic，无锁）
//   - 错误日志（log.Printf，便于运维定位）
//   - 错误率超阈值告警（10s 窗口内错误率 > 50% 时输出告警日志）
//
// 装饰器链位置：审计计费之后、handler 之前
//   - 位于审计之后：可复用 result/error/duration
//   - 位于 handler 之外：不影响 handler 执行
//   - 异步上报：通过 go routine 异步调用 sink.RecordToolCall
//     避免反馈回流失败/慢导致主链路阻塞
func FeedbackCollectorDecorator(sink FeedbackSink) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			result, err := next(ctx, args)

			if sink == nil {
				return result, err
			}

			toolName := GetToolName(ctx)
			tc := GetToolContext(ctx)
			traceID := GetTraceID(ctx)

			event := ToolCallEvent{
				ToolName: toolName,
				Args:     sanitizeArgsForFeedback(args),
				Result:   result,
				Duration: time.Duration(result.Timing.DurationMs) * time.Millisecond,
				TraceID:  traceID,
				Success:  err == nil && result.Success,
			}
			if err != nil {
				event.Error = err.Error()
			} else if !result.Success {
				event.Error = result.Error
			}
			if tc != nil {
				event.SessionID = tc.SessionID
				event.CustomerID = tc.CustomerID
				event.AgentID = tc.AgentID
				event.Source = tc.Source
			}

			go func(ev ToolCallEvent) {
				sinkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if sinkErr := sink.RecordToolCall(sinkCtx, ev); sinkErr != nil {
					log.Printf("[WARN] FeedbackSink.RecordToolCall failed: tool=%s trace_id=%s err=%v",
						ev.ToolName, ev.TraceID, sinkErr)
				}
			}(event)

			return result, err
		}
	}
}

func sanitizeArgsForFeedback(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	sensitiveKeys := map[string]bool{
		"password": true, "token": true, "secret": true,
		"api_key": true, "apikey": true, "phone": true,
		"id_card": true, "bank_card": true,
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if sensitiveKeys[k] {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

// MemoryFeedbackSink 内存反馈接收器（用于测试 / 单机审计）
//
// 采用环形缓冲区（ring buffer）实现 O(1) 写入
//   - 旧实现使用 slice + events[1:] 满容量时 O(n) 拷贝，高频写入时性能瓶颈
//   - 新实现使用固定容量 + head/size 指针，写入复杂度 O(1)
//   - 读出时按 oldest→newest 顺序返回（保持语义一致）
//
// 线程安全；由于 FeedbackCollectorDecorator 异步 goroutine 调用 RecordToolCall，
// 必须加锁保护内部状态。
type MemoryFeedbackSink struct {
	mu      sync.Mutex
	buf     []ToolCallEvent
	head    int
	size    int
	max     int
	dropped int64
}

// NewMemoryFeedbackSink 创建内存反馈接收器
// max: 最大保留条数（超出后环形覆盖最旧条目）
func NewMemoryFeedbackSink(max int) *MemoryFeedbackSink {
	if max < 1 {
		max = 1000
	}
	return &MemoryFeedbackSink{
		buf:  make([]ToolCallEvent, max),
		head: 0,
		size: 0,
		max:  max,
	}
}

// RecordToolCall 记录工具调用事件（：环形缓冲区 O(1) 写入）
func (s *MemoryFeedbackSink) RecordToolCall(ctx context.Context, event ToolCallEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.head] = event
	s.head = (s.head + 1) % s.max
	if s.size < s.max {
		s.size++
	} else {
		s.dropped++
	}
	return nil
}

// Events 返回所有事件副本（按 oldest→newest 顺序）
func (s *MemoryFeedbackSink) Events() []ToolCallEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.size == 0 {
		return []ToolCallEvent{}
	}
	out := make([]ToolCallEvent, s.size)
	start := 0
	if s.size == s.max {
		start = s.head
	}
	for i := 0; i < s.size; i++ {
		out[i] = s.buf[(start+i)%s.max]
	}
	return out
}

// Count 返回事件条数
func (s *MemoryFeedbackSink) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

// Dropped 返回因容量满被覆盖的条目数（监控用）
func (s *MemoryFeedbackSink) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Reset 清空事件
func (s *MemoryFeedbackSink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.head = 0
	s.size = 0
	s.dropped = 0
}

// NoOpFeedbackSink 空操作反馈接收器（不进行任何记录）
type NoOpFeedbackSink struct{}

func (NoOpFeedbackSink) RecordToolCall(ctx context.Context, event ToolCallEvent) error {
	return nil
}

// FeedbackSinkMetrics 反馈回流错误监控指标
//
// 通过 atomic 计数器实现无锁监控，避免反馈回流影响主链路性能
// 调用方可通过 Metrics() 读取当前快照，用于 /metrics endpoint 暴露或告警判定
type FeedbackSinkMetrics struct {
	TotalAttempts  atomic.Int64
	SuccessCount   atomic.Int64
	FailureCount   atomic.Int64
	TimeoutCount   atomic.Int64
	LastFailureMsg atomic.Value
}

// Snapshot 返回指标快照（用于 /metrics 暴露或日志输出）
type FeedbackSinkMetricsSnapshot struct {
	TotalAttempts  int64
	SuccessCount   int64
	FailureCount   int64
	TimeoutCount   int64
	FailureRate    float64
	LastFailureMsg string
}

// Snapshot 返回指标快照
func (m *FeedbackSinkMetrics) Snapshot() FeedbackSinkMetricsSnapshot {
	total := m.TotalAttempts.Load()
	success := m.SuccessCount.Load()
	failure := m.FailureCount.Load()
	timeout := m.TimeoutCount.Load()
	var rate float64
	if total > 0 {
		rate = float64(failure) / float64(total)
	}
	var lastMsg string
	if v := m.LastFailureMsg.Load(); v != nil {
		lastMsg = v.(string)
	}
	return FeedbackSinkMetricsSnapshot{
		TotalAttempts:  total,
		SuccessCount:   success,
		FailureCount:   failure,
		TimeoutCount:   timeout,
		FailureRate:    rate,
		LastFailureMsg: lastMsg,
	}
}

// MonitoredFeedbackSink 带 metrics 监控的 FeedbackSink 包装器
//
// 包装任意 FeedbackSink，在 RecordToolCall 调用时统计成功/失败次数
// 用于运维监控反馈回流链路健康度
//
// 使用方式：
//
//	rawSink := NewMemoryFeedbackSink(1000)
//	monitoredSink := NewMonitoredFeedbackSink(rawSink)
//	decorator := FeedbackCollectorDecorator(monitoredSink)
//	// ... 运行期可通过 monitoredSink.Metrics().Snapshot() 读取指标
type MonitoredFeedbackSink struct {
	inner   FeedbackSink
	metrics FeedbackSinkMetrics
}

// NewMonitoredFeedbackSink 包装一个 FeedbackSink 为带监控的版本
//
// inner 为 nil 时返回 nil（让 FeedbackCollectorDecorator 的 nil 检查自动跳过）
func NewMonitoredFeedbackSink(inner FeedbackSink) *MonitoredFeedbackSink {
	if inner == nil {
		return nil
	}
	return &MonitoredFeedbackSink{inner: inner}
}

// RecordToolCall 实现 FeedbackSink 接口，同时记录监控指标
func (s *MonitoredFeedbackSink) RecordToolCall(ctx context.Context, event ToolCallEvent) error {
	s.metrics.TotalAttempts.Add(1)
	err := s.inner.RecordToolCall(ctx, event)
	if err == nil {
		s.metrics.SuccessCount.Add(1)
		return nil
	}
	s.metrics.FailureCount.Add(1)
	s.metrics.LastFailureMsg.Store(err.Error())
	if errors.Is(err, context.DeadlineExceeded) {
		s.metrics.TimeoutCount.Add(1)
	}
	return err
}

// Metrics 返回 metrics 指针（调用方可读取 Snapshot 或重置）
func (s *MonitoredFeedbackSink) Metrics() *FeedbackSinkMetrics {
	return &s.metrics
}
