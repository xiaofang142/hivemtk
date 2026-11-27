// Package context defines provider- and storage-independent conversation
// context contracts used by the agent reply and lead pipelines.
package context

import "time"

const ContextSchemaVersion = "agent-context-v1"

type FactGrade string

const (
	FactConfirmed         FactGrade = "confirmed"
	FactInferred          FactGrade = "inferred"
	FactDerivedSummary    FactGrade = "derived_summary"
	FactSuggestion        FactGrade = "suggestion"
	FactExternalUntrusted FactGrade = "external_untrusted"
	FactLegacyUnverified  FactGrade = "legacy_unverified"
)

func (g FactGrade) CanStateAsFact() bool {
	return g == FactConfirmed
}

type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityMember  Visibility = "member"
	VisibilityPrivate Visibility = "private"
	VisibilityUnknown Visibility = "unknown"
)

func (v Visibility) IsProven() bool {
	switch v {
	case VisibilityPublic, VisibilityMember, VisibilityPrivate:
		return true
	default:
		return false
	}
}

type SourcePolicyKind string

const (
	SourcePolicyDM       SourcePolicyKind = "dm"
	SourcePolicyGroup    SourcePolicyKind = "group"
	SourcePolicyUnproven SourcePolicyKind = "unproven"
)

type MessageOrigin string

const (
	OriginMessageHub     MessageOrigin = "message_hub"
	OriginSessionMessage MessageOrigin = "session_message"
)

type HistorySource string

const (
	HistorySourceMessageBus     HistorySource = "message_bus"
	HistorySourceMessageHub     HistorySource = "message_hub"
	HistorySourceSessionMessage HistorySource = "session_message"
)

type ConversationScope struct {
	TenantID         uint
	Platform         string
	ChannelAccountID uint
	BusSessionID     string
	BusLLMSessionID  string
	SessionID        string
	ConversationID   string
	StablePeerID     string
	IsGroup          bool
	GroupID          string
}

type ContextMessage struct {
	MessageID      string
	Origin         MessageOrigin
	CorrelationID  string
	HistorySource  HistorySource
	SourceMetadata map[string]string
	Role           string
	Direction      string
	SenderID       string
	SenderName     string
	MemberRole     string
	Content        string
	MediaSummary   string
	SentAt         time.Time
	ReplyTo        string
	Mentions       []string
	Visibility     Visibility
	Audience       []string
	VisibleFrom    *time.Time
	VisibleTo      *time.Time
}

type Participant struct {
	SenderID    string
	DisplayName string
	MemberRole  string
	IsCurrent   bool
	VisibleFrom *time.Time
	VisibleTo   *time.Time
}

type BusinessFact struct {
	Key             string
	Value           string
	Grade           FactGrade
	Source          string
	SourceMessageID string
	ObservedAt      time.Time
	SubjectType     string
	SubjectID       string
}

type MemorySummary struct {
	Summary      string
	Grade        FactGrade
	SourceCursor string
	Version      int64
	UpdatedAt    time.Time
}

type CustomerContext struct {
	SubjectType string
	SubjectID   string
	DisplayName string
	Facts       []BusinessFact
	UpdatedAt   time.Time
}

type LeadContext struct {
	SubjectType        string
	SubjectID          string
	Status             string
	Requirement        string
	Facts              []BusinessFact
	EvidenceMessageIDs []string
	UpdatedAt          time.Time
}

type LLMConversationRef struct {
	ID        string
	ScopeHash string
}

type ContextBudget struct {
	MaxInputTokens     int
	MaxOutputTokens    int
	SafetyMarginTokens int
	HistoryTokens      int
	SummaryTokens      int
	ProfileTokens      int
	RetrievalTokens    int
}

type OutputBudget struct {
	MaxOutputTokens        int
	ReasoningReserveTokens int
	ShortReplyMin          int
	ShortReplyMax          int
	LongReplyMin           int
	LongReplyMax           int
	OutputSemantics        string
}

type ContextProvenance struct {
	ContextSchemaVersion string
	PromptVersion        string
	SourcePolicyVersion  string
	Included             []string
	Omitted              []string
	TruncationCursor     string
	SummarySourceCursor  string
	Redactions           []string
	VisibilityProof      string
	SnapshotAt           time.Time
}

type SourcePolicy struct {
	Kind         SourcePolicyKind
	Instructions string
	Version      string
}

type AgentConversationContext struct {
	SnapshotID      string
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
	Provenance      ContextProvenance
}
