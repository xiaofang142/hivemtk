package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestLeadReactDispatcherJudge_InvokesReActBeforeStructuredJudge(t *testing.T) {
	dispatcher, capture := newLeadReactTestDispatcher(t, false,
		`Action: query_historical_leads
Action Input: {"query":"u1"}`,
		"Final Answer: done",
		`{"is_lead":true,"intent_score":80,"lead_role":"demand","clue_type":"需求线索","summary":"采购意向","reason":"历史命中"}`,
	)
	resolver := LeadReactResolver{
		HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
			return "历史采购意向", nil
		},
		Customer360: func(context.Context, LeadReactTask, string) (string, error) {
			return "客户摘要", nil
		},
	}
	SetLeadReactHook(resolver, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })

	judge := &leadReactDispatcherJudge{dispatcher: dispatcher}
	jctx := &LeadJudgeContext{
		Messages: []DetectionMessage{{MessageID: "m1", Content: "这个能买吗", Role: "user", SenderID: "u1"}},
		Rule:     &model.LeadMiningRule{SystemDescription: "采购规则"},
	}
	jd, err := judge.JudgeWithContext(withLeadReactTask(leadReactTestContext(), leadReactTestTask()), jctx)
	if err != nil {
		t.Fatalf("二次判定失败: %v", err)
	}
	if jd.ReactOutcome == nil || jd.ReactOutcome.Degraded || len(jd.ReactOutcome.Evidence) != 1 {
		t.Fatalf("ReAct outcome 未进入结构化判定: %+v", jd)
	}
	if len(capture.requests) != 3 {
		t.Fatalf("应先两次 ReAct 再一次 structured judge，实际 %d 次", len(capture.requests))
	}
	last := renderLeadReactRequestText(capture.requests[2])
	if !strings.Contains(last, "历史采购意向") {
		t.Fatalf("结构化 judge 未消费 ReAct 证据: %s", last)
	}
}

func TestLeadReactDispatcherJudge_DegradedOutcomePersistsMachineState(t *testing.T) {
	dispatcher, _ := newLeadReactTestDispatcher(t, false,
		`Action: query_historical_leads
Action Input: {"query":"u1"}`,
		`{"is_lead":true,"intent_score":80,"lead_role":"demand","clue_type":"需求线索","summary":"候选","reason":"证据不足"}`,
	)
	SetLeadReactHook(LeadReactResolver{}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })

	judge := &leadReactDispatcherJudge{dispatcher: dispatcher}
	jd, err := judge.JudgeWithContext(withLeadReactTask(leadReactTestContext(), leadReactTestTask()), &LeadJudgeContext{
		Messages: []DetectionMessage{{Content: "这个能买吗", Role: "user"}},
	})
	if err != nil {
		t.Fatalf("二次判定失败: %v", err)
	}
	if !jd.Degraded || jd.ReactOutcome == nil || !jd.ReactOutcome.Degraded {
		t.Fatalf("ReAct 降级必须进入 LeadJudgement 机器状态: %+v", jd)
	}
	if strings.Contains(jd.ReactOutcome.Reason, "db down") {
		t.Fatal("typed outcome 不得泄漏内部错误")
	}
}

func TestLeadReactDispatcherJudge_DegradedOutcomePersisted(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	ctx := tenant.WithTenantID(context.Background(), 7301)
	jctx := &LeadJudgeContext{JudgementID: "lmj_react_degraded"}
	jd := &LeadJudgement{IsLead: true, IntentScore: 60, Degraded: true, ReactOutcome: &LeadReactOutcome{
		Status: leadReactStatusDegraded, Reason: leadReactReasonToolFailed, Degraded: true,
		Evidence: []string{"[工具失败] query_historical_leads"},
	}}
	hub := &model.MessageHub{MsgID: "m1", Platform: "telegram", AccountID: "acct-a", SenderID: "u1", ConversationID: "conv-a"}
	(&Service{}).recordLeadMiningJudgement(ctx, hub, jd, jctx)
	got, err := repository.NewLeadMiningJudgementRepositoryWithDB(database).GetByID(ctx, jctx.JudgementID)
	if err != nil {
		t.Fatalf("读取判定记录失败: %v", err)
	}
	if !got.Degraded || got.EvidenceComplete || !strings.Contains(got.ResultJSON, "react_outcome") {
		t.Fatalf("ReAct 降级未持久化机器状态: %+v", got)
	}
}

func TestLeadReactDispatcherJudge_StructuredFailurePreservesOutcomeInFallback(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	dispatcher, _ := newLeadReactTestDispatcher(t, false,
		`Action: query_historical_leads
Action Input: {"query":"u1"}`,
		"Final Answer: done",
		"structured judge unavailable",
	)
	SetLeadReactHook(LeadReactResolver{
		HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) {
			return "历史采购意向", nil
		},
		Customer360: func(context.Context, LeadReactTask, string) (string, error) {
			return "客户摘要", nil
		},
	}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })

	jctx := &LeadJudgeContext{
		JudgementID: "lmj_react_structured_failure",
		Messages:    []DetectionMessage{{Content: "这个能买吗", Role: "user", SenderID: "u1"}},
		Hits: []model.LeadMiningHit{{
			Type: "keyword", Complete: true,
			Payload: map[string]any{"match_count": 1, "matched": []map[string]any{{"keyword": "购买"}}},
		}},
	}
	_, err := (&leadReactDispatcherJudge{dispatcher: dispatcher}).JudgeWithContext(
		withLeadReactTask(leadReactTestContext(), leadReactTestTask()), jctx,
	)
	if err == nil {
		t.Fatal("structured judge 应返回错误")
	}
	fallback := buildDegradedJudgement(jctx, &model.LeadMiningConfig{MinIntentScore: 50}, err)
	if fallback == nil || !fallback.Degraded {
		t.Fatalf("本地命中应生成 degraded fallback: %+v", fallback)
	}
	if fallback.ReactOutcome == nil || fallback.ReactOutcome.Status != leadReactStatusOK || fallback.ReactOutcome.Degraded || len(fallback.ReactOutcome.Evidence) != 1 {
		t.Fatalf("structured judge 失败不得丢失成功 ReAct outcome: %+v", fallback.ReactOutcome)
	}
	hub := &model.MessageHub{TenantID: 7401, MsgID: "m1", Platform: "telegram", AccountID: "acct-1", SenderID: "u1", ConversationID: "conv-1"}
	(&Service{}).recordLeadMiningJudgement(leadReactTestContext(), hub, fallback, jctx)
	got, getErr := repository.NewLeadMiningJudgementRepositoryWithDB(database).GetByID(leadReactTestContext(), jctx.JudgementID)
	if getErr != nil {
		t.Fatalf("读取 fallback 判定记录失败: %v", getErr)
	}
	if !got.Degraded || got.EvidenceComplete || !strings.Contains(got.ResultJSON, "react_outcome") {
		t.Fatalf("fallback 未持久化 ReAct outcome/degraded/incomplete: %+v", got)
	}
}

