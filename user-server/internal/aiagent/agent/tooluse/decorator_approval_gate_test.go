// decorator_approval_gate_test.go T-P1-05：审批门装饰器形态的契约。
//
// 与熔断那张卡同一套"三条对照缺一不可"的写法：
//
//	shadow 一律不拦            → 本卡的正确行为（冷触达照常外发）
//	shadow=false 确实会拦       → 证明装饰器真接上了，否则"没拦"只是"没接线"
//	非冷触达工具根本不被询问     → 证明挂门范围没有外溢到会话内回复类工具
//
// 有意不放 audit/ratelimit 的断言在这里：那几个装饰器的组合由 executor 侧的
// TestExecutor_ApprovalGatePosition 负责，本文件只管这一层自身的语义。
package tooluse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingColdTool 冷触达工具；executed 计数是"请求真到达工具本体"的唯一凭证。
type countingColdTool struct {
	BaseTool
	mu       sync.Mutex
	executed int
}

func newCountingColdTool(name string, category ToolCategory) *countingColdTool {
	return &countingColdTool{BaseTool: BaseTool{
		NameVal:     name,
		CategoryVal: category,
		ParamsVal:   ToolParameters{Type: "object", Properties: map[string]ToolParam{}},
	}}
}

func (c *countingColdTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	c.mu.Lock()
	c.executed++
	c.mu.Unlock()
	return SuccessResult(c.NameVal, "sent"), nil
}

func (c *countingColdTool) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.executed
}

// recordingApprovalChecker 记录每次询问，便于断言"问没问过"与"用什么 owner key 问的"。
type recordingApprovalChecker struct {
	mu       sync.Mutex
	approved bool
	asked    []string
	lastArgs [2]string
}

func (r *recordingApprovalChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, toolName)
	r.lastArgs = [2]string{toolName, accountIDorOwnerKey}
	return r.approved
}

func (r *recordingApprovalChecker) askedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.asked)
}

func (r *recordingApprovalChecker) ownerKey() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastArgs[1]
}

func runGate(t *testing.T, tool Tool, checker ApprovalChecker, shadow bool, ctx context.Context) (ToolResult, error) {
	t.Helper()
	cold := tool.(*countingColdTool)
	handler := ApprovalGateDecorator(tool, checker, shadow)(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return cold.Execute(ctx, args)
	})
	res, err := handler(ctx, map[string]any{})
	return res, err
}

// TestApprovalGateDecorator_ShadowNeverBlocks AC②：checker 判 false 时冷触达仍执行。
func TestApprovalGateDecorator_ShadowNeverBlocks(t *testing.T) {
	tool := newCountingColdTool("reach.telegram.dm", CategoryReach)
	checker := &recordingApprovalChecker{approved: false}

	for i := 0; i < 5; i++ {
		res, err := runGate(t, tool, checker, true, context.Background())
		if errors.Is(err, ErrApprovalDenied) {
			t.Fatalf("第 %d 次调用被审批门拦下（shadow 不得拦截）：%v", i+1, err)
		}
		if err != nil || !res.Success {
			t.Fatalf("第 %d 次调用应原样透传工具结果，实际 err=%v success=%v", i+1, err, res.Success)
		}
	}
	if tool.count() != 5 {
		t.Errorf("shadow 下 5 次调用应全部到达工具本体，实际 %d 次", tool.count())
	}
	if checker.askedCount() != 5 {
		t.Errorf("前置条件不成立：shadow 仍须询问 checker（否则「没拦」只是因为没判定），实际询问 %d 次", checker.askedCount())
	}
}

