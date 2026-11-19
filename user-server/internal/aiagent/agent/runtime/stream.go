package agent_runtime

import (
	"context"
	"sync"
	"time"
)

// StreamEventType 流式事件类型
type StreamEventType string

const (
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventError      StreamEventType = "error"
	StreamEventComplete   StreamEventType = "complete"
	StreamEventStageStart StreamEventType = "stage_start"
	StreamEventStageEnd   StreamEventType = "stage_end"
	StreamEventOutcome    StreamEventType = "outcome"
)

// StreamEvent 流式事件
type StreamEvent struct {
	Type        StreamEventType  `json:"type"`
	ToolName    string           `json:"tool_name,omitempty"`
	CallID      string           `json:"call_id,omitempty"`
	Content     string           `json:"content,omitempty"`
	StageName   string           `json:"stage_name,omitempty"`
	Error       string           `json:"error,omitempty"`
	Timestamp   time.Time        `json:"timestamp"`
	Duration    time.Duration    `json:"duration,omitempty"`
	Outcome     InferenceOutcome `json:"outcome,omitempty"`
	Publishable bool             `json:"publishable"`
	FailedStage string           `json:"failed_stage,omitempty"`
}

// StreamHandler 流式处理器接口
type StreamHandler interface {
	OnEvent(event StreamEvent)
	OnError(err error)
	OnComplete()
}

// BufferedStreamHandler 缓冲流式处理器
type BufferedStreamHandler struct {
	events   []StreamEvent
	mu       sync.Mutex
	callback func(event StreamEvent)
}

// NewBufferedStreamHandler 创建缓冲流式处理器
func NewBufferedStreamHandler(callback func(event StreamEvent)) *BufferedStreamHandler {
	return &BufferedStreamHandler{
		events:   make([]StreamEvent, 0),
		callback: callback,
	}
}

// OnEvent 处理流式事件
func (h *BufferedStreamHandler) OnEvent(event StreamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	event.Timestamp = time.Now()
	h.events = append(h.events, event)

	if h.callback != nil {
		h.callback(event)
	}
}

// OnError 处理错误
func (h *BufferedStreamHandler) OnError(err error) {
	h.OnEvent(StreamEvent{
		Type:  StreamEventError,
		Error: err.Error(),
	})
}

// OnComplete 处理完成
func (h *BufferedStreamHandler) OnComplete() {
	h.OnEvent(StreamEvent{
		Type: StreamEventComplete,
	})
}

// Events 获取所有事件
func (h *BufferedStreamHandler) Events() []StreamEvent {
	h.mu.Lock()
	defer h.mu.Unlock()

	events := make([]StreamEvent, len(h.events))
	copy(events, h.events)
	return events
}

// Clear 清空事件
func (h *BufferedStreamHandler) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = make([]StreamEvent, 0)
}

// ChannelStreamHandler 通道流式处理器
type ChannelStreamHandler struct {
	channel chan StreamEvent
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
}

// NewChannelStreamHandler 创建通道流式处理器
func NewChannelStreamHandler(bufferSize int) *ChannelStreamHandler {
	return &ChannelStreamHandler{
		channel: make(chan StreamEvent, bufferSize),
		done:    make(chan struct{}),
	}
}

// OnEvent 处理流式事件
func (h *ChannelStreamHandler) OnEvent(event StreamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	event.Timestamp = time.Now()
	select {
	case h.channel <- event:
	default:
	}
}

// OnError 处理错误
func (h *ChannelStreamHandler) OnError(err error) {
	h.OnEvent(StreamEvent{
		Type:  StreamEventError,
		Error: err.Error(),
	})
}

// OnComplete 处理完成
func (h *ChannelStreamHandler) OnComplete() {
	h.OnEvent(StreamEvent{
		Type: StreamEventComplete,
	})
	h.closeStream()
}

func (h *ChannelStreamHandler) closeStream() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.channel)
	close(h.done)
}

// Events 获取事件通道
func (h *ChannelStreamHandler) Events() <-chan StreamEvent {
	return h.channel
}

// Done 获取完成信号
func (h *ChannelStreamHandler) Done() <-chan struct{} {
	return h.done
}

// CompositeStreamHandler 组合流式处理器
type CompositeStreamHandler struct {
	handlers []StreamHandler
	mu       sync.RWMutex
}

// NewCompositeStreamHandler 创建组合流式处理器
func NewCompositeStreamHandler(handlers ...StreamHandler) *CompositeStreamHandler {
	return &CompositeStreamHandler{
		handlers: handlers,
	}
}

// AddHandler 添加处理器
func (h *CompositeStreamHandler) AddHandler(handler StreamHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = append(h.handlers, handler)
}

// OnEvent 处理流式事件
func (h *CompositeStreamHandler) OnEvent(event StreamEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		handler.OnEvent(event)
	}
}

// OnError 处理错误
func (h *CompositeStreamHandler) OnError(err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		handler.OnError(err)
	}
}

// OnComplete 处理完成
func (h *CompositeStreamHandler) OnComplete() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		handler.OnComplete()
	}
}

func (h *CompositeStreamHandler) closeStream() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, handler := range h.handlers {
		closeInferenceStream(handler)
	}
}

func closeInferenceStream(handler StreamHandler) {
	if closer, ok := handler.(interface{ closeStream() }); ok {
		closer.closeStream()
	}
}

// RunOnceStream 执行一次完整推理闭环（支持流式输出）
//
// 参数：
//   - ctx: 上下文
//   - payload: 客户消息载荷
//   - agentCtx: 智能体上下文
//   - handler: 流式处理器
//
// 返回：
//   - InferenceDecision: 最终决策
//   - error: 阶段技术故障、超时或无效结果；失败不发成功完成事件。
func (c *InferenceCycle) RunOnceStream(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext, handler StreamHandler) (*InferenceDecision, error) {
	decision, err := c.runOnce(ctx, payload, agentCtx, handler)
	if handler == nil {
		return decision, err
	}
	if decision != nil {
		handler.OnEvent(StreamEvent{Type: StreamEventOutcome, Outcome: decision.Outcome, Publishable: decision.Publishable, FailedStage: decision.FailedStage, Error: decision.ErrorCode})
	}
	if err != nil {
		handler.OnError(err)
		closeInferenceStream(handler)
	} else if decision != nil && decision.Outcome == InferenceRejected {
		closeInferenceStream(handler)
	} else {
		handler.OnComplete()
	}
	return decision, err
}
