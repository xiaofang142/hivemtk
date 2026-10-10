package tooluse

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// StreamState 状态枚举
type StreamState string

const (
	StateNormal       StreamState = "normal"
	StateDetected     StreamState = "detected"
	StateParsing      StreamState = "parsing"
	StateExecuting    StreamState = "executing"
	StateReassembling StreamState = "reassembling"
	StateDone         StreamState = "done"
)

// StreamEvent 状态机事件
type StreamEvent string

const (
	EventTriggerDetected StreamEvent = "trigger_detected"
	EventJSONComplete    StreamEvent = "json_complete"
	EventParseError      StreamEvent = "parse_error"
	EventToolExecuted    StreamEvent = "tool_executed"
	EventReassembleReady StreamEvent = "reassemble_ready"
	EventTimeout         StreamEvent = "timeout"
)

// StreamStateMachine 流式状态机
//
// 用途：在 LLM 流式输出过程中，识别工具调用触发词、截取 JSON、执行工具、二次组装
//
// 典型用法：
//
//	sm := NewStreamStateMachine()
//	sm.SetTrigger("调用工具：")
//	sm.SetToolExecutor(executor)
//	for chunk := range llmStream {
//	    action, err := sm.Process(ctx, chunk)
//	    switch action {
//	    case ActionForwardToClient: // 推到前端
//	    case ActionBuffer:          // 暂存
//	    case ActionExecuteTool:     // 执行工具
//	    case ActionDone:            // 结束
//	    }
//	}
type StreamStateMachine struct {
	mu sync.Mutex

	trigger   string
	maxBuffer int
	timeout   time.Duration

	state     StreamState
	buffer    strings.Builder
	toolName  string
	toolArgs  map[string]any
	jsonStart int
	jsonEnd   int

	executor ToolExecutorAPI

	transitions []StateTransition
	startTime   time.Time

	onToolCall func(toolName string, args map[string]any)
	onComplete func(reply string)
}

// StateTransition 状态转换记录
type StateTransition struct {
	From   StreamState `json:"from"`
	To     StreamState `json:"to"`
	Event  StreamEvent `json:"event"`
	Reason string      `json:"reason,omitempty"`
	At     time.Time   `json:"at"`
}

// ToolExecutorAPI 工具执行器接口（用于状态机）
//
// 状态机只需要"按名称+参数执行工具"的能力，通过接口解耦
type ToolExecutorAPI interface {
	ExecuteByName(ctx context.Context, toolName string, args map[string]any) (ToolResult, error)
}

// StreamAction 状态机处理结果（对调用方的指令）
type StreamAction string

const (
	ActionForwardToClient StreamAction = "forward"
	ActionBuffer          StreamAction = "buffer"
	ActionExecuteTool     StreamAction = "execute_tool"
	ActionDone            StreamAction = "done"
	ActionFail            StreamAction = "fail"
)

// NewStreamStateMachine 默认状态机
func NewStreamStateMachine() *StreamStateMachine {
	return &StreamStateMachine{
		trigger:   "调用工具：",
		maxBuffer: 64 * 1024,
		timeout:   30 * time.Second,
		state:     StateNormal,
	}
}

// SetTrigger 设置触发词（默认"调用工具："）
func (sm *StreamStateMachine) SetTrigger(t string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.trigger = t
}

// SetExecutor 设置工具执行器
func (sm *StreamStateMachine) SetExecutor(exec ToolExecutorAPI) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.executor = exec
}

// SetOnToolCall 设置工具调用回调
func (sm *StreamStateMachine) SetOnToolCall(cb func(toolName string, args map[string]any)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onToolCall = cb
}

// SetOnComplete 设置完成回调
func (sm *StreamStateMachine) SetOnComplete(cb func(reply string)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onComplete = cb
}

// State 获取当前状态
func (sm *StreamStateMachine) State() StreamState {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.state
}

