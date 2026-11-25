package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type leadReactHTTPCapture struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (c *leadReactHTTPCapture) handler(t *testing.T, contents []string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("解码 LLM 请求失败: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.requests = append(c.requests, body)
		round := len(c.requests)
		c.mu.Unlock()
		content := "Final Answer: done"
		if round <= len(contents) {
			content = contents[round-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(content) + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func newLeadReactTestDispatcher(t *testing.T, noFC bool, contents ...string) (*llm.Dispatcher, *leadReactHTTPCapture) {
	t.Helper()
	capture := &leadReactHTTPCapture{}
	server := httptest.NewServer(capture.handler(t, contents))
	t.Cleanup(server.Close)
	dispatcher := llm.NewDispatcher(llm.NewLLMService())
	dispatcher.AddProvider(llm.ProviderConfig{
		Name: "lead-react-fake", BaseURL: server.URL, Model: "fake-model",
		Enabled: true, NoFC: noFC, QualityScore: 1,
	})
	dispatcher.SetRoute(llm.ScenarioRoute{Scenario: llm.ScenarioLowCost, Provider: "lead-react-fake"})
	return dispatcher, capture
}

func leadReactTestContext() context.Context {
	return tenant.WithTenantID(context.Background(), 7401)
}

func leadReactTestScope() *leadHistoryScope {
	return &leadHistoryScope{
		MessageHubHistoryScope: repository.MessageHubHistoryScope{
			Platform: "telegram", AccountIDs: []string{"acct-1", "11"}, IsGroup: false,
		},
		tenantID: 7401, accountID: 11, channelID: 3, accountKey: "id:11", sourceRef: "acct-1",
	}
}

func leadReactTestTask() LeadReactTask {
	return LeadReactTask{
		Platform: "telegram", CanonicalAccountID: 11, ChannelID: 3,
		AccountIDs: []string{"11", "acct-1"}, SourceRef: "acct-1", ScopeValid: true,
		ConversationID: "conv-1", SenderID: "u1", CurrentMessage: "这个能买吗",
		Messages: []LeadReactTaskMessage{
			{MessageID: "m0", Role: "user", SenderID: "u1", Content: "你好"},
			{MessageID: "m1", Role: "user", SenderID: "u1", Content: "这个能买吗"},
		},
		RuleDescription: "只判断采购意向", Keywords: []string{"购买"},
		Hits: []LeadReactTaskHit{{Type: "keyword", Complete: true, Reason: "购买"}},
	}
}

// TestLeadReactTextOnlyProductionDispatcher_NoFCAndNative 验证真实 Dispatcher 在两种
// provider 配置下都走服务自有文本协议：请求不得带 Tools，Action 不得被适配器吞掉。
func TestLeadReactTextOnlyProductionDispatcher_NoFCAndNative(t *testing.T) {
	for _, noFC := range []bool{true, false} {
		t.Run(map[bool]string{true: "NoFC", false: "NativeFC"}[noFC], func(t *testing.T) {
			dispatcher, capture := newLeadReactTestDispatcher(t, noFC,
				"Thought: 需要历史\nAction: query_historical_leads\nAction Input: {\"query\":\"u1\"}",
				"Final Answer: 证据充分",
			)
			var gotQuery string
			resolver := LeadReactResolver{HistoricalLeads: func(_ context.Context, _ LeadReactTask, query string) (string, error) {
				gotQuery = query
				return "历史命中", nil
			}}
			SetLeadReactHook(resolver, newLeadReactDispatch(dispatcher))
			t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })

			outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), defaultLeadReactTools(resolver), newLeadReactDispatch(dispatcher))
			if outcome.Degraded || outcome.Status != leadReactStatusOK || len(outcome.Evidence) != 1 || gotQuery != "u1" {
				t.Fatalf("文本 ReAct 应成功取证，outcome=%+v query=%q", outcome, gotQuery)
			}
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.requests) != 2 {
				t.Fatalf("期望 Action + Final Answer 两次请求，实际 %d 次", len(capture.requests))
			}
			for i, req := range capture.requests {
				if _, ok := req["tools"]; ok {
					t.Fatalf("第 %d 次请求不得携带原生 Tools: %+v", i+1, req)
				}
			}
			firstPrompt := renderLeadReactRequestText(capture.requests[0])
			for _, want := range []string{"这个能买吗", "你好", "只判断采购意向", "购买", "acct-1", "telegram", "conv-1", "u1", "query_historical_leads", "keyword"} {
				if !strings.Contains(firstPrompt, want) {
					t.Fatalf("首轮 prompt 缺少 %q: %s", want, firstPrompt)
				}
			}
		})
	}
}

