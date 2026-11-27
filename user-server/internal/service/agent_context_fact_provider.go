package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// AgentContextFacts 是从权威业务源读取、经分级后注入统一 ContextView 的事实。
// 这里只放最小必要信息：画像/线索/共享备注/长期摘要/群成员；任何未知来源或
// 未确认建议都降级，不允许在模型输入中冒充既成事实。
type AgentContextFacts struct {
	Customer       *agentcontext.CustomerContext
	Lead           *agentcontext.LeadContext
	SharedBusiness []agentcontext.BusinessFact
	OlderSummary   *agentcontext.MemorySummary
	Participants   []agentcontext.Participant
}

// ContextFactProvider 在编排器入口装配；返回 error 时本轮不得构造 ContextView，
// 防止缺事实的上下文被当作已验证视图放行。
type ContextFactProvider interface {
	Load(ctx context.Context, in *IncomingContext, sessionID string, now time.Time) (*AgentContextFacts, error)
}

type contextFactCustomerLookup interface {
	GetByOneID(ctx context.Context, oneID string) (*model.Customer, error)
	GetByID(ctx context.Context, id string) (*model.Customer, error)
}

type contextFactMemoryRepo interface {
	GetDialogueMemoryBySession(ctx context.Context, sessionID string) (*model.DialogueMemory, error)
}

type contextFactConversationRepo interface {
	Get(ctx context.Context, channelAccountID uint, externalConversationID string) (*model.ConversationContext, error)
}

type contextFactLeadRepo interface {
	ListRecentForReAct(ctx context.Context, platform string, accountIDs []string, conversationID, groupID, senderID string, isGroup bool, query string, limit int) ([]model.LeadMiningJudgement, error)
}

type defaultAgentContextFactsProvider struct {
	customerLookup contextFactCustomerLookup
	memoryRepo     contextFactMemoryRepo
	conversation   contextFactConversationRepo
	leads          contextFactLeadRepo
}

// NewAgentContextFactsProvider 用窄接口构造生产事实 provider；仓库实现由装配层注入。
func NewAgentContextFactsProvider(
	customerLookup contextFactCustomerLookup,
	memoryRepo contextFactMemoryRepo,
	conversation contextFactConversationRepo,
	leads contextFactLeadRepo,
) ContextFactProvider {
	return &defaultAgentContextFactsProvider{
		customerLookup: customerLookup,
		memoryRepo:     memoryRepo,
		conversation:   conversation,
		leads:          leads,
	}
}

func (p *defaultAgentContextFactsProvider) Load(ctx context.Context, in *IncomingContext, sessionID string, now time.Time) (*AgentContextFacts, error) {
	if p == nil {
		return nil, nil
	}
	if in == nil {
		return nil, nil
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
		return nil, nil
	}
	accountID, err := strconv.ParseUint(strings.TrimSpace(in.AccountID), 10, 64)
	if err != nil || accountID == 0 {
		return nil, errors.New("统一上下文事实读取缺少有效渠道账号")
	}
	facts := &AgentContextFacts{
		SharedBusiness: []agentcontext.BusinessFact{},
	}
	platform := strings.TrimSpace(string(in.Platform))
	subjectType, subjectID := contextFactSubject(in)

	facts.Customer = p.loadCustomer(ctx, in, platform, tid, uint(accountID), subjectType, subjectID)
	facts.Lead = p.loadLead(ctx, in, platform, tid, uint(accountID), subjectType, subjectID)
	facts.OlderSummary = p.loadSummary(ctx, sessionID)
	facts.SharedBusiness, facts.Participants = p.loadConversationFacts(ctx, in, platform, tid, uint(accountID), subjectType, subjectID, now)
	return facts, nil
}

func contextFactSubject(in *IncomingContext) (string, string) {
	if in.IsGroup {
		if strings.TrimSpace(in.GroupID) != "" {
			return "group", in.GroupID
		}
		return "group_member", in.SenderID
	}
	id := strings.TrimSpace(in.OneID)
	if id == "" {
		id = in.SenderID
	}
	return "dm_person", id
}

