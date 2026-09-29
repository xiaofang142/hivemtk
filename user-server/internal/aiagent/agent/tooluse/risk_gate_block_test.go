// risk_gate_block_test.go T-P9-01：阻断装饰器的语义锁定。
//
// 与 risk_gate_test.go（锁 shadow"没有拒绝路径"）对称：本文件锁 block"拒绝当且仅当
// would_deny"。两组测试共用 stubGrants/countingTool 两套替身，断言互为镜像 ——
// 同一份 grants 下 shadow 放行、block 拒人，P9 的"观察期数据可比"才有测试背书。
package tooluse

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newRiskEnforcedExecutor(t *testing.T, tool Tool, grants AgentGrantReader, obs RiskObserver) *ToolExecutor {
	t.Helper()
	registry := NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}
	return NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout:    5 * time.Second,
		PermissionChecker: NewWhitelistPermissionChecker(),
		RateLimiter:       NewTokenBucketLimiter(100, 100),
		RetryPolicy:       NewExponentialBackoffPolicy(1, 10*time.Millisecond, 100*time.Millisecond),
		AuditLogger:       NewMemoryAuditLogger(100),
		CostTracker:       NewMemoryCostTracker(),
		RiskGrants:        grants,
		RiskObserver:      obs,
		RiskEnforce:       true,
	})
}

// 阻断：高危 + 该 Agent 没逐条授权 ⇒ 拒，工具本体一次都不跑。
func TestRiskGateBlock_DeniesUngrantedHighWrite(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"customer.get"}}}
	obs := NewMemoryRiskObserver(100)
	exec := newRiskEnforcedExecutor(t, tool, grants, obs)

	res, err := exec.ExecuteByNameWithCtx(context.Background(), "reach.sms.send", map[string]any{"to": "x"},
		&ToolContext{AgentID: "agent-a", CallerID: "op-1"})
	if !errors.Is(err, ErrRiskDenied) {
		t.Fatalf("阻断态未授权高危调用 err=%v，期望 ErrRiskDenied", err)
	}
	if res.Success {
		t.Errorf("被拒调用的 Success=true，期望 false")
	}
	if tool.calls != 0 {
		t.Errorf("被拒后工具仍执行 %d 次，期望 0（拒在整条链之外）", tool.calls)
	}
	kept := obs.Snapshot()
	if len(kept) != 1 {
		t.Fatalf("留痕条数=%d，期望 1（拒了也要记）", len(kept))
	}
	if kept[0].Allowed || !kept[0].WouldDeny {
		t.Errorf("留痕 Allowed=%v WouldDeny=%v，期望 false/true", kept[0].Allowed, kept[0].WouldDeny)
	}
}

// 放行：高危 + 在该 Agent 自己的白名单里 ⇒ 过，Allowed=true。
func TestRiskGateBlock_AllowsGrantedHighWrite(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"reach.sms.send"}}}
	obs := NewMemoryRiskObserver(100)
	exec := newRiskEnforcedExecutor(t, tool, grants, obs)

	res, err := exec.ExecuteByNameWithCtx(context.Background(), "reach.sms.send", map[string]any{"to": "x"},
		&ToolContext{AgentID: "agent-a", CallerID: "op-1"})
	if err != nil {
		t.Fatalf("已授权高危调用被拒：%v", err)
	}
	if !res.Success || tool.calls != 1 {
		t.Errorf("放行结果异常：Success=%v calls=%d", res.Success, tool.calls)
	}
	kept := obs.Snapshot()
	if len(kept) != 1 || !kept[0].Allowed || kept[0].WouldDeny {
		t.Errorf("放行留痕异常：%+v", kept)
	}
}

// 非高危永不参与判定：没配任何白名单的 Agent 调 readonly，阻断态也放行。
func TestRiskGateBlock_BelowHighAlwaysPasses(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "customer.get", CategoryVal: CategoryCustomer, RiskVal: RiskReadonly}}
	grants := &stubGrants{byAgent: map[string][]string{}}
	obs := NewMemoryRiskObserver(100)
	exec := newRiskEnforcedExecutor(t, tool, grants, obs)

	res, err := exec.ExecuteByNameWithCtx(context.Background(), "customer.get", map[string]any{"id": "c1"},
		&ToolContext{AgentID: "agent-nobody", CallerID: "op-1"})
	if err != nil || !res.Success || tool.calls != 1 {
		t.Errorf("非高危被拦：err=%v Success=%v calls=%d", err, res.Success, tool.calls)
	}
}

// 未接线透传：RiskEnforce=true 但 grants/observer 为 nil ⇒ 与接线前一致。
func TestRiskGateBlock_NilWiringPassthrough(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	exec := newRiskEnforcedExecutor(t, tool, nil, nil)

	res, err := exec.ExecuteByNameWithCtx(context.Background(), "reach.sms.send", map[string]any{"to": "x"},
		&ToolContext{AgentID: "agent-a", CallerID: "op-1"})
	if err != nil || !res.Success || tool.calls != 1 {
		t.Errorf("未接线被拦：err=%v Success=%v calls=%d", err, res.Success, tool.calls)
	}
}

// 回归锁：同一份"会被拦"的 grants 下，shadow 态（RiskEnforce=false）必须照旧放行。
// 保证新增的挂载分支没有改动观察路径的一个字节。
func TestRiskGateShadow_UnchangedByEnforceBranch(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"customer.get"}}}
	obs := NewMemoryRiskObserver(100)
	registry := NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}
	exec := NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout:    5 * time.Second,
		PermissionChecker: NewWhitelistPermissionChecker(),
		RateLimiter:       NewTokenBucketLimiter(100, 100),
		RetryPolicy:       NewExponentialBackoffPolicy(1, 10*time.Millisecond, 100*time.Millisecond),
		AuditLogger:       NewMemoryAuditLogger(100),
		CostTracker:       NewMemoryCostTracker(),
		RiskGrants:        grants,
		RiskObserver:      obs,
		RiskEnforce:       false,
	})

	res, err := exec.ExecuteByNameWithCtx(context.Background(), "reach.sms.send", map[string]any{"to": "x"},
		&ToolContext{AgentID: "agent-a", CallerID: "op-1"})
	if err != nil || !res.Success || tool.calls != 1 {
		t.Errorf("shadow 态放行被改写：err=%v Success=%v calls=%d", err, res.Success, tool.calls)
	}
}
