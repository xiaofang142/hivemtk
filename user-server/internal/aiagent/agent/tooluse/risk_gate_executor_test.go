// risk_gate_executor_test.go T-P3-05 AC②：判定门穿过整条装饰器链后放行结果不变。
//
// 单测 RiskGateDecorator 不返回 error 只证明这一层干净；本文件锁的是**装配位置**：
// buildHandler 把门加在整条链之外，若哪天有人把它挪到 Retry/Timeout 之内、
// 或者让拒绝结论从 AuditDecorator 里漏出去，这里会红。
package tooluse

import (
	"context"
	"testing"
	"time"
)

func newRiskGatedExecutor(t *testing.T, tool Tool, grants AgentGrantReader, obs RiskObserver) *ToolExecutor {
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
	})
}

type countingTool struct {
	BaseTool
	calls int
}

func (c *countingTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	c.calls++
	return SuccessResult(c.Name(), map[string]any{"calls": c.calls}), nil
}

func TestRiskGate_EndToEndPassesHighWriteCallThrough(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"customer.get"}}}
	obs := NewMemoryRiskObserver(100)
	exec := newRiskGatedExecutor(t, tool, grants, obs)

	ctx := context.Background()
	res, err := exec.ExecuteByNameWithCtx(ctx, "reach.sms.send", map[string]any{"to": "x"},
		&ToolContext{AgentID: "agent-a", CallerID: "op-1"})
	if err != nil {
		t.Fatalf("shadow 判定门下调用被拒：%v", err)
	}
	if !res.Success {
		t.Errorf("放行结果被改写：%+v", res)
	}
	if tool.calls != 1 {
		t.Errorf("工具执行次数=%d，期望 1", tool.calls)
	}
	snap := obs.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("留痕数=%d，期望 1：AC② 的「只记录判定」要有记录才算", len(snap))
	}
	if !snap[0].WouldDeny || snap[0].Reason != RiskReasonNotGranted {
		t.Errorf("判定=%+v，期望 would_deny=true/agent_whitelist_excluded（agent-a 配了白名单、里面没有 reach.sms.send）", snap[0])
	}
	if !snap[0].Allowed {
		t.Error("Allowed 未记为 true：报告里会读成「这次没放行」")
	}
	// 审计链必须仍把它记成一次成功执行 —— 观察层不能污染既有审计口径。
	entries := exec.config.AuditLogger.(*MemoryAuditLogger).Entries()
	if len(entries) != 1 || !entries[0].Success {
		t.Errorf("审计=%+v，期望恰好 1 条且 Success=true", entries)
	}
}

// TestRiskGate_NotWiredIsByteIdentical 未接线（grants/observer 为 nil）时链上不该有这一层。
// 判据不是"没报错"，而是 observer 拿不到任何判定 —— 挂了门却没留痕，
// 与没挂门在数据上无法区分，正是 shadow 观察期最坏的失效方式。
func TestRiskGate_NotWiredIsByteIdentical(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	obs := NewMemoryRiskObserver(10)
	exec := newRiskGatedExecutor(t, tool, nil, obs)

	ctx := context.Background()
	if _, err := exec.ExecuteByNameWithCtx(ctx, "reach.sms.send", nil, &ToolContext{AgentID: "agent-a"}); err != nil {
		t.Fatalf("未接线场景调用失败：%v", err)
	}
	if n := len(obs.Snapshot()); n != 0 {
		t.Errorf("未接线却有 %d 条判定留痕", n)
	}
	if n := len(obs.Counts()); n != 0 {
		t.Errorf("未接线却有 %d 条计数", n)
	}
}

