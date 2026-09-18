package tooluse

import (
	"context"
	"sync"
	"testing"
)

// keyRecordingLimiter 记录每次 Acquire 收到的 key，用于锁定限流键维度。
type keyRecordingLimiter struct {
	mu   sync.Mutex
	keys []string
}

func (l *keyRecordingLimiter) Acquire(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = append(l.keys, key)
	return nil
}

func (l *keyRecordingLimiter) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.keys...)
}

// recordingTool 最小可注册工具。
type recordingTool struct {
	BaseTool
	calls int
	mu    sync.Mutex
}

func (t *recordingTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return SuccessResult(t.NameVal, map[string]any{"ok": true}), nil
}

func newRecordingTool(name string) *recordingTool {
	return &recordingTool{
		BaseTool: BaseTool{
			NameVal:        name,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "test tool",
			ParamsVal:      ToolParameters{Type: "object"},
		},
	}
}

// TestDefaultKeyBuilder_UsesAgentIDSuffix 锁定 ToolRouter 的限流键维度：toolName:AgentID。
func TestDefaultKeyBuilder_UsesAgentIDSuffix(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		tc       *ToolContext
		want     string
	}{
		{"带 AgentID", "reach.sms", &ToolContext{AgentID: "agent-9"}, "reach.sms:agent-9"},
		{"AgentID 为空退化裸名", "reach.sms", &ToolContext{CallerID: "user-77"}, "reach.sms"},
		{"tc 为 nil 退化裸名", "reach.sms", nil, "reach.sms"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defaultKeyBuilder(c.toolName, c.tc); got != c.want {
				t.Fatalf("keyBuilder(%q, %+v) = %q，期望 %q", c.toolName, c.tc, got, c.want)
			}
		})
	}
}

// TestExecutorAndRouter_KeyDimensionsAreDistinct 锁定两道限流门的键维度不同：
//
//	Executor 装饰器 = CallerID:toolName（按调用方配额）
//	Router          = toolName:AgentID（按智能体配额）
//
// 这正是两者必须各持独立 TokenBucket 实例的原因：合并实例会让两个维度争用同一份配额。
func TestExecutorAndRouter_KeyDimensionsAreDistinct(t *testing.T) {
	tc := &ToolContext{CallerID: "user-77", AgentID: "agent-9"}
	const toolName = "reach.sms"

	executorKey := toolName
	if tc.CallerID != "" {
		executorKey = tc.CallerID + ":" + toolName
	}
	routerKey := defaultKeyBuilder(toolName, tc)

	if executorKey != "user-77:reach.sms" {
		t.Fatalf("Executor 键维度漂移：实际 %q", executorKey)
	}
	if routerKey != "reach.sms:agent-9" {
		t.Fatalf("Router 键维度漂移：实际 %q", routerKey)
	}
	if executorKey == routerKey {
		t.Fatalf("两个键不应相等（相等说明共享实例是安全的，与本测试前提矛盾）")
	}
}

// TestRouterAndExecutor_GatesRunInSeries 锁定一次 Route 会依次经过两道限流门，
// 各自恰好计量 1 次。串联意味着整体吞吐取两道中较小者，而不是两者相加。
func TestRouterAndExecutor_GatesRunInSeries(t *testing.T) {
	const toolName = "test.serial_tool"

	tool := newRecordingTool(toolName)
	registry := NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}

	execLimiter := &keyRecordingLimiter{}
	routerLimiter := &keyRecordingLimiter{}

	exec := NewToolExecutor(registry, ToolExecutorConfig{
		PermissionChecker: NoOpPermissionChecker{},
		RateLimiter:       execLimiter,
	})
	router := NewToolRouter(exec, routerLimiter, RouterConfig{})

	tc := &ToolContext{CallerID: "user-77", AgentID: "agent-9"}
	if res := router.Route(context.Background(), toolName, nil, tc); res.Err != nil {
		t.Fatalf("Route 失败：%v", res.Err)
	}

	execKeys := execLimiter.snapshot()
	routerKeys := routerLimiter.snapshot()

	if len(execKeys) != 1 {
		t.Fatalf("Executor 门应恰好计量 1 次，实际 %v", execKeys)
	}
	if len(routerKeys) != 1 {
		t.Fatalf("Router 门应恰好计量 1 次，实际 %v", routerKeys)
	}
	if execKeys[0] != "user-77:"+toolName {
		t.Fatalf("Executor 门键应为 CallerID:toolName，实际 %q", execKeys[0])
	}
	if routerKeys[0] != toolName+":agent-9" {
		t.Fatalf("Router 门键应为 toolName:AgentID，实际 %q", routerKeys[0])
	}
	if tool.calls != 1 {
		t.Fatalf("工具应执行 1 次，实际 %d", tool.calls)
	}
}

// TestSharedLimiter_DegradesToSameBucketOnlyWhenIDsEmpty 锁定唯一会让两道门争用同一桶的
// 退化场景：CallerID 与 AgentID 同时为空时，两个键构造器都产出裸 toolName。
// 这解释了为什么生产环境必须保持两个独立实例而不是共享。
func TestSharedLimiter_DegradesToSameBucketOnlyWhenIDsEmpty(t *testing.T) {
	const toolName = "reach.sms"

	bothEmpty := &ToolContext{}
	if execKey, routerKey := toolName, defaultKeyBuilder(toolName, bothEmpty); execKey != routerKey {
		t.Fatalf("双空 ID 时两个键应收敛为同一个：%q vs %q", execKey, routerKey)
	}

	callerOnly := &ToolContext{CallerID: "user-77"}
	execKey := callerOnly.CallerID + ":" + toolName
	if execKey == defaultKeyBuilder(toolName, callerOnly) {
		t.Fatalf("仅 CallerID 非空时两个键不应相等")
	}

	agentOnly := &ToolContext{AgentID: "agent-9"}
	if (agentOnly.CallerID + ":" + toolName) == defaultKeyBuilder(toolName, agentOnly) {
		t.Fatalf("仅 AgentID 非空时两个键不应相等")
	}
}
