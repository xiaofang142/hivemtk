package tooluse

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
)

// N-2 验收：请求路径并发工具执行的 goroutine panic 必须被 SafeGo 兜底——
// panic 前该形状会击穿整个测试进程（go test 直接 exit 2），panic 后进程存活、
// 调用方拿到结构化结果（失败计数），wg 不悬挂（Done 在 panic 展开时照常执行）。

// TestBatchExecute_PanicInGoroutineDoesNotKillProcess
// ToolTraceSink 在 Execute 管线尾部被调（handler 自身的 panic 已被 TimeoutDecorator
// 转成 ErrToolPanic，到不了这里）——让 sink panic 即等价于「goroutine 体内 panic」。
func TestBatchExecute_PanicInGoroutineDoesNotKillProcess(t *testing.T) {
	origSink := ToolTraceSink
	ToolTraceSink = func(ctx context.Context, ev tracing.ToolTraceEvent) {
		panic("intentional trace-sink panic")
	}
	t.Cleanup(func() { ToolTraceSink = origSink })

	registry := NewToolRegistry()
	registry.MustRegister(echoTool("test.ok"))
	executor := NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout: 1 * time.Second,
	})

	req := BatchExecuteRequest{
		Parallel: true,
		Requests: []ExecuteRequest{
			{ToolName: "test.ok", Args: map[string]any{}},
			{ToolName: "test.ok", Args: map[string]any{}},
			{ToolName: "test.ok", Args: map[string]any{}},
		},
	}

	done := make(chan BatchExecuteResponse, 1)
	go func() {
		done <- executor.BatchExecute(context.Background(), req)
	}()

	select {
	case resp := <-done:
		// panic 的三条任务：results[idx] 未被赋值 → 零值 ExecuteResult → 计入 Failed
		if resp.FailedCount != 3 {
			t.Fatalf("panic 任务应计入 Failed（不悬挂、不崩溃），实际 failed=%d success=%d", resp.FailedCount, resp.SuccessCount)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BatchExecute 5s 未返回——panic 后 wg.Done 未执行导致死锁（保护回归）")
	}

	// 进程存活证明：还能继续跑一轮正常请求且成功
	ToolTraceSink = nil
	resp := executor.BatchExecute(context.Background(), req)
	if resp.SuccessCount != 3 {
		t.Fatalf("panic 后进程应存活且正常任务可继续执行，实际 success=%d", resp.SuccessCount)
	}
}

// TestDispatchByLLMToolCall_PanicInGoroutineDoesNotKillProcess
// LLM tool_call 并发调度路径（executor.go DispatchByLLMToolCall 的 SafeGo 化）。
// 只对 per-tool 事件 panic（Kind != SpanKindAgentTurn）：turn 级 trace 是调用方
// goroutine 里的同步代码（请求路径上有 gin.Recovery 兜底），不在本验收范围。
func TestDispatchByLLMToolCall_PanicInGoroutineDoesNotKillProcess(t *testing.T) {
	origSink := ToolTraceSink
	ToolTraceSink = func(ctx context.Context, ev tracing.ToolTraceEvent) {
		if ev.Kind != model.SpanKindAgentTurn {
			panic("intentional trace-sink panic")
		}
	}
	t.Cleanup(func() { ToolTraceSink = origSink })

	registry := NewToolRegistry()
	registry.MustRegister(echoTool("test.ok"))
	executor := NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout: 1 * time.Second,
	})

	done := make(chan []LLMToolResult, 1)
	go func() {
		done <- executor.DispatchByLLMToolCall(context.Background(), []LLMToolCall{
			{ID: "c1", Function: LLMToolFunction{Name: "test.ok", Arguments: "{}"}},
			{ID: "c2", Function: LLMToolFunction{Name: "test.ok", Arguments: "{}"}},
		}, nil)
	}()

	select {
	case results := <-done:
		if len(results) != 2 {
			t.Fatalf("panic 后调用方应拿到完整结果槽位（不崩溃、不悬挂），实际 %d 条", len(results))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DispatchByLLMToolCall 5s 未返回——panic 后 wg.Done 未执行导致死锁（保护回归）")
	}

	ToolTraceSink = nil
	results := executor.DispatchByLLMToolCall(context.Background(), []LLMToolCall{
		{ID: "c1", Function: LLMToolFunction{Name: "test.ok", Arguments: "{}"}},
	}, nil)
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("panic 后进程应存活且正常调度成功，实际 %+v", results)
	}
}

// TestFeedbackCollectorDecorator_PanicInSinkDoesNotKillProcess
// 工具事件落库路径（feedback_decorator.go 的 SafeGo 化）：sink panic 不影响主链路返回。
func TestFeedbackCollectorDecorator_PanicInSinkDoesNotKillProcess(t *testing.T) {
	var sinkCalled atomic.Bool
	panickingSink := &panickingFeedbackSink{onRecord: func() {
		sinkCalled.Store(true)
		panic("intentional sink panic")
	}}

	decorated := FeedbackCollectorDecorator(panickingSink)(echoTool("test.fb").ExecuteFn())

	res, err := decorated(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("sink panic 不能影响主链路，实际 err=%v", err)
	}
	if !res.Success {
		t.Fatalf("主链路结果不应被 sink panic 污染，实际 %+v", res)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sinkCalled.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !sinkCalled.Load() {
		t.Fatal("异步 sink 应已被调用（panic 发生在 goroutine 内）")
	}
	// 走到这里即进程存活证明：若 SafeGo 缺位，上面的 goroutine panic 已击穿测试进程。
}

type panickingFeedbackSink struct {
	onRecord func()
}

func (p *panickingFeedbackSink) RecordToolCall(ctx context.Context, event ToolCallEvent) error {
	p.onRecord()
	return errors.New("unreachable")
}

// echoTool 的 handler 直接复用（绕过注册表，聚焦 decorator 本身的保护）。
func (m *mockTool) ExecuteFn() ToolHandler {
	return m.executeFn
}
