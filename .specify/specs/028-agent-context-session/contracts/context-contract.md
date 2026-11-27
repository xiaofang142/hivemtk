# Contract: Agent Conversation Context

**Package:** `internal/aiagent/context`  
**Rule:** Pure Go domain contracts only; no GORM, gin, HTTP, or provider SDK imports.

## ConversationScope

```go
type ConversationScope struct {
    TenantID         uint
    Platform         string
    ChannelAccountID uint
    BusSessionID     string
    BusLLMSessionID  string
    SessionID        string
    ConversationID   string
    IsGroup          bool
    GroupID          string
}
```

Invariants:

- Tenant/platform/account are mandatory.
- Group scope uses `GroupID` as root; conversation/thread is a child dimension.
- DM without stable SenderID/conversation fails closed; SenderName is never an identity fallback.
- `BusLLMSessionID` is canonical continuation key; server derives once when absent and binds it to scope.
- Reuse across another scope is rejected and audited.

## AgentConversationContext

```go
type AgentConversationContext struct {
    SnapshotID     string
    Scope          ConversationScope
    Current        ContextMessage
    Recent         []ContextMessage
    OlderSummary   *MemorySummary
    Participants   []Participant
    Customer       *CustomerContext
    Lead           *LeadContext
    SharedBusiness []BusinessFact
    LLMConversation LLMConversationRef
    InputBudget    ContextBudget
    OutputBudget   OutputBudget
    Provenance     ContextProvenance
}
```

`SnapshotID = hash(MessageID, asOfCursor, contentHash, factVersions, policyVersion, contextSchemaVersion)`.

The snapshot is immutable within one inbound execution. Retries rebuild unless manifest and input watermark match.

## ContextMessage

```go
type ContextMessage struct {
    MessageID      string
    Origin         string // message_hub | session_message
    CorrelationID  string
    HistorySource  string // message_bus | message_hub | session_message
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
    Visibility     string // public | member | private | unknown
    Audience       []string
    VisibleFrom    *time.Time
    VisibleTo      *time.Time
}
```

Rules:

- Real MessageHub ID is required for evidence references.
- SenderName is display-only.
- `unknown` visibility in group history is fail-closed.
- Audience is intersected with membership intervals before inclusion.
- `SourceMetadata` is allowlisted; arbitrary channel extra fields are not injected.

## Fact grades

`confirmed`, `inferred`, `derived_summary`, `suggestion`, `external_untrusted`, `legacy_unverified`.

Only confirmed facts may be stated as completed/authorized reality. `legacy_unverified` and `derived_summary` retain source references and cannot be auto-promoted.

## SourcePolicy

The builder emits a server-owned policy block:

- `dm`: two-party relationship, long-term continuity, current customer intent.
- `group`: current speaker, member roles, audience, multi-party intent, noise and reply-scope rules.
- `unproven`: minimal current-speaker/current-message context with `visibility_unproven` provenance.

Message content cannot modify SourcePolicy.

## Provenance

Must include schema/prompt/source-policy versions, inclusion/omission reasons, truncation cursor, redaction markers, summary source cursor, visibility proof status, and snapshot time.