func (p *defaultAgentContextFactsProvider) loadCustomer(ctx context.Context, in *IncomingContext, platform string, tenantID, accountID uint, subjectType, subjectID string) *agentcontext.CustomerContext {
	if p.customerLookup == nil {
		return nil
	}
	oneID := strings.TrimSpace(in.OneID)
	if oneID == "" {
		oneID = in.SenderID
	}
	var customer *model.Customer
	if oneID != "" {
		if c, err := p.customerLookup.GetByOneID(ctx, oneID); err == nil {
			customer = c
		}
	}
	if customer == nil && oneID != "" {
		if c, err := p.customerLookup.GetByID(ctx, oneID); err == nil {
			customer = c
		}
	}
	if customer == nil || customer.TenantID != 0 && customer.TenantID != tenantID {
		return nil
	}
	if in.IsGroup {
		subjectType = "group_member"
		subjectID = in.SenderID
	}
	out := &agentcontext.CustomerContext{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		DisplayName: strings.TrimSpace(customer.Name),
		Facts:       []agentcontext.BusinessFact{},
	}
	if customer.LastTouchAt != nil {
		out.UpdatedAt = *customer.LastTouchAt
	}
	if out.DisplayName != "" {
		out.Facts = append(out.Facts, agentcontext.BusinessFact{
			Key: "customer_name", Value: out.DisplayName, Grade: agentcontext.FactConfirmed,
			Source: "customer_crm", SubjectType: subjectType, SubjectID: subjectID,
		})
	}
	for _, tag := range strings.Split(customer.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		out.Facts = append(out.Facts, agentcontext.BusinessFact{
			Key: "customer_tag", Value: tag, Grade: agentcontext.FactInferred,
			Source: "customer_crm", SubjectType: subjectType, SubjectID: subjectID,
		})
	}
	return out
}

func (p *defaultAgentContextFactsProvider) loadLead(ctx context.Context, in *IncomingContext, platform string, tenantID, accountID uint, subjectType, subjectID string) *agentcontext.LeadContext {
	if p.leads == nil || platform == "" || accountID == 0 {
		return nil
	}
	rows, err := p.leads.ListRecentForReAct(
		ctx, platform, []string{strconv.FormatUint(uint64(accountID), 10)},
		in.ConversationID, in.GroupID, in.SenderID, in.IsGroup, "", 5,
	)
	if err != nil || len(rows) == 0 {
		return nil
	}
	var chosen *model.LeadMiningJudgement
	for i := range rows {
		row := &rows[i]
		if row.TenantID != 0 && row.TenantID != tenantID {
			continue
		}
		if !row.Matched {
			continue
		}
		if in.IsGroup && row.SenderID != "" && row.SenderID != in.SenderID {
			continue
		}
		if chosen == nil || row.CreatedAt.After(chosen.CreatedAt) || row.ID > chosen.ID {
			chosen = row
		}
	}
	if chosen == nil {
		return nil
	}
	rowSubjectType, rowSubjectID := subjectType, subjectID
	if in.IsGroup {
		if chosen.SenderID == "" {
			rowSubjectType, rowSubjectID = "group", in.GroupID
		} else {
			rowSubjectType, rowSubjectID = "group_member", chosen.SenderID
		}
	}
	requirement := strings.TrimSpace(chosen.Summary)
	if requirement == "" {
		requirement = strings.TrimSpace(chosen.Reason)
	}
	out := &agentcontext.LeadContext{
		SubjectType:        rowSubjectType,
		SubjectID:          rowSubjectID,
		Status:             strings.TrimSpace(chosen.LeadRole),
		Requirement:        requirement,
		Facts:              []agentcontext.BusinessFact{},
		EvidenceMessageIDs: append([]string{}, chosen.ContextMessageIDs...),
		UpdatedAt:          chosen.CreatedAt,
	}
	if requirement != "" {
		out.Facts = append(out.Facts, agentcontext.BusinessFact{
			Key: "lead_summary", Value: requirement, Grade: agentcontext.FactDerivedSummary,
			Source: "lead_mining_judgement", SubjectType: rowSubjectType, SubjectID: rowSubjectID,
		})
	}
	return out
}