// TestRiskGate_RecordsEveryCallForNonHighWrite too：low_write 也各留一笔（below_high），
// 否则报告里"这一档一次都没被调用"和"这一档没被观察"分不开。
func TestRiskGate_RecordsBelowHighCalls(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "customer.update", CategoryVal: CategoryCustomer, RiskVal: RiskLowWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"customer.update"}}}
	obs := NewMemoryRiskObserver(10)
	exec := newRiskGatedExecutor(t, tool, grants, obs)

	ctx := context.Background()
	if _, err := exec.ExecuteByNameWithCtx(ctx, "customer.update", nil, &ToolContext{AgentID: "agent-a"}); err != nil {
		t.Fatalf("调用失败：%v", err)
	}
	snap := obs.Snapshot()
	if len(snap) != 1 || snap[0].WouldDeny || snap[0].Reason != RiskReasonBelowHigh {
		t.Errorf("留痕=%+v，期望 1 条 below_high 且 would_deny=false", snap)
	}
}

// TestRiskGate_SitsOutsideApprovalGate 锁定 buildHandler 里那句"比审批门还外层"。
//
// 判据不是读注释，而是让审批门真的拒一次：被拒的高危调用仍必须留下一条判定。
// 若有人把分级挪到审批门里侧，被拒调用就不再经过它，would_deny 统计会偏低，
// 而偏低的方向恰好是"转阻断后预计的破坏面比实际小"——这是最难在放量后被发现的一种失真。
func TestRiskGate_SitsOutsideApprovalGate(t *testing.T) {
	tool := &countingTool{BaseTool: BaseTool{NameVal: "reach.batch", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	grants := &stubGrants{byAgent: map[string][]string{"agent-a": {"reach.batch"}}}
	obs := NewMemoryRiskObserver(10)
	registry := NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	exec := NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout: 5 * time.Second,
		// block 态审批门（shadow=false）+ 恒拒绝的 checker ⇒ 调用必然被拒。
		ApprovalChecker: denyAllApprovalChecker{},
		ApprovalShadow:  false,
		RiskGrants:      grants,
		RiskObserver:    obs,
	})

	ctx := context.Background()
	_, err := exec.ExecuteByNameWithCtx(ctx, "reach.batch", nil, &ToolContext{AgentID: "agent-a"})
	if err == nil {
		t.Fatal("审批门 block 态却放行了：这个用例的前提（被拒）没成立，分级层也就没被检验")
	}
	snap := obs.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("被审批门拒掉的调用留下 %d 条判定，期望 1：分级层被挂到了审批门里侧", len(snap))
	}
	if tool.calls != 0 {
		t.Errorf("被拒的调用执行了 %d 次，期望 0", tool.calls)
	}
}

type denyAllApprovalChecker struct{}

func (denyAllApprovalChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	return false
}

