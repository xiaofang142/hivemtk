package context

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ContextAssemblerInput 是一次入站的不可变输入；调用方负责先完成租户/可见性校验。
type ContextAssemblerInput struct {
	Scope           ConversationScope
	Current         ContextMessage
	Recent          []ContextMessage
	OlderSummary    *MemorySummary
	Participants    []Participant
	Customer        *CustomerContext
	Lead            *LeadContext
	SharedBusiness  []BusinessFact
	LLMConversation LLMConversationRef
	InputBudget     ContextBudget
	OutputBudget    OutputBudget
	Policy          SourcePolicy
	PromptVersion   string
	Now             time.Time
}

// AssembleAgentContext 生成确定性的单次入站上下文快照；不读取存储、不推断权限。
func AssembleAgentContext(input ContextAssemblerInput) (*AgentConversationContext, error) {
	if err := validateAssemblerInput(input); err != nil {
		return nil, err
	}
	scope := input.Scope
	if _, err := DeriveScopeIdentity(scope); err != nil {
		return nil, err
	}
	included := []string{}
	omitted := []string{}

	recent := append([]ContextMessage{}, input.Recent...)
	sort.SliceStable(recent, func(i, j int) bool {
		if recent[i].SentAt.Equal(recent[j].SentAt) {
			return recent[i].MessageID < recent[j].MessageID
		}
		return recent[i].SentAt.Before(recent[j].SentAt)
	})
	kept := make([]ContextMessage, 0, len(recent))
	if input.Policy.Kind != SourcePolicyUnproven {
		used := 0
		limit := input.InputBudget.HistoryTokens
		for i := len(recent) - 1; i >= 0; i-- {
			msg := recent[i]
			if !msg.Visibility.IsProven() {
				omitted = append(omitted, "visibility_unproven:"+msg.MessageID)
				continue
			}
			if limit > 0 {
				cost := EstimateContextTokens(msg.Content)
				if used+cost > limit {
					omitted = append(omitted, "history_budget:"+msg.MessageID)
					continue
				}
				used += cost
			}
			kept = append(kept, msg)
		}
		sort.SliceStable(kept, func(i, j int) bool {
			if kept[i].SentAt.Equal(kept[j].SentAt) {
				return kept[i].MessageID < kept[j].MessageID
			}
			return kept[i].SentAt.Before(kept[j].SentAt)
		})
		for _, msg := range kept {
			included = append(included, "message:"+msg.MessageID)
		}
	} else {
		omitted = append(omitted, "unproven_source_policy")
	}
	included = append(included, "current:"+input.Current.MessageID)

	customer := cloneCustomer(input.Customer)
	lead := cloneLead(input.Lead)
	shared := append([]BusinessFact{}, input.SharedBusiness...)
	participants := append([]Participant{}, input.Participants...)
	older := input.OlderSummary
	if input.Policy.Kind == SourcePolicyUnproven {
		customer = nil
		lead = nil
		shared = nil
		participants = nil
		older = nil
	} else if input.Policy.Kind == SourcePolicyGroup {
		customer = filterGroupFacts(customer, input.Current.SenderID)
		lead = filterGroupFactsLead(lead, input.Current.SenderID)
		shared = filterGroupFactsList(shared, input.Current.SenderID)
	}
	if customer != nil {
		for _, fact := range customer.Facts {
			included = append(included, "customer_fact:"+fact.Key+":"+string(fact.Grade))
		}
	}
	if lead != nil {
		for _, fact := range lead.Facts {
			included = append(included, "lead_fact:"+fact.Key+":"+string(fact.Grade))
		}
	}

	visibilityProof := "scope_verified"
	switch input.Policy.Kind {
	case SourcePolicyGroup:
		if groupAudienceProven(input, kept) {
			visibilityProof = "group_audienceVerified"
		} else {
			visibilityProof = "visibility_unproven"
		}
	case SourcePolicyUnproven:
		visibilityProof = "visibility_unproven"
	}

	outputBudget := input.OutputBudget
	if input.InputBudget.MaxOutputTokens > 0 && outputBudget.MaxOutputTokens > input.InputBudget.MaxOutputTokens {
		outputBudget.MaxOutputTokens = input.InputBudget.MaxOutputTokens
	}

	snapshotAt := input.Now
	if snapshotAt.IsZero() {
		snapshotAt = time.Now().UTC()
	}
	snapshotID := assemblerSnapshotID(input, kept, customer, lead, shared, snapshotAt)

	return &AgentConversationContext{
		SnapshotID:      snapshotID,
		Scope:           scope,
		Current:         input.Current,
		Recent:          kept,
		OlderSummary:    older,
		Participants:    participants,
		Customer:        customer,
		Lead:            lead,
		SharedBusiness:  shared,
		LLMConversation: input.LLMConversation,
		InputBudget:     input.InputBudget,
		OutputBudget:    outputBudget,
		Provenance: ContextProvenance{
			ContextSchemaVersion: ContextSchemaVersion,
			PromptVersion:        input.PromptVersion,
			SourcePolicyVersion:  input.Policy.Version,
			Included:             included,
			Omitted:              omitted,
			TruncationCursor:     truncationCursor(kept),
			SummarySourceCursor:  summaryCursor(older),
			Redactions:           []string{},
			VisibilityProof:      visibilityProof,
			SnapshotAt:           snapshotAt,
		},
	}, nil
}