// TestApprovalGateDecorator_BlockDoesBlock 反向对照：shadow=false 时确实拦，且不打到工具本体。
func TestApprovalGateDecorator_BlockDoesBlock(t *testing.T) {
	tool := newCountingColdTool("reach.telegram.dm", CategoryReach)
	checker := &recordingApprovalChecker{approved: false}

	res, err := runGate(t, tool, checker, false, context.Background())
	if !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("err = %v, want ErrApprovalDenied（否则 shadow 的不拦毫无意义）", err)
	}
	if res.Success {
		t.Fatal("被拒时 result.Success 必须为 false")
	}
	if got := ClassifyToolError(err); got != ToolErrApprovalDenied {
		t.Errorf("错误码应为 %s（决定不进重试），实际 %s", ToolErrApprovalDenied, got)
	}
	if tool.count() != 0 {
		t.Errorf("被拒的调用不得到达工具本体，实际到达 %d 次", tool.count())
	}
}

// TestApprovalGateDecorator_ApprovedExecutesOnce checker 放行时不多跑也不漏跑。
func TestApprovalGateDecorator_ApprovedExecutesOnce(t *testing.T) {
	tool := newCountingColdTool("reach.batch_send.sms", CategoryReach)
	checker := &recordingApprovalChecker{approved: true}

	res, err := runGate(t, tool, checker, false, context.Background())
	if err != nil || !res.Success {
		t.Fatalf("放行时不应报错，实际 err=%v", err)
	}
	if tool.count() != 1 || checker.askedCount() != 1 {
		t.Errorf("期望询问 1 次 / 执行 1 次，实际 %d / %d", checker.askedCount(), tool.count())
	}
}

// TestApprovalGateDecorator_OwnerKeyResolution 审批主体口径：CallerID 优先，回退 AgentID，无上下文为空串。
func TestApprovalGateDecorator_OwnerKeyResolution(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			"caller 优先",
			WithToolContext(context.Background(), &ToolContext{CallerID: "u-1", AgentID: "a-1"}),
			"u-1",
		},
		{
			"回退 agent",
			WithToolContext(context.Background(), &ToolContext{AgentID: "a-1"}),
			"a-1",
		},
		{
			"无 ToolContext",
			context.Background(),
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool := newCountingColdTool("reach.lead_outreach", CategoryReach)
			checker := &recordingApprovalChecker{approved: true}
			if _, _ = runGate(t, tool, checker, false, c.ctx); checker.ownerKey() != c.want {
				t.Errorf("owner key = %q, want %q", checker.ownerKey(), c.want)
			}
		})
	}
}

// TestApprovalGateDecorator_OnlyColdOutreachAsked 挂门范围不外溢：非冷触达工具一次都不被询问。
func TestApprovalGateDecorator_OnlyColdOutreachAsked(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		category ToolCategory
		asked    bool
	}{
		{"warm reach 直发", "reach.weixin.send", CategoryReach, false},
		{"reach recall", "reach.recall", CategoryReach, false},
		{"非 reach 且名字像批量", "batch_send.email", CategoryKnowledge, false},
		{"cold dm", "reach.telegram.dm", CategoryReach, true},
		{"cold schedule", "reach.schedule.wecom", CategoryReach, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool := newCountingColdTool(c.toolName, c.category)
			checker := &recordingApprovalChecker{approved: false}
			// shadow=false：一旦被误挂就会被拦，用这个最强姿势反证"没被询问"
			res, err := runGate(t, tool, checker, false, context.Background())
			if checker.askedCount() != 0 && !c.asked {
				t.Fatalf("非冷触达工具不应询问审批门，实际询问 %d 次", checker.askedCount())
			}
			if c.asked {
				if checker.askedCount() == 0 {
					t.Fatal("冷触达工具必须被询问")
				}
				if !errors.Is(err, ErrApprovalDenied) {
					t.Fatalf("冷触达 + 未批准 + 非 shadow 应被拦，实际 err=%v", err)
				}
				return
			}
			if err != nil || !res.Success || tool.count() != 1 {
				t.Fatalf("未挂门的工具应原样执行，实际 err=%v success=%v 执行 %d 次", err, res.Success, tool.count())
			}
		})
	}
}