func TestLeadReactDispatcherJudge_MissingFrozenTaskDegrades(t *testing.T) {
	dispatcher, capture := newLeadReactTestDispatcher(t, false,
		`{"is_lead":true,"intent_score":60,"lead_role":"demand","reason":"候选"}`,
	)
	SetLeadReactHook(LeadReactResolver{
		HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) { return "history", nil },
		Customer360:     func(context.Context, LeadReactTask, string) (string, error) { return "summary", nil },
	}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })

	jd, err := (&leadReactDispatcherJudge{dispatcher: dispatcher}).JudgeWithContext(leadReactTestContext(), &LeadJudgeContext{
		Messages: []DetectionMessage{{Content: "这个能买吗", Role: "user"}},
	})
	if err != nil {
		t.Fatalf("structured judge 失败: %v", err)
	}
	if !jd.Degraded || jd.ReactOutcome == nil || jd.ReactOutcome.Status != leadReactStatusUnavailable || jd.ReactOutcome.Reason != leadReactReasonInvalidScope {
		t.Fatalf("缺 frozen task 必须 visible degraded: %+v", jd)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("缺 frozen task 不得发起 ReAct 请求，实际 %d 次", len(capture.requests))
	}
}

func TestLeadReactDispatcherJudge_RawFallbackOutcomePersisted(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.LeadMiningJudgement{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	dispatcher, _ := newLeadReactTestDispatcher(t, false,
		"Final Answer: done",
		`{"is_lead":true,"intent_score":60,"lead_role":"demand","reason":"候选"}`,
	)
	SetLeadReactHook(LeadReactResolver{
		HistoricalLeads: func(context.Context, LeadReactTask, string) (string, error) { return "raw history", nil },
		Customer360:     func(context.Context, LeadReactTask, string) (string, error) { return "", errLeadReactToolUnavailable },
	}, newLeadReactDispatch(dispatcher))
	t.Cleanup(func() { SetLeadReactHook(LeadReactResolver{}, nil) })
	task := leadReactTestTask()
	task.CanonicalAccountID = 0
	task.ChannelID = 0
	task.AccountIDs = []string{"raw-ref"}
	task.SourceRef = "raw-ref"
	task.RawFallback = true
	jctx := &LeadJudgeContext{JudgementID: "lmj_react_raw", Messages: []DetectionMessage{{Content: "这个能买吗", Role: "user"}}}
	jd, err := (&leadReactDispatcherJudge{dispatcher: dispatcher}).JudgeWithContext(withLeadReactTask(leadReactTestContext(), task), jctx)
	if err != nil {
		t.Fatalf("structured judge 失败: %v", err)
	}
	hub := &model.MessageHub{TenantID: 7401, MsgID: "m1", Platform: "telegram", AccountID: "raw-ref", SenderID: "u1", ConversationID: "conv-1"}
	(&Service{}).recordLeadMiningJudgement(leadReactTestContext(), hub, jd, jctx)
	got, err := repository.NewLeadMiningJudgementRepositoryWithDB(database).GetByID(leadReactTestContext(), jctx.JudgementID)
	if err != nil {
		t.Fatalf("读取判定记录失败: %v", err)
	}
	if !got.Degraded || got.EvidenceComplete || !strings.Contains(got.ResultJSON, "raw_account_fallback") {
		t.Fatalf("raw fallback 未持久化 degraded/incomplete: %+v", got)
	}
}

func TestLeadReactDispatcherJudge_NoHookKeepsLegacyStructuredPath(t *testing.T) {
	SetLeadReactHook(LeadReactResolver{}, nil)
	dispatcher, capture := newLeadReactTestDispatcher(t, false,
		`{"is_lead":false,"intent_score":10,"lead_role":"unknown","reason":"无线索"}`,
	)
	judge := &leadReactDispatcherJudge{dispatcher: dispatcher}
	jd, err := judge.JudgeWithContext(context.Background(), &LeadJudgeContext{
		Messages: []DetectionMessage{{Content: "你好", Role: "user"}},
	})
	if err != nil || jd == nil || jd.IsLead || jd.ReactOutcome != nil {
		t.Fatalf("未装配 hook 应保持旧 structured 路径，jd=%+v,err=%v", jd, err)
	}
	if len(capture.requests) != 1 {
		t.Fatalf("未装配 hook 不应增加 ReAct 请求，实际 %d", len(capture.requests))
	}
}

var _ LLMJudge = (*leadReactDispatcherJudge)(nil)
var _ = llm.ScenarioLowCost
