package tooluse

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// DoubleInterceptOrchestrator 双拦截编排器
type DoubleInterceptOrchestrator struct {
	stateMachine *StreamStateMachine
	router       *ToolRouter
	secondPass   SecondPassLLM

	mu          sync.Mutex
	clientMsgs  []ClientMessage
	toolResults []ToolExecutionRecord
	stats       OrchestratorStats
}

// OrchestratorStats 编排器统计
type OrchestratorStats struct {
	FirstPassChunks   int
	InterceptedChunks int
	ToolExecutions    int
	SecondPassChunks  int
	FinalReplyLength  int
	TotalDuration     time.Duration
}

// ClientMessage 客户端消息（推送或缓存）
type ClientMessage struct {
	Content   string
	Forwarded bool
	Timestamp time.Time
}

// ToolExecutionRecord 工具执行记录
type ToolExecutionRecord struct {
	ToolName   string
	Args       map[string]any
	Result     ToolResult
	Err        error
	ExecutedAt time.Time
}

// SecondPassLLM 二次推理 LLM 接口
//
// 编排器在工具执行完毕后调用，传入工具结果，让 LLM 重新生成最终回复
type SecondPassLLM interface {
	GenerateReassembledReply(
		ctx context.Context,
		originalContent string,
		toolName string,
		toolResult ToolResult,
		chunkHandler func(chunk string),
	) (string, error)
}

// OrchestratorConfig 编排器配置
type OrchestratorConfig struct {
	Trigger      string
	Router       *ToolRouter
	StateMachine *StreamStateMachine
	SecondPass   SecondPassLLM
}

// NewDoubleInterceptOrchestrator 创建双拦截编排器
func NewDoubleInterceptOrchestrator(cfg OrchestratorConfig) (*DoubleInterceptOrchestrator, error) {
	if cfg.Router == nil {
		return nil, errors.New("router required")
	}
	if cfg.SecondPass == nil {
		return nil, errors.New("secondPass LLM required")
	}

	sm := cfg.StateMachine
	if sm == nil {
		sm = NewStreamStateMachine()
	}
	if cfg.Trigger != "" {
		sm.SetTrigger(cfg.Trigger)
	}

	return &DoubleInterceptOrchestrator{
		stateMachine: sm,
		router:       cfg.Router,
		secondPass:   cfg.SecondPass,
		clientMsgs:   make([]ClientMessage, 0),
		toolResults:  make([]ToolExecutionRecord, 0),
	}, nil
}

// Run 完整执行双拦截流程
//
// 入参：
//   - ctx: 上下文
//   - originalContent: 原始用户消息（用于二进宫）
//   - firstPassStream: 第一次 LLM 推理的流（chan string）
//
// 返回：最终回复 + 错误
func (o *DoubleInterceptOrchestrator) Run(ctx context.Context, originalContent string, firstPassStream <-chan string) (string, error) {
	o.stateMachine.Reset()
	o.mu.Lock()
	o.clientMsgs = o.clientMsgs[:0]
	o.toolResults = o.toolResults[:0]
	o.stats = OrchestratorStats{}
	start := time.Now()
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.stats.TotalDuration = time.Since(start)
		o.mu.Unlock()
	}()

	for chunk := range firstPassStream {
		o.stats.FirstPassChunks++
		action, err := o.stateMachine.Process(ctx, chunk)
		if err != nil {
			return "", err
		}

		switch action {
		case ActionForwardToClient:
			o.recordClientMessage(chunk, true)
		case ActionBuffer:
			o.stats.InterceptedChunks++
			o.recordClientMessage(chunk, false)
		case ActionExecuteTool:
			o.stats.InterceptedChunks++
			if err := o.executeToolCall(ctx, originalContent); err != nil {
				return "", err
			}
		case ActionDone:
		case ActionFail:
			return "", errors.New("state machine failed")
		}
	}

	if o.stateMachine.State() == StateReassembling || o.hasToolExecution() {
		reply, err := o.runSecondPass(ctx, originalContent)
		if err != nil {
			return "", err
		}
		o.stateMachine.MarkReassembled()
		o.stats.FinalReplyLength = len(reply)
		return reply, nil
	}

	return o.assembleDirectReply(), nil
}

func (o *DoubleInterceptOrchestrator) executeToolCall(ctx context.Context, originalContent string) error {
	toolName := o.stateMachine.ToolName()
	toolArgs := o.stateMachine.ToolArgs()

	record := ToolExecutionRecord{
		ToolName:   toolName,
		Args:       toolArgs,
		ExecutedAt: time.Now(),
	}

	result := o.router.Route(ctx, toolName, toolArgs, &ToolContext{
		CallerID:   "agent_runtime",
		AgentID:    "default",
		AuditTrace: "trace_" + originalContent,
	})
	record.Result = result.Result
	record.Err = result.Err

	o.mu.Lock()
	o.toolResults = append(o.toolResults, record)
	o.stats.ToolExecutions++
	o.mu.Unlock()

	o.stateMachine.MarkToolExecuted(result.Result, result.Err)

	return nil
}

func (o *DoubleInterceptOrchestrator) runSecondPass(ctx context.Context, originalContent string) (string, error) {
	o.mu.Lock()
	var lastRecord ToolExecutionRecord
	if len(o.toolResults) > 0 {
		lastRecord = o.toolResults[len(o.toolResults)-1]
	}
	o.mu.Unlock()

	var sb strings.Builder
	reply, err := o.secondPass.GenerateReassembledReply(
		ctx,
		originalContent,
		lastRecord.ToolName,
		lastRecord.Result,
		func(chunk string) {
			o.stats.SecondPassChunks++
			sb.WriteString(chunk)
		},
	)
	if err != nil {
		return "", err
	}
	_ = sb.String()

	o.recordClientMessage(reply, true)
	return reply, nil
}

func (o *DoubleInterceptOrchestrator) recordClientMessage(content string, forwarded bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.clientMsgs = append(o.clientMsgs, ClientMessage{
		Content:   content,
		Forwarded: forwarded,
		Timestamp: time.Now(),
	})
}

func (o *DoubleInterceptOrchestrator) hasToolExecution() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.toolResults) > 0
}

func (o *DoubleInterceptOrchestrator) assembleDirectReply() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var sb strings.Builder
	for _, m := range o.clientMsgs {
		if m.Forwarded {
			sb.WriteString(m.Content)
		}
	}
	return sb.String()
}

// GetClientMessages 获取客户端消息记录（用于审计）
func (o *DoubleInterceptOrchestrator) GetClientMessages() []ClientMessage {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]ClientMessage, len(o.clientMsgs))
	copy(out, o.clientMsgs)
	return out
}

// GetToolResults 获取工具执行记录
func (o *DoubleInterceptOrchestrator) GetToolResults() []ToolExecutionRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]ToolExecutionRecord, len(o.toolResults))
	copy(out, o.toolResults)
	return out
}

// GetStats 获取统计
func (o *DoubleInterceptOrchestrator) GetStats() OrchestratorStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stats
}

// StateMachine 获取状态机（外部可查询）
func (o *DoubleInterceptOrchestrator) StateMachine() *StreamStateMachine {
	return o.stateMachine
}