func renderLeadReactRequestText(req map[string]any) string {
	raw, _ := json.Marshal(req)
	return string(raw)
}

func TestLeadReactStrictArgs_RejectsMalformedAndScopeOverride(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "malformed", input: `not-json`},
		{name: "scope_override", input: `{"query":"u1","tenant_id":99,"account_id":"other"}`},
		{name: "empty_query", input: `{"query":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			tools := defaultLeadReactTools(LeadReactResolver{HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
				called = true
				return "should-not-run", nil
			}})
			dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
				return &llm.DispatchResult{Content: "Action: query_historical_leads\nAction Input: " + tc.input}, nil
			}
			outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), tools, dispatch)
			if !outcome.Degraded || called || outcome.Reason != leadReactReasonInvalidArgs {
				t.Fatalf("非法参数必须降级且不得调用 resolver，outcome=%+v called=%v", outcome, called)
			}
		})
	}
}

func TestLeadReactWhitelist_DictionaryUnsupported(t *testing.T) {
	tools := defaultLeadReactTools(LeadReactResolver{})
	if len(tools) != 2 {
		t.Fatalf("文本 ReAct 白名单应仅含 customer360/historical_leads，实际 %d 个", len(tools))
	}
	for _, tool := range tools {
		if tool.Name == reactToolDictionary {
			t.Fatal("query_dictionary 必须作为显式不支持工具移出白名单")
		}
	}
}

func TestLeadReactLoop_RoundLimitVisible(t *testing.T) {
	tools := defaultLeadReactTools(LeadReactResolver{HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
		return "observation", nil
	}})
	rounds := 0
	dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
		rounds++
		return &llm.DispatchResult{Content: `Action: query_historical_leads
Action Input: {"query":"u1"}`}, nil
	}
	outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), tools, dispatch)
	if !outcome.Degraded || outcome.Reason != leadReactReasonRoundLimit || rounds != leadReactMaxRounds || len(outcome.Evidence) != leadReactMaxRounds {
		t.Fatalf("超过三轮必须带证据降级，rounds=%d outcome=%+v", rounds, outcome)
	}
}

func TestLeadReactLoop_TimeoutVisible(t *testing.T) {
	ctx, cancel := context.WithCancel(leadReactTestContext())
	cancel()
	dispatch := func(ctx context.Context, _ llm.DispatchRequest) (*llm.DispatchResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	outcome := runLeadReactLoop(ctx, leadReactTestTask(), defaultLeadReactTools(LeadReactResolver{}), dispatch)
	if !outcome.Degraded || outcome.Reason != leadReactReasonTimeout {
		t.Fatalf("超时必须可见降级，outcome=%+v", outcome)
	}
}

func TestLeadReactLoop_EmptyResolverVisible(t *testing.T) {
	dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
		return &llm.DispatchResult{Content: `Action: query_historical_leads
Action Input: {"query":"u1"}`}, nil
	}
	outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), defaultLeadReactTools(LeadReactResolver{}), dispatch)
	if !outcome.Degraded || outcome.Status != leadReactStatusUnavailable || len(outcome.Evidence) == 0 {
		t.Fatalf("空 resolver 必须 unavailable/degraded，outcome=%+v", outcome)
	}
}

func TestInstallLeadReactDispatcher_EmptyResolverVisible(t *testing.T) {
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })
	if err := InstallLeadReactDispatcher(LeadReactResolver{}); err == nil {
		t.Fatal("空 resolver 生产装配必须返回明确错误")
	}
	capture := &LeadReactOutcome{}
	ctx := withLeadReactOutcomeCapture(withLeadReactTask(leadReactTestContext(), leadReactTestTask()), capture)
	evidence, degraded := getLeadReactHook()(ctx, &LeadJudgeContext{})
	if !degraded || len(evidence) != 0 || capture.Status != leadReactStatusUnavailable || capture.Reason != leadReactReasonToolUnavailable {
		t.Fatalf("生产空 resolver 必须 unavailable/degraded，capture=%+v evidence=%+v degraded=%v", capture, evidence, degraded)
	}
}

func TestLeadReactLoop_DispatchErrorDoesNotLeak(t *testing.T) {
	dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
		return nil, errors.New("provider secret detail")
	}
	outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), defaultLeadReactTools(LeadReactResolver{}), dispatch)
	if !outcome.Degraded || outcome.Reason != leadReactReasonDispatchFailed || strings.Contains(outcome.Reason, "secret") {
		t.Fatalf("dispatch 错误必须固定降级且不泄漏内部详情: %+v", outcome)
	}
}

func TestLeadReactHook_EmptyResolverFinalAnswerStillUnavailable(t *testing.T) {
	dispatcher, _ := newLeadReactTestDispatcher(t, false, "Final Answer: 无需取证")
	SetLeadReactHook(LeadReactResolver{}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })
	capture := &LeadReactOutcome{}
	ctx := withLeadReactOutcomeCapture(withLeadReactTask(leadReactTestContext(), leadReactTestTask()), capture)
	evidence, degraded := getLeadReactHook()(ctx, &LeadJudgeContext{
		Messages: []DetectionMessage{{Content: "你好", Role: "user", SenderID: "u1"}},
	})
	if !degraded || len(evidence) != 0 || capture.Status != leadReactStatusUnavailable || capture.Reason != leadReactReasonToolUnavailable {
		t.Fatalf("空 resolver 即使直接 Final Answer 也必须 unavailable，capture=%+v evidence=%+v degraded=%v", capture, evidence, degraded)
	}
}

func TestLeadReactLoop_UnknownToolAndInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name, content, reason string
	}{
		{name: "unknown", content: "Action: delete_all\nAction Input: {\"query\":\"u1\"}", reason: leadReactReasonUnknownTool},
		{name: "plain_text", content: "我认为这是线索", reason: leadReactReasonInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
				return &llm.DispatchResult{Content: tc.content}, nil
			}
			outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), defaultLeadReactTools(LeadReactResolver{}), dispatch)
			if !outcome.Degraded || outcome.Reason != tc.reason {
				t.Fatalf("应 visible degraded，outcome=%+v", outcome)
			}
		})
	}
}

func TestBuildLeadReactTask_BoundedAndDeterministic(t *testing.T) {
	jctx := &LeadJudgeContext{
		AccountID: "acct-1", Platform: "telegram", ChannelAccountID: 11,
		Rule: &model.LeadMiningRule{SystemDescription: strings.Repeat("规", 5000), Keywords: []string{"购买"}},
	}
	for i := 0; i < 30; i++ {
		jctx.Messages = append(jctx.Messages, DetectionMessage{
			MessageID: string(rune('a' + i)), Content: strings.Repeat("x", 2000), Role: "user", SenderID: "u1",
		})
	}
	hub := &model.MessageHub{TenantID: 7401, Platform: "telegram", AccountID: "acct-1", ConversationID: "conv-1", SenderID: "u1", Content: strings.Repeat("当前", 2000)}
	task := buildLeadReactTask(leadReactTestContext(), hub, jctx, leadReactTestScope())
	if !task.ScopeValid || task.CanonicalAccountID != 11 || task.SourceRef != "acct-1" {
		t.Fatalf("frozen scope 未进入 task: %+v", task)
	}
	if len(task.Messages) != leadReactTaskMaxMessages {
		t.Fatalf("历史消息必须确定性截到 %d 条，实际 %d", leadReactTaskMaxMessages, len(task.Messages))
	}
	if len([]rune(task.Messages[0].Content)) > leadReactTaskMaxMessageRunes || len([]rune(task.CurrentMessage)) > leadReactTaskMaxCurrentRunes {
		t.Fatal("单条消息必须按 rune 截断")
	}
	if len([]rune(task.RuleDescription)) > leadReactTaskMaxRuleRunes {
		t.Fatal("规则描述必须按 rune 截断")
	}
	if task.Messages[len(task.Messages)-1].MessageID != string(rune('a'+29)) {
		t.Fatalf("必须保留最近消息，实际尾部 %q", task.Messages[len(task.Messages)-1].MessageID)
	}
}

func TestLeadReactLoop_ResolverErrorVisible(t *testing.T) {
	tools := defaultLeadReactTools(LeadReactResolver{HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
		return "", errors.New("db down")
	}})
	dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
		return &llm.DispatchResult{Content: `Action: query_historical_leads
Action Input: {"query":"u1"}`}, nil
	}
	outcome := runLeadReactLoop(leadReactTestContext(), leadReactTestTask(), tools, dispatch)
	if !outcome.Degraded || outcome.Reason != leadReactReasonToolFailed || len(outcome.Evidence) != 1 {
		t.Fatalf("resolver 错误必须 visible degraded，outcome=%+v", outcome)
	}
	if strings.Contains(outcome.Evidence[0], "db down") {
		t.Fatal("证据中不得泄漏内部错误原文")
	}
}

func TestLeadReactAdaptation_CanonicalAliasesReplaceExactLookup(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{}, &model.CustomerSession{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	rows := []*model.LeadMiningJudgement{
		{TenantID: 7401, JudgementID: "canonical", Platform: "telegram", AccountID: "11", ConversationID: "conv-1", SenderID: "u1", Summary: "规范账号历史"},
		{TenantID: 7401, JudgementID: "alias", Platform: "telegram", AccountID: "acct-1", ConversationID: "conv-1", SenderID: "u1", Summary: "别名账号历史"},
		{TenantID: 7401, JudgementID: "other", Platform: "telegram", AccountID: "acct-2", ConversationID: "conv-1", SenderID: "u1", Summary: "其他账号历史"},
	}
	if err := database.WithContext(tenant.WithSystemScope(leadReactTestContext())).Create(&rows).Error; err != nil {
		t.Fatalf("准备判定夹具失败: %v", err)
	}
	sessions := []*model.CustomerSession{
		{TenantID: 7401, SessionID: "canonical-session", Platform: model.ChannelTelegram, AccountID: "11", UserID: "u1", Status: model.SessionStatusResolved},
		{TenantID: 7401, SessionID: "alias-session", Platform: model.ChannelTelegram, AccountID: "acct-1", UserID: "u1", Status: model.SessionStatusClosed},
	}
	if err := database.WithContext(tenant.WithSystemScope(leadReactTestContext())).Create(&sessions).Error; err != nil {
		t.Fatalf("准备会话夹具失败: %v", err)
	}
	resolver, err := NewLeadReactResolver(repository.NewLeadMiningJudgementRepositoryWithDB(database), repository.NewCustomerSessionRepositoryWithDB(database))
	if err != nil {
		t.Fatalf("构造 resolver 失败: %v", err)
	}
	task := leadReactTestTask()
	leadJSON, err := resolver.HistoricalLeads(leadReactTestContext(), task, "")
	if err != nil || !strings.Contains(leadJSON, "规范账号历史") || !strings.Contains(leadJSON, "别名账号历史") || strings.Contains(leadJSON, "其他账号历史") {
		t.Fatalf("canonical+verified aliases 应合并同账号历史，实际 %s,err=%v", leadJSON, err)
	}
	customerJSON, err := resolver.Customer360(leadReactTestContext(), task, "u1")
	if err != nil {
		t.Fatalf("canonical Customer360 应可用: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(customerJSON), &summary); err != nil || summary["session_count"] != float64(2) {
		t.Fatalf("canonical Customer360 应合并别名会话，实际 %s,err=%v", customerJSON, err)
	}
}

func TestLeadReactAdaptation_RawFallbackCustomer360Unavailable(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.CustomerSession{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	if err := database.WithContext(tenant.WithSystemScope(leadReactTestContext())).Create(&model.CustomerSession{
		TenantID: 7401, SessionID: "raw-session", Platform: model.ChannelTelegram, AccountID: "raw-ref", UserID: "u1",
	}).Error; err != nil {
		t.Fatalf("准备会话夹具失败: %v", err)
	}
	resolver, err := NewLeadReactResolver(repository.NewLeadMiningJudgementRepositoryWithDB(database), repository.NewCustomerSessionRepositoryWithDB(database))
	if err != nil {
		t.Fatalf("构造 resolver 失败: %v", err)
	}
	task := leadReactTestTask()
	task.CanonicalAccountID = 0
	task.ChannelID = 0
	task.AccountIDs = []string{"raw-ref"}
	task.SourceRef = "raw-ref"
	task.RawFallback = true
	if _, err := resolver.Customer360(leadReactTestContext(), task, "u1"); !errors.Is(err, errLeadReactToolUnavailable) {
		t.Fatalf("raw fallback Customer360 必须 unavailable 且不得查询，实际 %v", err)
	}
}

func TestLeadReactAdaptation_MissingFrozenTaskDoesNotRebuildFromJctx(t *testing.T) {
	dispatcher, capture := newLeadReactTestDispatcher(t, false,
		`Action: query_historical_leads
Action Input: {"query":"u1"}`,
		"Final Answer: done",
	)
	SetLeadReactHook(LeadReactResolver{
		HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
			return "should-not-run", nil
		},
		Customer360: func(context.Context, LeadReactTask, string) (string, error) {
			return "should-not-run", nil
		},
	}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })
	captureOutcome := &LeadReactOutcome{}
	evidence, degraded := getLeadReactHook()(withLeadReactOutcomeCapture(leadReactTestContext(), captureOutcome), &LeadJudgeContext{
		AccountID: "acct-1", Platform: "telegram",
		Messages: []DetectionMessage{{Content: "这个能买吗", Role: "user", SenderID: "u1"}},
	})
	if !degraded || len(evidence) != 0 || captureOutcome.Status != leadReactStatusUnavailable || captureOutcome.Reason != leadReactReasonInvalidScope || len(capture.requests) != 0 {
		t.Fatalf("缺 frozen task 必须 unavailable 且不得重建/请求 LLM，outcome=%+v evidence=%+v requests=%d", captureOutcome, evidence, len(capture.requests))
	}
}

func TestLeadReactAdaptation_MissingOrSystemTenantFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing", ctx: context.Background()},
		{name: "system", ctx: tenant.WithSystemScope(leadReactTestContext())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
				return &llm.DispatchResult{Content: "Final Answer: done"}, nil
			}
			outcome := runLeadReactLoop(tc.ctx, leadReactTestTask(), defaultLeadReactTools(LeadReactResolver{
				HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) { return "history", nil },
				Customer360:     func(context.Context, LeadReactTask, string) (string, error) { return "summary", nil },
			}), dispatch)
			if !outcome.Degraded || outcome.Status != leadReactStatusUnavailable || outcome.Reason != "invalid_scope" {
				t.Fatalf("缺租户/SystemScope 必须 unavailable，实际 %+v", outcome)
			}
		})
	}
}

func TestLeadReactAdaptation_RawFallbackHistoricalExactSourceRefOnly(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	rows := []*model.LeadMiningJudgement{
		{TenantID: 7401, JudgementID: "raw", Platform: "telegram", AccountID: "raw-ref", ConversationID: "conv-1", SenderID: "u1", Summary: "raw历史"},
		{TenantID: 7401, JudgementID: "alias", Platform: "telegram", AccountID: "alias-ref", ConversationID: "conv-1", SenderID: "u1", Summary: "不得扩展别名"},
		{TenantID: 7401, JudgementID: "other-conv", Platform: "telegram", AccountID: "raw-ref", ConversationID: "other-conv", SenderID: "u1", Summary: "不得跨会话"},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&rows).Error; err != nil {
		t.Fatalf("准备判定夹具失败: %v", err)
	}
	resolver, err := NewLeadReactResolver(repository.NewLeadMiningJudgementRepositoryWithDB(database), repository.NewCustomerSessionRepositoryWithDB(database))
	if err != nil {
		t.Fatalf("构造 resolver 失败: %v", err)
	}
	task := leadReactTestTask()
	task.CanonicalAccountID = 0
	task.ChannelID = 0
	task.AccountIDs = []string{"raw-ref"}
	task.SourceRef = "raw-ref"
	task.RawFallback = true
	raw, err := resolver.HistoricalLeads(leadReactTestContext(), task, "")
	if err != nil || !strings.Contains(raw, "raw历史") || strings.Contains(raw, "不得扩展别名") || strings.Contains(raw, "不得跨会话") {
		t.Fatalf("raw fallback 历史必须仅 exact sourceRef+范围，实际 %s,err=%v", raw, err)
	}
}

func TestLeadReactLoop_RawFallbackFinalAnswerStillDegraded(t *testing.T) {
	task := leadReactTestTask()
	task.CanonicalAccountID = 0
	task.ChannelID = 0
	task.AccountIDs = []string{"raw-ref"}
	task.SourceRef = "raw-ref"
	task.RawFallback = true
	dispatch := func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
		return &llm.DispatchResult{Content: "Final Answer: done"}, nil
	}
	outcome := runLeadReactLoop(leadReactTestContext(), task, defaultLeadReactTools(LeadReactResolver{}), dispatch)
	if !outcome.Degraded || outcome.Status != leadReactStatusDegraded || outcome.Reason != leadReactReasonRawAccountFallback {
		t.Fatalf("raw fallback 即使 Final Answer 也必须 degraded，实际 %+v", outcome)
	}
}

func TestBuildLeadReactTask_RawFallbackUsesExactSourceRef(t *testing.T) {
	scope := &leadHistoryScope{
		MessageHubHistoryScope: repository.MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"raw-ref"}},
		tenantID:               7401, accountKey: "raw:raw-ref", sourceRef: "raw-ref",
	}
	hub := &model.MessageHub{TenantID: 7401, Platform: "telegram", AccountID: "raw-ref", ConversationID: "conv-1", SenderID: "u1", Content: "这个能买吗"}
	task := buildLeadReactTask(leadReactTestContext(), hub, &LeadJudgeContext{Platform: "telegram", AccountID: "raw-ref"}, scope)
	if !task.ScopeValid || !task.RawFallback || task.CanonicalAccountID != 0 || task.ChannelID != 0 || len(task.AccountIDs) != 1 || task.AccountIDs[0] != "raw-ref" {
		t.Fatalf("raw fallback task 必须仅保留 exact sourceRef: %+v", task)
	}
}

func TestBuildLeadReactTask_RejectsInvalidFrozenScope(t *testing.T) {
	hub := &model.MessageHub{TenantID: 7401, Platform: "telegram", AccountID: "acct-1", ConversationID: "conv-1", SenderID: "u1", Content: "这个能买吗"}
	jctx := &LeadJudgeContext{Platform: "telegram", AccountID: "acct-1", ChannelAccountID: 11}
	wrongTenant := leadReactTestScope()
	wrongTenant.tenantID = 7402
	wrongPlatform := leadReactTestScope()
	wrongPlatform.Platform = "qq"
	wrongChannel := leadReactTestScope()
	wrongChannel.channelID = 0
	rawAliasExpansion := &leadHistoryScope{
		MessageHubHistoryScope: repository.MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"acct-1", "alias"}},
		tenantID:               7401, accountKey: "raw:acct-1", sourceRef: "acct-1",
	}
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		scope *leadHistoryScope
	}{
		{name: "missing_scope", ctx: leadReactTestContext()},
		{name: "tenant_mismatch", ctx: leadReactTestContext(), scope: wrongTenant},
		{name: "system_scope", ctx: tenant.WithSystemScope(leadReactTestContext()), scope: leadReactTestScope()},
		{name: "platform_mismatch", ctx: leadReactTestContext(), scope: wrongPlatform},
		{name: "channel_mismatch", ctx: leadReactTestContext(), scope: wrongChannel},
		{name: "raw_alias_expansion", ctx: leadReactTestContext(), scope: rawAliasExpansion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := buildLeadReactTask(tc.ctx, hub, jctx, tc.scope)
			if task.ScopeValid || len(task.AccountIDs) != 0 {
				t.Fatalf("非法 frozen scope 不得形成 ACL: %+v", task)
			}
		})
	}
}

func TestLeadReactTask_TimeoutConstantBounded(t *testing.T) {
	if leadReactTimeout <= 0 || leadReactTimeout > 30*time.Second {
		t.Fatalf("ReAct 超时必须有界，实际 %v", leadReactTimeout)
	}
}
