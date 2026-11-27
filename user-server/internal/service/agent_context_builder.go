package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// ConversationContextReader 只暴露构建器需要的历史读口。
type ConversationContextReader interface {
	List(ctx context.Context, query UnifiedMessageQuery) ([]UnifiedMessage, error)
}

type AgentContextBuilderInput struct {
	Scope          agentcontext.ConversationScope
	Current        agentcontext.ContextMessage
	Customer       *agentcontext.CustomerContext
	Lead           *agentcontext.LeadContext
	SharedBusiness []agentcontext.BusinessFact
	OlderSummary   *agentcontext.MemorySummary
	Participants   []agentcontext.Participant
	// Memberships 是当前发言者在群内的成员关系证据；群聊历史过滤时交给
	// FilterVisibleGroupMessages 做 fail-closed 判定。缺省时 member 可见性
	// 只能靠消息显式 audience 证明。
	Memberships   []model.ConversationMembership
	PromptVersion string
	PolicyKind    string
	InputBudget   agentcontext.ContextBudget
	OutputBudget  agentcontext.OutputBudget
	Limit         int
	Now           time.Time
}

type ConversationContextBuilder struct {
	reader ConversationContextReader
}

func NewConversationContextBuilder(reader ConversationContextReader) *ConversationContextBuilder {
	return &ConversationContextBuilder{reader: reader}
}

func (b *ConversationContextBuilder) Build(ctx context.Context, input AgentContextBuilderInput) (*agentcontext.AgentConversationContext, error) {
	if b == nil || b.reader == nil {
		return nil, errors.New("上下文历史读取器未装配")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || tid != input.Scope.TenantID {
		return nil, errors.New("共享上下文租户范围无效")
	}
	query, err := builderMessageQuery(input)
	if err != nil {
		return nil, err
	}
	rows, err := b.reader.List(ctx, query)
	if err != nil {
		return nil, err
	}
	history := builderVisibleHistory(rows, input.Current.SenderID, input.Scope, input.Memberships)
	customer := builderFilterCustomer(input.Customer, input.Current.SenderID)
	lead := builderFilterLead(input.Lead, input.Current.SenderID)
	shared := builderFilterFacts(input.SharedBusiness, input.Current.SenderID)

	identity, err := agentcontext.DeriveScopeIdentity(input.Scope)
	if err != nil {
		return nil, err
	}
	kind := agentcontext.SourcePolicyKind(input.PolicyKind)
	if kind == "" {
		if input.Scope.IsGroup {
			kind = agentcontext.SourcePolicyGroup
		} else {
			kind = agentcontext.SourcePolicyDM
		}
	}
	policy := agentcontext.SourcePolicy{Kind: kind, Version: "source-policy-v1"}
	if kind == agentcontext.SourcePolicyUnproven {
		history = nil
		customer = nil
		lead = nil
		shared = nil
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	return agentcontext.AssembleAgentContext(agentcontext.ContextAssemblerInput{
		Scope:          input.Scope,
		Current:        input.Current,
		Recent:         history,
		OlderSummary:   input.OlderSummary,
		Participants:   input.Participants,
		Customer:       customer,
		Lead:           lead,
		SharedBusiness: shared,
		LLMConversation: agentcontext.LLMConversationRef{
			ID: identity.LLMConversationID, ScopeHash: identity.ScopeHash,
		},
		InputBudget:   input.InputBudget,
		OutputBudget:  input.OutputBudget,
		Policy:        policy,
		PromptVersion: input.PromptVersion,
		Now:           now,
	})
}

func builderMessageQuery(input AgentContextBuilderInput) (UnifiedMessageQuery, error) {
	scope := input.Scope
	if scope.Platform == "" || scope.ChannelAccountID == 0 {
		return UnifiedMessageQuery{}, errors.New("渠道账号或平台缺失")
	}
	if scope.IsGroup && scope.GroupID == "" {
		return UnifiedMessageQuery{}, errors.New("群聊缺 group_id 根标识")
	}
	if !scope.IsGroup && scope.ConversationID == "" && scope.StablePeerID == "" {
		return UnifiedMessageQuery{}, errors.New("单聊缺稳定会话标识")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	accountIDs := []string{strconv.FormatUint(uint64(scope.ChannelAccountID), 10)}
	conversationID := scope.ConversationID
	if scope.IsGroup {
		conversationID = ""
	}
	return UnifiedMessageQuery{
		TenantID: scope.TenantID, Platform: scope.Platform, AccountIDs: accountIDs,
		IsGroup: scope.IsGroup, ConversationID: conversationID,
		GroupID: scope.GroupID, StablePeerID: scope.StablePeerID, Limit: limit,
	}, nil
}

func builderVisibleHistory(rows []UnifiedMessage, currentSenderID string, scope agentcontext.ConversationScope, memberships []model.ConversationMembership) []agentcontext.ContextMessage {
	messages := make([]agentcontext.ContextMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, row.ContextMessage)
	}
	var out []agentcontext.ContextMessage
	if scope.IsGroup {
		// 群聊历史统一走 fail-closed 过滤器：public 放行；member 必须有显式
		// audience 或可证明的成员区间；private 必须显式 audience；未知可见性
		// 永不授权。单聊仍沿用原有 audience/发言者口径。
		out = FilterVisibleGroupMessages(messages, memberships, scope, currentSenderID).Visible
	} else {
		out = make([]agentcontext.ContextMessage, 0, len(rows))
		for _, msg := range messages {
			if builderCanSee(msg, currentSenderID) {
				out = append(out, msg)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SentAt.Equal(out[j].SentAt) {
			return out[i].MessageID < out[j].MessageID
		}
		return out[i].SentAt.Before(out[j].SentAt)
	})
	return out
}

func builderCanSee(msg agentcontext.ContextMessage, currentSenderID string) bool {
	switch msg.Visibility {
	case agentcontext.VisibilityPublic:
		return true
	case agentcontext.VisibilityMember, agentcontext.VisibilityPrivate:
		if currentSenderID != "" && msg.SenderID == currentSenderID {
			return true
		}
		for _, audience := range msg.Audience {
			if audience == currentSenderID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func builderFilterFacts(facts []agentcontext.BusinessFact, currentSenderID string) []agentcontext.BusinessFact {
	out := make([]agentcontext.BusinessFact, 0, len(facts))
	for _, fact := range facts {
		if fact.SubjectType == "group_member" && fact.SubjectID != "" && fact.SubjectID != currentSenderID {
			continue
		}
		out = append(out, fact)
	}
	return out
}

func builderFilterCustomer(customer *agentcontext.CustomerContext, currentSenderID string) *agentcontext.CustomerContext {
	if customer == nil {
		return nil
	}
	copy := *customer
	copy.Facts = builderFilterFacts(customer.Facts, currentSenderID)
	if copy.SubjectType == "group_member" && copy.SubjectID != "" && copy.SubjectID != currentSenderID {
		return nil
	}
	return &copy
}

func builderFilterLead(lead *agentcontext.LeadContext, currentSenderID string) *agentcontext.LeadContext {
	if lead == nil {
		return nil
	}
	copy := *lead
	copy.Facts = builderFilterFacts(lead.Facts, currentSenderID)
	if copy.SubjectType == "group_member" && copy.SubjectID != "" && copy.SubjectID != currentSenderID {
		return nil
	}
	return &copy
}
