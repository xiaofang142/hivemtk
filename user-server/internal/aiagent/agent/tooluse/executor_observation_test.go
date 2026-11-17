package tooluse

import (
	"context"
	"testing"
	"time"
)

func TestObservation_ExecutorHasOperationTime(t *testing.T) {
	tool := newTestTool("test.observation", SuccessResult("test.observation", "done"), nil)
	before := time.Now()
	_, event := runToolAndCapture(t, tool, ToolExecutorConfig{}, context.Background(), ExecuteRequest{})
	if event == nil || !event.DurationMeasured || event.StartedAt == nil || event.CompletedAt == nil ||
		event.StartedAt.Before(before) || event.CompletedAt.Before(*event.StartedAt) {
		t.Fatal("工具实测必须在调用前开始，在调用后结束，不以日志落库推导")
	}
	_, turn := runDispatchAndCaptureTurn(t, tool, ToolExecutorConfig{}, context.Background(), []LLMToolCall{
		{ID: "observation-call", Function: LLMToolFunction{Name: tool.Name(), Arguments: "{}"}},
	})
	if turn == nil || !turn.DurationMeasured || turn.StartedAt == nil || turn.CompletedAt == nil || turn.CompletedAt.Before(*turn.StartedAt) {
		t.Fatal("智能体工具轮同样需要独立操作边界")
	}
}