// groupAudienceProven 只在实际消息元数据能证明可见性时授予群 audience 证明。
// 不能仅凭 SourcePolicyGroup（来自 is_group 标志）就认为可见性已被验证。
func groupAudienceProven(input ContextAssemblerInput, kept []ContextMessage) bool {
	if strings.TrimSpace(input.Scope.GroupID) == "" {
		return false
	}
	if !input.Current.Visibility.IsProven() {
		return false
	}
	if !messageVisibleToGroup(input.Current, input.Scope.GroupID) {
		return false
	}
	for _, msg := range kept {
		if !msg.Visibility.IsProven() || !messageVisibleToGroup(msg, input.Scope.GroupID) {
			return false
		}
	}
	return true
}

func messageVisibleToGroup(msg ContextMessage, groupID string) bool {
	switch msg.Visibility {
	case VisibilityPublic:
		return true
	case VisibilityMember, VisibilityPrivate:
		for _, audience := range msg.Audience {
			if strings.TrimSpace(audience) == groupID {
				return true
			}
		}
	}
	return false
}

func validateAssemblerInput(input ContextAssemblerInput) error {
	if input.Scope.TenantID == 0 || strings.TrimSpace(input.Scope.Platform) == "" || input.Scope.ChannelAccountID == 0 {
		return errors.New("上下文 scope 缺租户/平台/账号")
	}
	if strings.TrimSpace(input.Current.MessageID) == "" {
		return errors.New("当前消息缺少真实 ID")
	}
	return nil
}

// EstimateContextTokens 是保守本地估算；供应商真实计量以 checked output 的 usage 为准。
func EstimateContextTokens(text string) int {
	runes := len([]rune(text))
	return runes/4 + 4
}

func assemblerSnapshotID(input ContextAssemblerInput, recent []ContextMessage, customer *CustomerContext, lead *LeadContext, shared []BusinessFact, at time.Time) string {
	parts := []string{
		ContextSchemaVersion,
		input.PromptVersion,
		input.Policy.Version,
		input.Current.MessageID,
		input.Current.Content,
		strconv.FormatInt(at.UnixNano(), 10),
	}
	for _, msg := range recent {
		parts = append(parts, "m:"+msg.MessageID+":"+msg.Content)
	}
	if customer != nil && len(customer.Facts) > 0 {
		parts = append(parts, "customer:"+customer.SubjectType+":"+customer.SubjectID)
		for _, fact := range customer.Facts {
			parts = append(parts, "cf:"+fact.Key+":"+fact.Value+":"+string(fact.Grade))
		}
	}
	if lead != nil && len(lead.Facts) > 0 {
		parts = append(parts, "lead:"+lead.SubjectType+":"+lead.SubjectID)
		for _, fact := range lead.Facts {
			parts = append(parts, "lf:"+fact.Key+":"+fact.Value+":"+string(fact.Grade))
		}
	}
	for _, fact := range shared {
		parts = append(parts, "sf:"+fact.Key+":"+fact.Value+":"+string(fact.Grade))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func truncationCursor(recent []ContextMessage) string {
	if len(recent) == 0 {
		return ""
	}
	return recent[0].MessageID
}

func summaryCursor(summary *MemorySummary) string {
	if summary == nil {
		return ""
	}
	return summary.SourceCursor
}

func cloneCustomer(in *CustomerContext) *CustomerContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Facts = append([]BusinessFact{}, in.Facts...)
	return &out
}

func cloneLead(in *LeadContext) *LeadContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Facts = append([]BusinessFact{}, in.Facts...)
	out.EvidenceMessageIDs = append([]string{}, in.EvidenceMessageIDs...)
	return &out
}

func groupFactAllowed(fact BusinessFact, senderID string) bool {
	if fact.SubjectType != "group_member" {
		return true
	}
	return senderID != "" && fact.SubjectID == senderID && fact.Grade == FactConfirmed
}

func filterGroupFactsList(in []BusinessFact, senderID string) []BusinessFact {
	out := make([]BusinessFact, 0, len(in))
	for _, fact := range in {
		if groupFactAllowed(fact, senderID) {
			out = append(out, fact)
		}
	}
	return out
}

func filterGroupFacts(in *CustomerContext, senderID string) *CustomerContext {
	if in == nil {
		return nil
	}
	out := cloneCustomer(in)
	out.Facts = filterGroupFactsList(in.Facts, senderID)
	return out
}

func filterGroupFactsLead(in *LeadContext, senderID string) *LeadContext {
	if in == nil {
		return nil
	}
	out := cloneLead(in)
	out.Facts = filterGroupFactsList(in.Facts, senderID)
	return out
}