func TestBuildRiskReport_ReflectsRegistryGrantsAndCounts(t *testing.T) {
	registry := NewToolRegistry()
	smsTool := &countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	batch := &countingTool{BaseTool: BaseTool{NameVal: "reach.batch", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}
	low := &countingTool{BaseTool: BaseTool{NameVal: "customer.update", CategoryVal: CategoryCustomer, RiskVal: RiskLowWrite}}
	undeclared := &noBaseTool{name: "third_party.tool"}
	for _, tool := range []Tool{smsTool, batch, low, undeclared} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("注册 %s 失败：%v", tool.Name(), err)
		}
	}
	grants := &stubGrants{byAgent: map[string][]string{
		"agent-a": {"reach.sms.send"},
		"agent-b": {"reach.email.send"},
	}}
	obs := NewMemoryRiskObserver(10)
	obs.Observe(context.Background(), RiskDecision{
		ToolName: "reach.sms.send", Level: RiskHighWrite, Declared: true, WouldDeny: false, Allowed: true,
	})
	obs.Observe(context.Background(), RiskDecision{
		ToolName: "reach.sms.send", Level: RiskHighWrite, Declared: true, WouldDeny: true, Allowed: true, Reason: RiskReasonWhitelistAbsent,
	})

	rep := BuildRiskReport(registry, grants, obs, "shadow", false, "FF_TOOL_PERMISSION_ENFORCE")
	if rep.Total != 4 {
		t.Errorf("total=%d，期望 4", rep.Total)
	}
	if rep.ByLevel["high_write"] != 3 || rep.ByLevel["low_write"] != 1 {
		t.Errorf("by_level=%v，期望 high_write=3 low_write=1（兜底计入高危）", rep.ByLevel)
	}
	if len(rep.Undeclared) != 1 || rep.Undeclared[0] != "third_party.tool" {
		t.Errorf("undeclared=%v，期望 [third_party.tool]", rep.Undeclared)
	}
	if rep.AgentWhitelistConfig != 2 {
		t.Errorf("agent_whitelist_configured=%d，期望 2", rep.AgentWhitelistConfig)
	}
	byName := map[string]RiskReportTool{}
	for _, row := range rep.Tools {
		byName[row.Name] = row
	}
	smsRow := byName["reach.sms.send"]
	if smsRow.GrantedAgents != 1 || len(smsRow.GrantedBy) != 1 || smsRow.GrantedBy[0] != "agent-a" {
		t.Errorf("reach.sms.send 授权面=%+v，期望只被 agent-a 授权", smsRow)
	}
	if smsRow.Calls != 2 || smsRow.WouldDeny != 1 {
		t.Errorf("reach.sms.send 计数 calls=%d would_deny=%d，期望 2/1", smsRow.Calls, smsRow.WouldDeny)
	}
	// 审批门按**名字段**启发式认冷触达，reach.sms.send 不在其列；分级按**声明**认后果。
	// 两者覆盖面不同这件事必须留在报告里（high_write_in_approval_gate）：
	// 它直接给出 P9 转阻断前审批门的盲区大小。
	if smsRow.ColdOutreach {
		t.Error("reach.sms.send 被标成 cold_outreach：审批门的判据是名字段，send 不在冷触达段清单里")
	}
	if !byName["reach.batch"].ColdOutreach {
		t.Error("reach.batch 未标 cold_outreach：覆盖面会虚报成 0，盲区大小无从判断")
	}
	if len(rep.HighWrite) != 3 || rep.HighWriteInGate != 1 {
		t.Errorf("high_write=%v 门内=%d，期望 3 个且只有 reach.batch 在门内", rep.HighWrite, rep.HighWriteInGate)
	}
	if byName["customer.update"].GrantedAgents != 0 {
		t.Errorf("customer.update 授权数=%d，期望 0", byName["customer.update"].GrantedAgents)
	}
	if rep.ObservationsPersisted {
		t.Error("ObservationsPersisted=true：内存观察不落库，报告不能声称跨重启可用")
	}
	if rep.ObservationsRetained != 2 {
		t.Errorf("observations_retained=%d，期望 2", rep.ObservationsRetained)
	}
}

// TestBuildRiskReport_NilGrantsSaysSo 未接权限检查器时授权列必须全 0 并如实反映在
// agent_whitelist_configured_agents 上 —— 否则 P9 看到的是"0 个 Agent 被授权"，
// 而真相是"没人提供授权数据"。
func TestBuildRiskReport_NilGrantsSaysSo(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(&countingTool{BaseTool: BaseTool{NameVal: "reach.sms.send", CategoryVal: CategoryReach, RiskVal: RiskHighWrite}}); err != nil {
		t.Fatal(err)
	}
	rep := BuildRiskReport(registry, nil, NewMemoryRiskObserver(10), "off", false, "FF_TOOL_PERMISSION_ENFORCE")
	if rep.AgentWhitelistConfig != 0 {
		t.Errorf("configured=%d，期望 0", rep.AgentWhitelistConfig)
	}
	if rep.Tools[0].GrantedAgents != 0 {
		t.Error("无授权源却报出授权数")
	}
	if rep.Mode != "off" || rep.BlocksWhenDenied {
		t.Errorf("mode/blocks=%s/%v，期望 off/false", rep.Mode, rep.BlocksWhenDenied)
	}
}
