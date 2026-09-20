// risk_gate_test.go T-P3-05 AC②：分级判定层的语义锁定。
//
// 本卡的边界写得很死——"只记录判定，不改放行结果"。因此这一层最难锁的性质是
// **它没有拒绝路径**：判定结论只能以 RiskDecision 的形式流出去，绝不能变成
// ToolResult。一旦有人"顺手"把 would_deny 接成 return error，AC② 就静默失效，
// 而表现是某些 Agent 的工具调用开始报错——那正是 P9 才该做的决定。
//
// 所以这里的断言分两组：一组证明判定算得对（否则 P9 评审的是假数据），
// 一组证明算错了也不会拦人（shadow 的结构保证）。
package tooluse

import (
	"context"
	"testing"
)

// stubGrants 只做"哪个 Agent 白名单里有哪些工具"的查表，语义与
// WhitelistPermissionChecker.agentWhitelist 一致：没配置 = 空白名单（不是"全授权"）。
type stubGrants struct {
	byAgent map[string][]string
}

func (s *stubGrants) AgentHasTool(agentID, toolName string) bool {
	for _, t := range s.byAgent[agentID] {
		if t == toolName {
			return true
		}
	}
	return false
}

func (s *stubGrants) AgentWhitelistConfigured(agentID string) bool {
	_, ok := s.byAgent[agentID]
	return ok
}

func (s *stubGrants) ListConfiguredAgents() []string {
	out := make([]string, 0, len(s.byAgent))
	for id := range s.byAgent {
		out = append(out, id)
	}
	return out
}

func (s *stubGrants) ListAgentWhitelist(agentID string) []string { return s.byAgent[agentID] }

func TestRiskVerdict_HighWriteNeedsAgentsOwnWhitelist(t *testing.T) {
	grants := &stubGrants{byAgent: map[string][]string{
		"agent-granted":   {"reach.sms.send"},
		"agent-other":     {"reach.email.send"},
		"agent-wildcard":  {"*"},
		"agent-unrelated": {"customer.get"},
	}}
	high := &riskStubTool{BaseTool{NameVal: "reach.sms.send", RiskVal: RiskHighWrite}}

	cases := []struct {
		name       string
		agentID    string
		wantDeny   bool
		wantReason string
	}{
		{"自己的白名单里有这个工具⇒不拦", "agent-granted", false, RiskReasonGranted},
		{"白名单里没有⇒拦", "agent-other", true, RiskReasonNotGranted},
		{"完全没配置白名单⇒拦（P9 评审的关键量）", "agent-never-configured", true, RiskReasonWhitelistAbsent},
		{"未配置且 agentID 为空⇒无法归因，判拦并单独记原因", "", true, RiskReasonNoAgent},
		{"\"*\" 通配不算显式授权⇒拦：分级要的是逐工具授权", "agent-wildcard", true, RiskReasonNotGranted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := &ToolContext{AgentID: c.agentID}
			d := RiskVerdict(high, tc, grants)
			if d.WouldDeny != c.wantDeny {
				t.Errorf("would_deny=%v，期望 %v（reason=%s）", d.WouldDeny, c.wantDeny, d.Reason)
			}
			if d.Reason != c.wantReason {
				t.Errorf("reason=%s，期望 %s", d.Reason, c.wantReason)
			}
		})
	}
}

func TestRiskVerdict_NonHighWriteNeverJudged(t *testing.T) {
	grants := &stubGrants{byAgent: map[string][]string{}}
	for _, lvl := range []ToolRiskLevel{RiskReadonly, RiskLowWrite} {
		tool := &riskStubTool{BaseTool{NameVal: "any.tool", RiskVal: lvl}}
		d := RiskVerdict(tool, &ToolContext{AgentID: "nobody"}, grants)
		if d.WouldDeny || d.Reason != RiskReasonBelowHigh {
			t.Errorf("%s 工具被判 would_deny=%v reason=%s：分级闸门只管 high_write", lvl, d.WouldDeny, d.Reason)
		}
		if d.Level != lvl {
			t.Errorf("level=%s，期望 %s", d.Level, lvl)
		}
	}
}