func (p *defaultAgentContextFactsProvider) loadSummary(ctx context.Context, sessionID string) *agentcontext.MemorySummary {
	if p.memoryRepo == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	mem, err := p.memoryRepo.GetDialogueMemoryBySession(ctx, sessionID)
	if err != nil || mem == nil || strings.TrimSpace(mem.Summary) == "" {
		return nil
	}
	at := mem.LastActiveAt
	if at.IsZero() {
		at = mem.CreatedAt
	}
	return &agentcontext.MemorySummary{
		Summary: mem.Summary, Grade: agentcontext.FactDerivedSummary,
		SourceCursor: "session:" + sessionID, Version: int64(mem.ID), UpdatedAt: at,
	}
}

func (p *defaultAgentContextFactsProvider) loadConversationFacts(
	ctx context.Context, in *IncomingContext, platform string, tenantID, accountID uint,
	subjectType, subjectID string, now time.Time,
) ([]agentcontext.BusinessFact, []agentcontext.Participant) {
	facts := []agentcontext.BusinessFact{}
	participants := []agentcontext.Participant{}
	if p.conversation == nil || platform == "" || accountID == 0 {
		return facts, participants
	}
	scope := ConversationContextScope{Platform: platform, IsGroup: in.IsGroup, GroupID: in.GroupID}
	externalID := in.ConversationID
	if in.IsGroup {
		externalID = in.GroupID
	}
	externalID = conversationContextID(externalID, scope)
	if externalID == "" {
		return facts, participants
	}
	conv, err := p.conversation.Get(ctx, accountID, externalID)
	if err != nil || conv == nil || conv.TenantID != 0 && conv.TenantID != tenantID {
		return facts, participants
	}
	shared := SharedBusinessInputFromContext(conv, ContextNoteNamespaceAutoReply, sharedBusinessInputBudget)
	if shared.Summary != "" {
		facts = append(facts, agentcontext.BusinessFact{
			Key: "conversation_summary", Value: shared.Summary, Grade: agentcontext.FactDerivedSummary,
			Source: "conversation_context", SubjectType: subjectType, SubjectID: subjectID,
			ObservedAt: conv.UpdatedAt,
		})
	}
	for _, note := range shared.Notes {
		grade := agentcontext.FactLegacyUnverified
		switch NormalizeBusinessNoteAttribute(note.Attribute) {
		case BusinessNoteFact:
			grade = agentcontext.FactInferred
		case BusinessNoteSuggestion:
			grade = agentcontext.FactSuggestion
		}
		facts = append(facts, agentcontext.BusinessFact{
			Key: "shared_note", Value: strings.TrimSpace(note.Text), Grade: grade,
			Source: note.Source, ObservedAt: time.Unix(note.At, 0),
			SubjectType: subjectType, SubjectID: subjectID,
		})
	}
	for _, tag := range shared.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		facts = append(facts, agentcontext.BusinessFact{
			Key: "conversation_tag", Value: tag, Grade: agentcontext.FactInferred,
			Source: "conversation_context", SubjectType: subjectType, SubjectID: subjectID,
		})
	}
	for memberID, role := range conv.MemberRoles {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			continue
		}
		participants = append(participants, agentcontext.Participant{
			SenderID: memberID, DisplayName: memberID, MemberRole: strings.TrimSpace(role),
			IsCurrent: memberID == in.SenderID,
		})
	}
	if len(participants) == 0 && strings.TrimSpace(in.SenderID) != "" {
		participants = append(participants, agentcontext.Participant{SenderID: in.SenderID, DisplayName: in.SenderName, IsCurrent: true})
	}
	sort.SliceStable(participants, func(i, j int) bool {
		if participants[i].IsCurrent != participants[j].IsCurrent {
			return participants[i].IsCurrent
		}
		return participants[i].SenderID < participants[j].SenderID
	})
	return facts, participants
}