// TestApprovalGateDecorator_NilGuards checker/tool 为 nil 时必须透传（不 panic、不拦）。
func TestApprovalGateDecorator_NilGuards(t *testing.T) {
	tool := newCountingColdTool("reach.telegram.dm", CategoryReach)
	h := ApprovalGateDecorator(nil, &recordingApprovalChecker{approved: false}, false)(
		func(ctx context.Context, args map[string]any) (ToolResult, error) {
			return tool.Execute(ctx, args)
		})
	if res, err := h(context.Background(), nil); err != nil || !res.Success {
		t.Fatalf("tool 为 nil 时应透传，实际 err=%v", err)
	}

	h2 := ApprovalGateDecorator(tool, nil, false)(
		func(ctx context.Context, args map[string]any) (ToolResult, error) {
			return tool.Execute(ctx, args)
		})
	if res, err := h2(context.Background(), nil); err != nil || !res.Success {
		t.Fatalf("checker 为 nil 时应透传，实际 err=%v", err)
	}
	if tool.count() != 2 {
		t.Errorf("两次透传都应到达工具本体，实际 %d 次", tool.count())
	}
}

func newExecutorWithGate(tool Tool, checker ApprovalChecker, shadow bool) *ToolExecutor {
	reg := NewToolRegistry()
	_ = reg.Register(tool)
	return NewToolExecutor(reg, ToolExecutorConfig{
		DefaultTimeout:  5 * time.Second,
		ApprovalChecker: checker,
		ApprovalShadow:  shadow,
	})
}

// TestExecutor_ApprovalGatePosition 审批门必须在整条链之外：
// 被拦的调用不消耗限流令牌、不进重试、不产生审计记录。
func TestExecutor_ApprovalGatePosition(t *testing.T) {
	tool := newCountingColdTool("reach.telegram.dm", CategoryReach)
	limiter := NewTokenBucketLimiter(1, 1)
	audit := NewMemoryAuditLogger(10)
	reg := NewToolRegistry()
	_ = reg.Register(tool)
	exec := NewToolExecutor(reg, ToolExecutorConfig{
		DefaultTimeout:  5 * time.Second,
		RateLimiter:     limiter,
		RetryPolicy:     NewExponentialBackoffPolicy(3, time.Millisecond, 5*time.Millisecond),
		AuditLogger:     audit,
		ApprovalChecker: &recordingApprovalChecker{approved: false},
		ApprovalShadow:  false,
	})

	req := ExecuteRequest{ToolName: "reach.telegram.dm", Args: map[string]any{}, ToolCtx: &ToolContext{CallerID: "u-1"}}
	for i := 0; i < 4; i++ {
		if res := exec.Execute(context.Background(), req); !errors.Is(res.Err, ErrApprovalDenied) {
			t.Fatalf("第 %d 次调用应被审批门拦下，实际 err=%v", i+1, res.Err)
		}
	}
	if tool.count() != 0 {
		t.Errorf("被拦的调用不得到达工具本体，实际 %d 次", tool.count())
	}
	if n := audit.Count(); n != 0 {
		t.Errorf("被拦的调用不应留下执行审计，实际 %d 条", n)
	}
	// 令牌桶容量 1：若门在限流之内，第 2 次起就会拿到 rate-limited 而不是 approval-denied。
	// 上面 4 次全部返回 ErrApprovalDenied 即是"门在限流之外"的证据。
}

