package service

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type fakeContextFactProvider struct {
	facts *AgentContextFacts
	err   error
	calls int
}

func (f *fakeContextFactProvider) Load(context.Context, *IncomingContext, string, time.Time) (*AgentContextFacts, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.facts, nil
}

type fakeFactCustomerLookup struct {
	byOneID *model.Customer
	byID    *model.Customer
}

func (f fakeFactCustomerLookup) GetByOneID(context.Context, string) (*model.Customer, error) {
	return f.byOneID, nil
}

func (f fakeFactCustomerLookup) GetByID(context.Context, string) (*model.Customer, error) {
	return f.byID, nil
}

type fakeFactMemoryRepo struct {
	mem *model.DialogueMemory
	err error
}

func (f fakeFactMemoryRepo) GetDialogueMemoryBySession(context.Context, string) (*model.DialogueMemory, error) {
	return f.mem, f.err
}

type fakeFactConversationRepo struct {
	conv *model.ConversationContext
	err  error
}

func (f fakeFactConversationRepo) Get(context.Context, uint, string) (*model.ConversationContext, error) {
	return f.conv, f.err
}

type fakeFactLeadRepo struct {
	rows []model.LeadMiningJudgement
	err  error
}

func (f fakeFactLeadRepo) ListRecentForReAct(context.Context, string, []string, string, string, string, bool, string, int) ([]model.LeadMiningJudgement, error) {
	return f.rows, f.err
}

func TestBuildIncomingContextViewInjectsFactsAndVisibility(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	reader := &wiringContextReader{}
	builder := NewConversationContextBuilder(reader)
	in := &IncomingContext{
		Platform: model.Platform("telegram"), AccountID: "42", SenderID: "member-1",
		Content: "在群里问一下", MessageID: "hub:21", IsGroup: true, GroupID: "group-1",
		ConversationID: "thread-9", OriginalSentAt: time.Unix(200, 0),
		Visibility: agentcontext.VisibilityMember, Audience: []string{"group-1"},
	}
	facts := &AgentContextFacts{
		Customer: &agentcontext.CustomerContext{
			SubjectType: "group_member", SubjectID: "member-1", DisplayName: "张三",
			Facts: []agentcontext.BusinessFact{{Key: "name", Value: "张三", Grade: agentcontext.FactConfirmed, SubjectType: "group_member", SubjectID: "member-1"}},
		},
		Lead: &agentcontext.LeadContext{
			SubjectType: "group_member", SubjectID: "member-1", Status: "demand",
			Requirement: "想买报价方案",
			Facts:       []agentcontext.BusinessFact{{Key: "lead_summary", Value: "需求线索", Grade: agentcontext.FactDerivedSummary, SubjectType: "group_member", SubjectID: "member-1"}},
		},
		SharedBusiness: []agentcontext.BusinessFact{{Key: "shared_note", Value: "上次确认过交期", Grade: agentcontext.FactInferred}},
		OlderSummary:   &agentcontext.MemorySummary{Summary: "历史摘要", Grade: agentcontext.FactDerivedSummary, SourceCursor: "hub:20"},
		Participants:   []agentcontext.Participant{{SenderID: "member-1", DisplayName: "张三", IsCurrent: true}, {SenderID: "member-2", DisplayName: "李四", MemberRole: "supplier"}},
	}
	provider := &fakeContextFactProvider{facts: facts}

	view, err := buildIncomingContextView(ctx, in, "session-2", builder, time.Unix(300, 0), provider)
	if err != nil {
		t.Fatalf("构建带事实的上下文失败: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("事实 provider 应只调用一次, got %d", provider.calls)
	}
	if view.Customer == nil || view.Customer.DisplayName != "张三" {
		t.Fatalf("客户画像未注入: %+v", view.Customer)
	}
	if view.Lead == nil || view.Lead.Status != "demand" || view.Lead.Requirement != "想买报价方案" {
		t.Fatalf("线索信息未注入: %+v", view.Lead)
	}
	if len(view.SharedBusiness) != 1 || view.SharedBusiness[0].Grade != agentcontext.FactInferred {
		t.Fatalf("共享事实未注入或分级错误: %+v", view.SharedBusiness)
	}
	if view.OlderSummary == nil || view.OlderSummary.Summary != "历史摘要" {
		t.Fatalf("摘要未注入: %+v", view.OlderSummary)
	}
	if len(view.Participants) != 2 || !view.Participants[0].IsCurrent {
		t.Fatalf("成员信息未注入: %+v", view.Participants)
	}
	if view.Current.Visibility != agentcontext.VisibilityMember || len(view.Current.Audience) != 1 || view.Current.Audience[0] != "group-1" {
		t.Fatalf("当前消息可见性未采用权威值: %+v", view.Current)
	}
	if view.Provenance.VisibilityProof != "group_audienceVerified" {
		t.Fatalf("群可见性证明错误: %q", view.Provenance.VisibilityProof)
	}
}

func TestBuildIncomingContextViewProviderErrorFailsClosed(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	in := &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: "42", SenderID: "u1",
		Content: "你好", MessageID: "hub:1", ConversationID: "conv-1",
	}
	provider := &fakeContextFactProvider{err: errors.New("fact store down")}
	view, err := buildIncomingContextView(ctx, in, "s1", NewConversationContextBuilder(&wiringContextReader{}), time.Unix(300, 0), provider)
	if err == nil || view != nil {
		t.Fatalf("事实读取失败必须 fail-closed: view=%v err=%v", view, err)
	}
}

