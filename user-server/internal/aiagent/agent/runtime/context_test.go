package agent_runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
)

// ToolResult 的字段集在全仓只允许有一份定义（在 portcontract）。本包曾各自定义过一份，
// 少 error_code 与 card 两个字段 ⇒ 同一份工具结果 JSON 在两条链路上读出不同字段集。
// 若有人再改回本地结构体，下面的类型别名赋值会直接编不过。
func TestToolResultIsTheSharedContractType(t *testing.T) {
	var _ portcontract.ToolResult = ToolResult{}

	raw := ToolResult{
		Error:     "上游超时",
		ErrorCode: "TOOL_TIMEOUT",
		Card:      &model.RichCard{Type: "generic", Title: "回执"},
	}.ToJSON()

	for _, want := range []string{`"error_code":"TOOL_TIMEOUT"`, `"card":{`} {
		if !strings.Contains(raw, want) {
			t.Errorf("ToJSON() 应含 %s，实际 %s", want, raw)
		}
	}
}

type mockLLMClient struct {
	response string
	err      error
}

func (c *mockLLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	return c.response, c.err
}

func TestSimpleTokenEstimator_Estimate(t *testing.T) {
	estimator := &SimpleTokenEstimator{}

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"空文本", "", 0},
		{"中文", "你好世界", 8},
		{"英文", "hello world", 2},
		{"混合", "你好world", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimator.Estimate(tt.text)
			if result != tt.expected {
				t.Errorf("Estimate(%s) = %d, want %d", tt.text, result, tt.expected)
			}
		})
	}
}

func TestSummarizationCompressor_ShouldCompress(t *testing.T) {
	llmClient := &mockLLMClient{response: "摘要"}
	compressor := NewSummarizationCompressor(llmClient, 0.8)

	messages := []Message{
		{Role: "user", Content: "这是一个测试消息"},
		{Role: "assistant", Content: "这是回复"},
	}

	if compressor.ShouldCompress(messages, 1000) {
		t.Error("小消息不应该压缩")
	}

	largeMessages := make([]Message, 100)
	for i := range largeMessages {
		largeMessages[i] = Message{
			Role:    "user",
			Content: "这是一个很长的消息用于测试压缩功能",
		}
	}

	if !compressor.ShouldCompress(largeMessages, 100) {
		t.Error("大消息应该压缩")
	}
}

func TestSummarizationCompressor_Compress(t *testing.T) {
	llmClient := &mockLLMClient{response: "这是对话摘要"}
	compressor := NewSummarizationCompressor(llmClient, 0.8)

	messages := make([]Message, 20)
	for i := range messages {
		messages[i] = Message{
			Role:    "user",
			Content: "这是第" + string(rune('0'+i%10)) + "条消息",
		}
	}

	compressed, err := compressor.Compress(messages, 50)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	if len(compressed) >= len(messages) {
		t.Errorf("压缩失败，期望消息数减少，原始 %d，压缩后 %d", len(messages), len(compressed))
	}

	if compressed[0].Role != "system" {
		t.Errorf("第一条消息应该是摘要，实际 %s", compressed[0].Role)
	}
}

func TestToolCallHistory_AddAndGet(t *testing.T) {
	history := NewToolCallHistory()

	record := ToolCallRecord{
		CallID:   "call-1",
		ToolName: "test.tool",
		Args:     map[string]any{"key": "value"},
		Result: &ToolResult{
			Success: true,
			Data:    "result",
		},
		ExecutedAt: time.Now(),
		Duration:   time.Second,
	}

	history.AddCall(record)

	if history.Count() != 1 {
		t.Errorf("Count() = %d, want 1", history.Count())
	}

	got, exists := history.GetCall("call-1")
	if !exists {
		t.Fatal("GetCall() not found")
	}

	if got.ToolName != "test.tool" {
		t.Errorf("ToolName = %s, want test.tool", got.ToolName)
	}

	result, exists := history.GetResult("call-1")
	if !exists {
		t.Fatal("GetResult() not found")
	}

	if !result.Success {
		t.Error("Result.Success should be true")
	}
}

func TestToolCallHistory_GetCallsByTool(t *testing.T) {
	history := NewToolCallHistory()

	history.AddCall(ToolCallRecord{CallID: "call-1", ToolName: "tool.a"})
	history.AddCall(ToolCallRecord{CallID: "call-2", ToolName: "tool.b"})
	history.AddCall(ToolCallRecord{CallID: "call-3", ToolName: "tool.a"})

	calls := history.GetCallsByTool("tool.a")
	if len(calls) != 2 {
		t.Errorf("GetCallsByTool(tool.a) = %d, want 2", len(calls))
	}

	calls = history.GetCallsByTool("tool.b")
	if len(calls) != 1 {
		t.Errorf("GetCallsByTool(tool.b) = %d, want 1", len(calls))
	}
}

func TestToolCallHistory_ToMessages(t *testing.T) {
	history := NewToolCallHistory()

	history.AddCall(ToolCallRecord{
		CallID:     "call-1",
		ToolName:   "test.tool",
		Args:       map[string]any{"key": "value"},
		Result:     &ToolResult{Success: true, Data: "result"},
		ExecutedAt: time.Now(),
		Duration:   time.Second,
	})

	messages := history.ToMessages()

	if len(messages) != 2 {
		t.Errorf("ToMessages() returned %d messages, want 2", len(messages))
	}

	if messages[0].Role != "assistant" {
		t.Errorf("First message role = %s, want assistant", messages[0].Role)
	}

	if messages[1].Role != "tool" {
		t.Errorf("Second message role = %s, want tool", messages[1].Role)
	}
}

func TestToolCallHistory_Clear(t *testing.T) {
	history := NewToolCallHistory()

	history.AddCall(ToolCallRecord{CallID: "call-1"})
	history.AddCall(ToolCallRecord{CallID: "call-2"})

	history.Clear()

	if history.Count() != 0 {
		t.Errorf("Count() after Clear() = %d, want 0", history.Count())
	}
}

func TestChannelStreamHandler(t *testing.T) {
	handler := NewChannelStreamHandler(10)

	handler.OnEvent(StreamEvent{Type: StreamEventToolCall, ToolName: "test.tool"})
	handler.OnEvent(StreamEvent{Type: StreamEventToolResult, Content: "result"})
	handler.OnComplete()

	events := handler.Events()
	count := 0
	for range events {
		count++
	}

	if count != 3 {
		t.Errorf("Received %d events, want 3", count)
	}
}
