package agent_runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------- fakes ----------

type fakeChainLookup struct {
	spec *IntentChainSpec
	err  error

	calls      int
	lastIntent string
}

func (f *fakeChainLookup) LookupIntentChain(ctx context.Context, intent string) (*IntentChainSpec, error) {
	f.calls++
	f.lastIntent = intent
	return f.spec, f.err
}

func chainIC(intent Intent, content, customerID string) *InferenceContext {
	return &InferenceContext{
		Payload: CustomerMessagePayload{
			Content:     content,
			CustomerID:  customerID,
			SessionID:   "sess-001",
			ChannelType: "douyin",
			AccountID:   "acc-9",
		},
		Intent: IntentResult{Primary: intent, Score: 0.8},
	}
}

// ---------- 未装配绑定层：既有行为零变化 ----------

func TestPlannerChain_NotWired_BehaviorUnchanged(t *testing.T) {
	p := NewDefaultTaskPlanner()
	ic := chainIC(IntentGreeting, "你好", "c-1")

	plan, err := p.Plan(context.Background(), ic)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if plan.PlanType != "customer_service" {
		t.Fatalf("expected既有 customer_service, got %s", plan.PlanType)
	}
	if plan.PlanSource != "" {
		t.Fatalf("未装配绑定层时 PlanSource 应为空, got %q", plan.PlanSource)
	}
}

// ---------- 命中绑定：按数据生成计划 ----------

func TestPlannerChain_Hit_BuildsPlanFromData(t *testing.T) {
	p := NewDefaultTaskPlanner()
	lk := &fakeChainLookup{spec: &IntentChainSpec{
		IntentCode: string(IntentOrderStatus),
		Steps: []IntentChainStep{
			{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}", "top_k": 3}, Priority: 1, Enabled: true},
			{ToolName: "logistics.track", Args: map[string]any{"note": "会话 {{session_id}} / 渠道 {{channel_type}} / 账号 {{account_id}}"}, Priority: 2, Enabled: true},
			{ToolName: "knowledge.search", Args: map[string]any{"query": "{{content}}"}, Priority: 3, Enabled: false},
		},
		ReplyHint: "先查订单再查物流",
	}}
	p.SetChainLookup(lk)

	plan, err := p.Plan(context.Background(), chainIC(IntentOrderStatus, "我的快递到哪了", "cust-42"))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if lk.calls != 1 || lk.lastIntent != string(IntentOrderStatus) {
		t.Fatalf("lookup 应恰被调 1 次且意图正确, calls=%d intent=%s", lk.calls, lk.lastIntent)
	}
	if plan.PlanType != "chain" || plan.PlanSource != "chain" {
		t.Fatalf("expected chain plan, got type=%s src=%s", plan.PlanType, plan.PlanSource)
	}
	if plan.Confidence != 0.90 {
		t.Fatalf("chain 命中置信度应 0.90, got %v", plan.Confidence)
	}
	if len(plan.ToolCalls) != 2 {
		t.Fatalf("禁用步应跳过: 恰 2 步, got %d", len(plan.ToolCalls))
	}
	if plan.ToolCalls[0].ToolName != "order.lookup" || plan.ToolCalls[0].Args["customer_id"] != "cust-42" {
		t.Fatalf("模板渲染 customer_id 失败: %+v", plan.ToolCalls[0])
	}
	if _, keep := plan.ToolCalls[0].Args["top_k"]; !keep {
		t.Fatal("非字符串模板值应原样透传")
	}
	if plan.ToolCalls[1].Args["note"] != "会话 sess-001 / 渠道 douyin / 账号 acc-9" {
		t.Fatalf("多变量渲染失败: %+v", plan.ToolCalls[1].Args)
	}
	if plan.ReplyHint != "先查订单再查物流" {
		t.Fatalf("ReplyHint 应取绑定层文本, got %q", plan.ReplyHint)
	}
}

// ---------- 回落矩阵：miss / error / 拒用 ----------

func TestPlannerChain_Miss_FallsBackToBuiltin(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{spec: nil}) // 无绑定

	plan, err := p.Plan(context.Background(), chainIC(IntentOrderStatus, "订单呢", "c-1"))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if plan.PlanType != "customer_service" {
		t.Fatalf("miss 应回落硬编码 customer_service, got %s", plan.PlanType)
	}
	if plan.PlanSource != "builtin:miss" {
		t.Fatalf("expected builtin:miss, got %q", plan.PlanSource)
	}
	found := false
	for _, c := range plan.ToolCalls {
		if c.ToolName == "order.lookup" {
			found = true
		}
	}
	if !found {
		t.Fatal("回落后应保留既有 order.lookup 计划")
	}
}

func TestPlannerChain_LookupError_FallsBackToBuiltin(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{err: errors.New("db down")})

	plan, err := p.Plan(context.Background(), chainIC(IntentRefund, "退款", "c-1"))
	if err != nil {
		t.Fatalf("lookup 错误不得上抛: %v", err)
	}
	if plan.PlanType != "customer_service" || plan.PlanSource != "builtin:error" {
		t.Fatalf("error 应回落硬编码, got type=%s src=%s", plan.PlanType, plan.PlanSource)
	}
}

func TestPlannerChain_UnknownTemplateVar_RejectsChain(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{spec: &IntentChainSpec{
		IntentCode: "order_status",
		Steps: []IntentChainStep{
			{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{evil_var}}"}, Enabled: true},
		},
	}})

	plan, err := p.Plan(context.Background(), chainIC(IntentOrderStatus, "查单", "c-1"))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if plan.PlanSource != "builtin:rejected" {
		t.Fatalf("白名单外变量应整链拒用回落, got src=%q", plan.PlanSource)
	}
	for _, c := range plan.ToolCalls {
		if s, ok := c.Args["customer_id"].(string); ok && strings.Contains(s, "{{evil_var}}") {
			t.Fatal("白名单外模板变量不得漏进工具参数")
		}
	}
}