// TestRiskVerdict_UndeclaredIsJudgedAsHighWrite 是 safe-by-default 的落点：
// 未声明工具在判定上必须与真高危**同路**，且原因里要能看出它是兜底顶上来的。
func TestRiskVerdict_UndeclaredIsJudgedAsHighWrite(t *testing.T) {
	// agent-granted 只被授权了 reach.sms.send：未声明工具不在这张表里，判定必须与
	// "真的声明了 high_write 但没被授权"的工具拿到同一结论 —— 兜底不是从轻处理。
	grants := &stubGrants{byAgent: map[string][]string{"agent-granted": {"reach.sms.send"}}}
	tc := &ToolContext{AgentID: "agent-granted"}

	undeclared := RiskVerdict(&noBaseTool{name: "third_party.outbound"}, tc, grants)
	declaredHigh := RiskVerdict(&riskStubTool{BaseTool{NameVal: "third_party.outbound", RiskVal: RiskHighWrite}}, tc, grants)
	if undeclared.Declared {
		t.Fatal("未声明工具被报成已声明：报告里会看不出它是兜底顶上来的")
	}
	if declaredHigh.Declared != true {
		t.Fatal("已声明工具被报成未声明")
	}
	if undeclared.Level != RiskHighWrite {
		t.Errorf("level=%s，期望 high_write", undeclared.Level)
	}
	if !undeclared.WouldDeny || undeclared.Reason != RiskReasonNotGranted {
		t.Errorf("would_deny=%v reason=%s：未声明工具不该因为「没人分级」而免于判定", undeclared.WouldDeny, undeclared.Reason)
	}
	if undeclared.WouldDeny != declaredHigh.WouldDeny || undeclared.Reason != declaredHigh.Reason {
		t.Errorf("兜底与真高危不同路：(%v,%s) vs (%v,%s)",
			undeclared.WouldDeny, undeclared.Reason, declaredHigh.WouldDeny, declaredHigh.Reason)
	}
}

// TestRiskGateDecorator_HasNoDenyPath 结构层保证：checker 说"会拦"，
// 装饰器依然把调用原样交给 next，且返回值与不挂门时逐字一致。
func TestRiskGateDecorator_HasNoDenyPath(t *testing.T) {
	tool := &riskStubTool{BaseTool{NameVal: "reach.sms.send", RiskVal: RiskHighWrite}}
	obs := NewMemoryRiskObserver(50)
	grants := &stubGrants{byAgent: map[string][]string{"agent-x": {"customer.get"}}}

	var innerCalls int
	next := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		innerCalls++
		return SuccessResult(tool.Name(), map[string]any{"ok": 1}), nil
	}
	handler := RiskGateDecorator(tool, grants, obs)(next)

	ctx := WithToolContext(context.Background(), &ToolContext{AgentID: "agent-x", CallerID: "caller-1"})
	res, err := handler(ctx, map[string]any{"to": "+8613800000000"})
	if err != nil {
		t.Fatalf("shadow 判定层返回了 error：%v —— 本卡不允许任何拒绝路径", err)
	}
	if !res.Success {
		t.Errorf("放行结果被改写：success=false content=%v", res.Data)
	}
	if innerCalls != 1 {
		t.Errorf("内层执行次数=%d，期望 1", innerCalls)
	}

	snap := obs.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("判定留痕数=%d，期望 1", len(snap))
	}
	d := snap[0]
	if !d.WouldDeny {
		t.Error("agent-x 的白名单里没有 reach.sms.send，判定却会是 would_deny=false：P9 拿到的是假数据")
	}
	if d.Allowed != true {
		t.Error("Allowed 必须如实记录「这次真的放行了」，与 WouldDeny 并存")
	}
	if d.ToolName != "reach.sms.send" || d.Level != RiskHighWrite || !d.Declared {
		t.Errorf("留痕字段不完整：%+v", d)
	}
	if d.AgentID != "agent-x" {
		t.Errorf("agent=%s，期望 agent-x：没有归因的观察数据无法用于评审", d.AgentID)
	}
}

// TestRiskGateDecorator_NilPartsAreInert 未接线（grants/observer 为 nil）时必须纯透传，
// 且不能留痕、不能 panic —— 旗子 off 时装配处就是传 nil 进来。
func TestRiskGateDecorator_NilPartsAreInert(t *testing.T) {
	tool := &riskStubTool{BaseTool{NameVal: "reach.sms.send", RiskVal: RiskHighWrite}}
	next := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return SuccessResult(tool.Name(), map[string]any{"ran": true}), nil
	}
	handler := RiskGateDecorator(tool, nil, nil)(next)
	res, err := handler(context.Background(), nil)
	if err != nil || !res.Success {
		t.Fatalf("nil 依赖下行为被改变：err=%v success=%v", err, res.Success)
	}
}

// TestMemoryRiskObserver_BoundedAndCounted 环形缓冲必须有界（每工具每次调用都留一行，
// 不设上限就是一个随流量线性增长的内存泄漏），计数则必须无界（累计量才是评审要的）。
func TestMemoryRiskObserver_BoundedAndCounted(t *testing.T) {
	obs := NewMemoryRiskObserver(10)
	for i := 0; i < 25; i++ {
		obs.Observe(context.Background(), RiskDecision{
			ToolName: "reach.sms.send", Level: RiskHighWrite, Declared: true,
			Allowed: true, WouldDeny: i%2 == 0, Reason: RiskReasonNotGranted,
		})
	}
	if got := len(obs.Snapshot()); got != 10 {
		t.Errorf("快照长度=%d，期望被上限截为 10", got)
	}
	counts := obs.Counts()["reach.sms.send"]
	if counts.Calls != 25 {
		t.Errorf("Calls=%d，期望 25（留痕有界不能影响计数）", counts.Calls)
	}
	if counts.WouldDeny != 13 {
		t.Errorf("WouldDeny=%d，期望 13", counts.WouldDeny)
	}
}
