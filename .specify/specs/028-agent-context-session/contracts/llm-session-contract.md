# Contract: LLM Session, Execution Scenario & Provider Capability

## 1. Identity

```go
type LLMConversationRef struct {
    LLMConversationID string
    ScopeHash         string
}

type LLMSessionRef struct {
    BusLLMSessionID       string // canonical continuation key
    ExecutionScenarioID   string
    Provider              string
    Model                 string
    Protocol              string
    Region                string
    Generation            int64
    ProviderContinuation  string // encrypted at rest; adapter-specific
    PromptPrefixVersion   string
    ContextSchemaVersion  string
}
```

`BusLLMSessionID` is carried from the message bus and validated against tenant/platform/account/conversation-or-group scope. If absent, user-server derives it once from stable scope and returns it to downstream events. `ExecutionScenarioID` isolates execution chains; `DispatchScenario` remains routing/billing only.

## 2. ExecutionScenarioID values

- `intent`
- `fine_intent`
- `emotion`
- `objection`
- `humanize_eval`
- `humanize_rewrite`
- `sales_generate`
- `lead_detect`
- `lead_judge`
- `lead_react`
- `agent_runtime`
- `summary`

Unknown values are rejected for session state creation. Routing scenarios such as `low_cost` never select a session row.

## 3. ProviderCapability

```go
type ProviderCapability struct {
    Provider              string
    Model                 string
    Protocol              string
    Region                string
    ContextWindow         int
    MaxInputTokens        int
    ProtocolInputLimit    int
    MaxOutputTokens       int
    ReasoningReserveTokens int
    OutputSemantics       string // response_only | response_plus_reasoning
    CacheMode             string // none | implicit | explicit | provider_session_cache
    CacheMinTokens        int
    CacheTTL              time.Duration
    SessionMode           string // none | native_response | provider_conversation
    NativeSessionAllowed  bool
    StoreMode             string // stateless | native
    AllowedRegions        []string
    SupportsIdempotencyKey bool
    MaxRequestBytes       int64
    RequestTimeout        time.Duration
    RouteMode             string // direct | platform_proxy
}
```

Effective input budget:

```text
min(
  ContextWindow - actualOutputReserve - ReasoningReserveTokens - SafetyMargin,
  MaxInputTokens,
  ProtocolInputLimit
)
```

Unknown capabilities fail conservative; they do not inherit a vendor-wide million-token assumption.

## 4. Request and response

`DispatchRequest` adds:

- `LLMSessionRef`
- `ContextSnapshotID`
- `PromptPrefixVersion`
- `CachePolicy`
- `RequestIdempotencyKey`
- `ExecutionScenarioID`

Responses add normalized usage:

- `PromptTokens`
- `CompletionTokens`
- `ReasoningTokens`
- `CachedInputTokens`
- `CacheCreationTokens`
- `CacheReadTokens`
- `FinishReason`
- `ProviderContinuationRef`
- `RequestID`

A checked API returns an error when `FinishReason=length`, structured schema validation fails, evidence IDs are absent, or privacy validation fails. Callers must not send unchecked output.

## 5. State and concurrency

- Unique state key: canonical session blind index + execution scenario + provider/model/protocol/region + generation.
- Native continuation uses a full-scope lease covering provider call, tools and commit, with a fencing token.
- Lease failure is fail-closed for native mode or explicitly degrades to stateless.
- Per-inbound `ConversationMessageDisposition` is the user-visible reply state; scenario watermarks cannot replace it.
- `planned → authorized → dispatched → unknown|confirmed` is the tool side-effect state machine.

## 6. Cache separation

- Session reuse is continuity, not a cache hit.
- Prompt cache is provider-side prefix reuse, not answer reuse.
- Answer cache remains limited to eligible pure-knowledge new questions and bypasses personalized/tool/multi-turn flows.