func TestPlannerChain_EmptyToolName_RejectsChain(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{spec: &IntentChainSpec{
		Steps: []IntentChainStep{{ToolName: "  ", Enabled: true}},
	}})
	plan, _ := p.Plan(context.Background(), chainIC(IntentInquiry, "多少钱", "c-1"))
	if plan.PlanSource != "builtin:rejected" {
		t.Fatalf("空工具名应拒链回落, got src=%q", plan.PlanSource)
	}
}

func TestPlannerChain_TooManySteps_RejectsChain(t *testing.T) {
	p := NewDefaultTaskPlanner()
	steps := make([]IntentChainStep, ChainMaxSteps+1)
	for i := range steps {
		steps[i] = IntentChainStep{ToolName: "knowledge.search", Enabled: true, Priority: i + 1}
	}
	p.SetChainLookup(&fakeChainLookup{spec: &IntentChainSpec{Steps: steps}})
	plan, _ := p.Plan(context.Background(), chainIC(IntentInquiry, "介绍下", "c-1"))
	if plan.PlanSource != "builtin:rejected" {
		t.Fatalf("超 %d 步应拒链回落, got src=%q", ChainMaxSteps, plan.PlanSource)
	}
}

func TestPlannerChain_EmptyReplyHint_FallsBackToDefaultHint(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{spec: &IntentChainSpec{
		Steps: []IntentChainStep{{ToolName: "order.lookup", Enabled: true}},
	}})
	plan, _ := p.Plan(context.Background(), chainIC(IntentOrderStatus, "查单", "c-1"))
	if plan.PlanType != "chain" {
		t.Fatalf("expected chain, got %s", plan.PlanType)
	}
	if plan.ReplyHint == "" {
		t.Fatal("绑定层无 ReplyHint 时应用默认提示兜底")
	}
}

// ---------- FAQ 短路优先于链 ----------

func TestPlannerChain_FAQShortCircuitWins(t *testing.T) {
	p := NewDefaultTaskPlanner()
	lk := &fakeChainLookup{spec: &IntentChainSpec{
		Steps: []IntentChainStep{{ToolName: "order.lookup", Enabled: true}},
	}}
	p.SetChainLookup(lk)
	p.FAQLibrary = func(ctx context.Context, content string) (bool, string) {
		return true, "标准答案"
	}

	plan, err := p.Plan(context.Background(), chainIC(IntentOrderStatus, "退货政策是什么", "c-1"))
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !plan.SkipLLM || plan.SkipReason != "faq_matched" {
		t.Fatalf("FAQ 命中应保持既有短路语义, got skip=%v reason=%s", plan.SkipLLM, plan.SkipReason)
	}
	if lk.calls != 0 {
		t.Fatalf("FAQ 短路后不得再查绑定层, got %d 次", lk.calls)
	}
}

// ---------- 阶段观测 ----------

func TestPlannerChain_ExecuteStageReasonCarriesSource(t *testing.T) {
	p := NewDefaultTaskPlanner()
	p.SetChainLookup(&fakeChainLookup{spec: &IntentChainSpec{
		Steps: []IntentChainStep{{ToolName: "order.lookup", Enabled: true}},
	}})
	ic := chainIC(IntentOrderStatus, "查单", "c-1")
	if r := p.Execute(context.Background(), ic); !r.Continue {
		t.Fatalf("execute should continue: %+v", r)
	}
	var plannerDecision *StageDecision
	for i := range ic.Stages {
		if ic.Stages[i].Stage == "planner" {
			plannerDecision = &ic.Stages[i]
		}
	}
	if plannerDecision == nil {
		t.Fatal("应记录 planner 阶段决策")
	}
	if !strings.Contains(plannerDecision.Reason, "src=chain") {
		t.Fatalf("阶段观测应带 src=chain, got %q", plannerDecision.Reason)
	}
}

func TestPlannerChain_ExecuteNotWired_NoSourceSuffix(t *testing.T) {
	p := NewDefaultTaskPlanner()
	ic := chainIC(IntentGreeting, "你好", "c-1")
	_ = p.Execute(context.Background(), ic)
	var plannerDecision *StageDecision
	for i := range ic.Stages {
		if ic.Stages[i].Stage == "planner" {
			plannerDecision = &ic.Stages[i]
		}
	}
	if plannerDecision == nil {
		t.Fatal("应记录 planner 阶段决策")
	}
	if strings.Contains(plannerDecision.Reason, "src=") {
		t.Fatalf("未装配绑定层时观测文本零变化, got %q", plannerDecision.Reason)
	}
}

// ---------- 模板渲染器独立契约 ----------

func TestRenderChainTemplate_VariablesAndLiteral(t *testing.T) {
	ic := chainIC(IntentOrderStatus, "运单号多少", "cust-7")
	cases := []struct {
		tpl  string
		want string
		ok   bool
	}{
		{"无变量纯文本", "无变量纯文本", true},
		{"{{customer_id}}", "cust-7", true},
		{"前缀-{{content}}-后缀", "前缀-运单号多少-后缀", true},
		{"{{ no_space_var }}", "", false}, // 白名单外
		{"{{unclosed", "", false},         // 未闭合
	}
	for _, tc := range cases {
		got, ok := renderChainTemplate(tc.tpl, ic)
		if ok != tc.ok {
			t.Fatalf("tpl=%q ok=%v want %v", tc.tpl, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("tpl=%q got=%q want=%q", tc.tpl, got, tc.want)
		}
	}
}