// Process 处理一个 chunk
//
// 返回 (action, error)：
//   - ActionForwardToClient: 调用方应把 chunk 推给前端
//   - ActionBuffer: 调用方应丢弃 chunk（不推送）
//   - ActionExecuteTool: 调用方应执行工具（通过 sm.Executor）
//   - ActionDone: 流处理完成
//   - ActionFail: 处理失败（错误已写入 sm.LastError）
func (sm *StreamStateMachine) Process(ctx context.Context, chunk string) (StreamAction, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.startTime.IsZero() {
		sm.startTime = time.Now()
	}

	sm.buffer.WriteString(chunk)

	if time.Since(sm.startTime) > sm.timeout {
		sm.transition(StateDone, EventTimeout, "state machine timeout")
		return ActionFail, errors.New("stream state machine timeout")
	}

	if sm.buffer.Len() > sm.maxBuffer {
		return ActionFail, errors.New("buffer overflow")
	}

	switch sm.state {
	case StateNormal:
		if idx := strings.Index(sm.buffer.String(), sm.trigger); idx >= 0 {
			prefix := sm.buffer.String()[:idx]
			sm.buffer.Reset()
			sm.buffer.WriteString(prefix)
			sm.buffer.WriteString(chunk)
			sm.jsonStart = 0
			sm.transition(StateDetected, EventTriggerDetected, "trigger found at "+itoa(idx))
			return sm.processDetected()
		}
		sm.buffer.Reset()
		sm.buffer.WriteString(chunk)
		return ActionForwardToClient, nil

	case StateDetected, StateParsing:
		return sm.processDetected()

	case StateExecuting, StateReassembling:
		return ActionBuffer, nil

	case StateDone:
		return ActionDone, nil

	default:
		return ActionFail, errors.New("unknown state: " + string(sm.state))
	}
}

func (sm *StreamStateMachine) processDetected() (StreamAction, error) {
	jsonStartIdx := strings.Index(sm.buffer.String(), "{")
	if jsonStartIdx < 0 {
		sm.state = StateParsing
		return ActionBuffer, nil
	}

	endIdx, balanced := matchJSONEnd(sm.buffer.String(), jsonStartIdx)
	if !balanced {
		sm.state = StateParsing
		return ActionBuffer, nil
	}

	jsonStr := sm.buffer.String()[jsonStartIdx : endIdx+1]
	var tc struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
		sm.transition(StateDone, EventParseError, err.Error())
		return ActionFail, errors.New("parse tool json: " + err.Error())
	}

	if tc.Tool == "" {
		sm.transition(StateDone, EventParseError, "tool name empty")
		return ActionFail, errors.New("tool name empty")
	}

	sm.toolName = tc.Tool
	sm.toolArgs = tc.Args
	sm.jsonEnd = endIdx

	if sm.onToolCall != nil {
		sm.onToolCall(tc.Tool, tc.Args)
	}

	sm.transition(StateExecuting, EventJSONComplete, "parsed: "+tc.Tool)
	return ActionExecuteTool, nil
}

// MarkToolExecuted 标记工具执行完成（外部调用方在收到 ActionExecuteTool 后执行工具，然后调用本方法）
//
// 内部转换：Executing → Reassembling
func (sm *StreamStateMachine) MarkToolExecuted(_ ToolResult, _ error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.transition(StateReassembling, EventToolExecuted, "")
}

// MarkReassembled 标记二进宫组装完成
//
// 内部转换：Reassembling → Done
func (sm *StreamStateMachine) MarkReassembled() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.transition(StateDone, EventReassembleReady, "")
}

// MarkFailed 标记失败
func (sm *StreamStateMachine) MarkFailed(reason string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.transition(StateDone, EventParseError, reason)
}

func (sm *StreamStateMachine) transition(to StreamState, event StreamEvent, reason string) {
	sm.transitions = append(sm.transitions, StateTransition{
		From:   sm.state,
		To:     to,
		Event:  event,
		Reason: reason,
		At:     time.Now(),
	})
	sm.state = to
}

// Transitions 获取所有状态转换记录
func (sm *StreamStateMachine) Transitions() []StateTransition {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make([]StateTransition, len(sm.transitions))
	copy(out, sm.transitions)
	return out
}

// ToolName 获取解析出的工具名
func (sm *StreamStateMachine) ToolName() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.toolName
}

// ToolArgs 获取解析出的工具参数
func (sm *StreamStateMachine) ToolArgs() map[string]any {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.toolArgs
}

// Reset 重置（用于测试）
func (sm *StreamStateMachine) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state = StateNormal
	sm.buffer.Reset()
	sm.toolName = ""
	sm.toolArgs = nil
	sm.jsonStart = 0
	sm.jsonEnd = 0
	sm.transitions = nil
	sm.startTime = time.Time{}
}

func matchJSONEnd(s string, start int) (int, bool) {
	if start >= len(s) || s[start] != '{' {
		return -1, false
	}

	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
