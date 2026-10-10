package agent_runtime

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
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
)

// StreamEvent 流式事件
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	ToolName  string          `json:"tool_name,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	StageName string          `json:"stage_name,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	Duration  time.Duration   `json:"duration,omitempty"`
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
//   - error: 仅在编排器本身失败时返回错误
func (c *InferenceCycle) RunOnceStream(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext, handler StreamHandler) (*InferenceDecision, error) {
	if c.stopped {
		return nil, ErrRuntimeStopped
	}

	start := time.Now()

	if agentCtx == nil {
		agentCtx = &AgentContext{
			AgentID:   0,
			AgentCode: "default",
			Name:      "默认智能体",
			AgentType: "customer_service",
			LoadedAt:  time.Now(),
		}
	}

	tctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()

	ic := &InferenceContext{
		Payload:   payload,
		AgentCtx:  agentCtx,
		StartTime: start,
		Stages:    []StageDecision{},
		Decision: InferenceDecision{
			ReplyType:  "text",
			Confidence: 0,
		},
	}

	c.mu.RLock()
	provider := c.memoryProvider
	c.mu.RUnlock()
	if provider != nil {
		mem, err := provider.LoadEpisodicMemory(tctx, payload.SessionID, payload.CustomerID)
		if err != nil {
			logger.Warnf("[inference_cycle] load episodic memory failed session=%s customer=%s err=%v",
				payload.SessionID, payload.CustomerID, err)
		} else if mem != "" {
			ic.EpisodicMemory = mem
		}
	}

	stages := []InferenceStage{
		c.PerceptionStage,
		c.AlignmentStage,
		c.GatekeeperStage,
		c.PlannerStage,
	}

	for _, stage := range stages {
		if stage == nil {
			continue
		}

		if handler != nil {
			handler.OnEvent(StreamEvent{
				Type:      StreamEventStageStart,
				StageName: stage.Name(),
			})
		}

		sctx, scancel := context.WithTimeout(tctx, c.StageTimeout)
		stageStart := time.Now()
		result := stage.Execute(sctx, ic)
		stageDuration := time.Since(stageStart)
		scancel()

		if handler != nil {
			handler.OnEvent(StreamEvent{
				Type:      StreamEventStageEnd,
				StageName: stage.Name(),
				Duration:  stageDuration,
			})
		}

		if result.Error != nil {
			logger.Warnf("[inference_cycle] stage=%s error=%v", stage.Name(), result.Error)
			if handler != nil {
				handler.OnError(result.Error)
			}
		}

		if result.EarlyReturn {
			if result.Decision != nil {
				ic.Decision = mergeDecision(ic.Decision, *result.Decision)
			}
			ic.Decision.TotalDuration = time.Since(start)
			ic.Decision.Crisis = ic.Crisis
			ic.Decision.Sentiment = ic.Sentiment
			ic.Decision.Intent = ic.Intent
			ic.Decision.Alignment = ic.Alignment
			c.recordStats(ic.Decision)

			if handler != nil {
				handler.OnComplete()
			}

			logger.Infof("[inference_cycle] early return at stage=%s handoff=%v reason=%s duration=%s",
				stage.Name(), ic.Decision.HandoffToHuman, ic.Decision.StopReason, ic.Decision.TotalDuration)
			return &ic.Decision, nil
		}
	}

	ic.Decision.TotalDuration = time.Since(start)
	if ic.Plan != nil {
		ic.Decision.ReplyType = "text"
		ic.Decision.Confidence = ic.Plan.Confidence
		if ic.Plan.SkipLLM {
			ic.Decision.Reply = ic.Plan.ReplyHint
			ic.Decision.ReplyType = "text"
			ic.Decision.StopReason = "faq_skip_llm"
		} else {
			ic.Decision.StopReason = "plan_ready"
		}
	} else {
		ic.Decision.StopReason = "no_plan"
	}
	ic.Decision.Crisis = ic.Crisis
	ic.Decision.Sentiment = ic.Sentiment
	ic.Decision.Intent = ic.Intent
	ic.Decision.Alignment = ic.Alignment

	c.recordStats(ic.Decision)

	if handler != nil {
		handler.OnComplete()
	}

	logger.Infof("[inference_cycle] completed trace=%s stages=%d plan_type=%s confidence=%.2f duration=%s",
		payload.TraceID, len(ic.Stages),
		planTypeOf(ic.Plan), ic.Decision.Confidence, ic.Decision.TotalDuration)

	return &ic.Decision, nil
}