func TestAgentContextFactsProviderLoadsAuthoritativeFacts(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	updated := time.Date(2026, 11, 27, 9, 30, 0, 0, time.UTC)
	provider := NewAgentContextFactsProvider(
		fakeFactCustomerLookup{
			byOneID: &model.Customer{TenantID: 7, ID: "c-1", UnifiedID: "u1", Name: "张三", Tags: "vip,老客户", ChurnRisk: "low"},
		},
		fakeFactMemoryRepo{mem: &model.DialogueMemory{
			TenantID: 7, ID: 9, SessionID: "s1", Summary: "历史摘要", LastActiveAt: updated,
		}},
		fakeFactConversationRepo{conv: &model.ConversationContext{
			TenantID: 7, ChannelAccountID: 42, MemberRoles: model.MemberRoles{"member-1": "demand", "member-2": "supplier"},
			Summary: "群聊长期摘要", Tags: []string{
				"public-tag",
				EncodeBusinessNote(BusinessNote{Source: ContextNoteNamespaceLeadMining, At: 1700000000, Attribute: BusinessNoteFact, Text: "判定为需求线索"}),
				"note:legacy_namespace:待核实的旧备注",
			},
		}},
		fakeFactLeadRepo{rows: []model.LeadMiningJudgement{{
			TenantID: 7, ID: 3, IsGroup: true, GroupID: "group-1", SenderID: "member-1",
			Matched: true, LeadRole: "demand", Summary: "想买报价方案", Reason: "询问报价",
			ContextMessageIDs: model.JSONStrings{"hub:11", "hub:12"}, CreatedAt: updated,
		}}},
	)

	in := &IncomingContext{
		Platform: model.Platform("telegram"), AccountID: "42", SenderID: "member-1",
		OneID: "u1", IsGroup: true, GroupID: "group-1", ConversationID: "thread-9",
		MessageID: "hub:21", Content: "这个方案多少钱",
	}
	facts, err := provider.Load(ctx, in, "s1", updated)
	if err != nil {
		t.Fatalf("加载权威事实失败: %v", err)
	}
	if facts.Customer == nil || facts.Customer.DisplayName != "张三" || facts.Customer.SubjectType != "group_member" || facts.Customer.SubjectID != "member-1" {
		t.Fatalf("客户画像加载错误: %+v", facts.Customer)
	}
	if len(facts.Customer.Facts) == 0 || facts.Customer.Facts[0].Grade != agentcontext.FactConfirmed {
		t.Fatalf("客户确认事实缺失: %+v", facts.Customer.Facts)
	}
	if facts.Lead == nil || facts.Lead.Status != "demand" || facts.Lead.Requirement != "想买报价方案" || len(facts.Lead.EvidenceMessageIDs) != 2 {
		t.Fatalf("线索事实加载错误: %+v", facts.Lead)
	}
	if facts.Lead.Facts[0].Grade != agentcontext.FactDerivedSummary || facts.Lead.Facts[0].SubjectType != "group_member" || facts.Lead.Facts[0].SubjectID != "member-1" {
		t.Fatalf("线索事实分级或主体错误: %+v", facts.Lead.Facts)
	}
	if facts.OlderSummary == nil || facts.OlderSummary.Summary != "历史摘要" || facts.OlderSummary.Grade != agentcontext.FactDerivedSummary {
		t.Fatalf("长期摘要加载错误: %+v", facts.OlderSummary)
	}
	if len(facts.SharedBusiness) == 0 {
		t.Fatal("共享业务事实为空")
	}
	var foundLegacy bool
	for _, fact := range facts.SharedBusiness {
		if fact.Grade == agentcontext.FactLegacyUnverified {
			foundLegacy = true
		}
		if fact.Grade == agentcontext.FactConfirmed && fact.Source == ContextNoteNamespaceLeadMining {
			t.Fatal("其他智能体的 fact 备注不得直接升级为 confirmed")
		}
	}
	if !foundLegacy {
		t.Fatalf("旧格式/非确认备注必须降级: %+v", facts.SharedBusiness)
	}
	if len(facts.Participants) != 2 || facts.Participants[0].SenderID != "member-1" || !facts.Participants[0].IsCurrent {
		t.Fatalf("群成员加载错误: %+v", facts.Participants)
	}
}

func TestAgentContextFactsProviderMissingScopeFailsClosed(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	provider := NewAgentContextFactsProvider(nil, nil, nil, nil)
	if _, err := provider.Load(ctx, &IncomingContext{AccountID: "not-a-number", MessageID: "m"}, "s1", time.Now()); err == nil {
		t.Fatal("账号 ID 无效必须 fail-closed")
	}
}