// TestExecutor_ApprovalGateShadowPassesExecutor AC② 的 executor 级锁定：
// 全套装饰器都在时，shadow 仍然放行。
func TestExecutor_ApprovalGateShadowPassesExecutor(t *testing.T) {
	tool := newCountingColdTool("reach.batch_send.sms", CategoryReach)
	checker := &recordingApprovalChecker{approved: false}
	reg := NewToolRegistry()
	_ = reg.Register(tool)
	exec := NewToolExecutor(reg, ToolExecutorConfig{
		DefaultTimeout:  5 * time.Second,
		RateLimiter:     NewTokenBucketLimiter(20, 50),
		RetryPolicy:     NewExponentialBackoffPolicy(3, time.Millisecond, 5*time.Millisecond),
		AuditLogger:     NewMemoryAuditLogger(10),
		ApprovalChecker: checker,
		ApprovalShadow:  true,
	})

	for i := 0; i < 6; i++ {
		res := exec.Execute(context.Background(), ExecuteRequest{
			ToolName: "reach.batch_send.sms",
			Args:     map[string]any{},
			ToolCtx:  &ToolContext{AgentID: "a-9"},
		})
		if errors.Is(res.Err, ErrApprovalDenied) {
			t.Fatalf("第 %d 次被拦（shadow 不得拦）：%v", i+1, res.Err)
		}
		if res.Err != nil || !res.Success {
			t.Fatalf("第 %d 次应成功，实际 err=%v", i+1, res.Err)
		}
	}
	if tool.count() != 6 {
		t.Errorf("6 次调用应全部外发，实际 %d 次", tool.count())
	}
	if checker.ownerKey() != "a-9" {
		t.Errorf("owner key 应回退到 AgentID，实际 %q", checker.ownerKey())
	}
}

// TestExecutor_ApprovalGateDoesNotBleedToWarmTools 接审批门唯一的外溢风险，是门挂到了
// 不该挂的工具上：会话内回复类工具占绝大多数，它们一旦被询问，"默认拒绝"的白名单语义
// 就会把正常回复变成错误。
//
// 做法：同名同类的两份工具，一份配"拒绝型 checker + 非 shadow"，一份不配 checker，
// 两者结果必须逐字段一致、且 checker 一次都不被询问。最后一行是冷触达对照，
// 证明这套装置本身是活的（否则上面的一致什么也说明不了）。
func TestExecutor_ApprovalGateDoesNotBleedToWarmTools(t *testing.T) {
	cases := []struct {
		toolName string
		category ToolCategory
		cold     bool
	}{
		{"reach.weixin.send", CategoryReach, false},
		{"reach.recall", CategoryReach, false},
		{"reach.account.list", CategoryReach, false},
		{"batch_send.email", CategoryKnowledge, false},
		{"customer.reply", CategoryCustomer, false},
		{"reach.telegram.dm", CategoryReach, true},
	}
	for _, c := range cases {
		t.Run(c.toolName, func(t *testing.T) {
			gated := newCountingColdTool(c.toolName, c.category)
			baseline := newCountingColdTool(c.toolName, c.category)
			checker := &recordingApprovalChecker{approved: false}

			resGated := newExecutorWithGate(gated, checker, false).
				Execute(context.Background(), ExecuteRequest{ToolName: c.toolName, Args: map[string]any{}})
			resBaseline := newExecutorWithGate(baseline, nil, false).
				Execute(context.Background(), ExecuteRequest{ToolName: c.toolName, Args: map[string]any{}})

			if !c.cold {
				if checker.askedCount() != 0 {
					t.Errorf("非冷触达工具被询问了 %d 次（门外溢）", checker.askedCount())
				}
				if resGated.Err != nil || !resGated.Success {
					t.Fatalf("非冷触达工具应原样成功，实际 err=%v", resGated.Err)
				}
				if resGated.Data != resBaseline.Data {
					t.Errorf("接门与未接门结果不一致：gated=%v baseline=%v", resGated.Data, resBaseline.Data)
				}
				if gated.count() != 1 || baseline.count() != 1 {
					t.Errorf("两者都应各执行一次，实际 %d / %d", gated.count(), baseline.count())
				}
				return
			}
			if checker.askedCount() != 1 {
				t.Errorf("冷触达对照：checker 应被询问 1 次，实际 %d 次", checker.askedCount())
			}
			if !errors.Is(resGated.Err, ErrApprovalDenied) {
				t.Fatalf("冷触达对照：非 shadow + 未批准应被拦，实际 err=%v（这条不成立则上面的「一致」无意义）", resGated.Err)
			}
		})
	}
}
