package model

import "time"

// LLMSessionState 保存会话续接元数据，不存原始消息、客户画像或线索内容。
type LLMSessionState struct {
	ID                         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID                   uint      `gorm:"not null;uniqueIndex:uni_llm_session_state,priority:1" json:"tenant_id"`
	Platform                   string    `gorm:"type:varchar(32);not null" json:"platform"`
	ChannelAccountID           uint      `gorm:"not null;uniqueIndex:uni_llm_session_state,priority:2" json:"channel_account_id"`
	LLMConversationID          string    `gorm:"type:varchar(191);not null;index" json:"llm_conversation_id"`
	ScopeHash                  string    `gorm:"type:char(64);not null;index" json:"scope_hash"`
	BusLLMSessionIDEnc         []byte    `gorm:"type:bytea;not null" json:"-"`
	BusLLMSessionIDBlind       string    `gorm:"type:char(64);not null;uniqueIndex:uni_llm_session_state,priority:3;index" json:"bus_llm_session_id_blind"`
	ExecutionScenarioID        string    `gorm:"type:varchar(64);not null;uniqueIndex:uni_llm_session_state,priority:4" json:"execution_scenario_id"`
	DispatchScenario           string    `gorm:"type:varchar(64);not null;default:''" json:"dispatch_scenario"`
	Provider                   string    `gorm:"type:varchar(64);not null;uniqueIndex:uni_llm_session_state,priority:5" json:"provider"`
	Model                      string    `gorm:"type:varchar(191);not null;uniqueIndex:uni_llm_session_state,priority:6" json:"model"`
	Protocol                   string    `gorm:"type:varchar(64);not null;uniqueIndex:uni_llm_session_state,priority:7" json:"protocol"`
	Region                     string    `gorm:"type:varchar(64);not null;uniqueIndex:uni_llm_session_state,priority:8" json:"region"`
	Generation                 int64     `gorm:"not null;default:1;uniqueIndex:uni_llm_session_state,priority:9" json:"generation"`
	ProviderContinuationRefEnc []byte    `gorm:"type:bytea" json:"-"`
	LastMessageID              string    `gorm:"type:varchar(191);not null;default:''" json:"last_message_id"`
	LastMessageCursor          string    `gorm:"type:varchar(191);not null;default:''" json:"last_message_cursor"`
	RepliedThroughCursor       string    `gorm:"type:varchar(191);not null;default:''" json:"replied_through_cursor"`
	PromptPrefixVersion        string    `gorm:"type:varchar(64);not null" json:"prompt_prefix_version"`
	ContextSchemaVersion       string    `gorm:"type:varchar(32);not null" json:"context_schema_version"`
	CapabilityFingerprint      string    `gorm:"type:char(64);not null" json:"capability_fingerprint"`
	RetentionMode              string    `gorm:"type:varchar(32);not null;default:'stateless'" json:"retention_mode"`
	ExpiresAt                  time.Time `gorm:"not null;index" json:"expires_at"`
	LastUsedAt                 time.Time `gorm:"not null" json:"last_used_at"`
	Version                    int64     `gorm:"not null;default:1" json:"version"`
	CreatedAt                  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt                  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (LLMSessionState) TableName() string { return "llm_session_states" }

// ScenarioTranscript 保存可重建的加密执行片段；platform 代理不得复制该内容。
type ScenarioTranscript struct {
	ID                    uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID              uint      `gorm:"not null;index" json:"tenant_id"`
	LLMSessionStateID     uint      `gorm:"not null;uniqueIndex:uni_scenario_transcript_sequence,priority:1;uniqueIndex:uni_scenario_transcript_idempotency,priority:1" json:"llm_session_state_id"`
	SnapshotID            string    `gorm:"type:varchar(191);not null" json:"snapshot_id"`
	ExecutionScenarioID   string    `gorm:"type:varchar(64);not null" json:"execution_scenario_id"`
	Generation            int64     `gorm:"not null;default:1;uniqueIndex:uni_scenario_transcript_sequence,priority:2" json:"generation"`
	SequenceNo            int64     `gorm:"not null;uniqueIndex:uni_scenario_transcript_sequence,priority:3" json:"sequence_no"`
	EntryType             string    `gorm:"type:varchar(32);not null" json:"entry_type"`
	PayloadEnc            []byte    `gorm:"type:bytea;not null" json:"-"`
	PayloadHash           string    `gorm:"type:char(64);not null" json:"payload_hash"`
	ProviderRequestHash   string    `gorm:"type:char(64);not null;default:''" json:"provider_request_hash"`
	RequestIdempotencyKey string    `gorm:"type:varchar(191);not null;uniqueIndex:uni_scenario_transcript_idempotency,priority:2" json:"request_idempotency_key"`
	CreatedAt             time.Time `gorm:"autoCreateTime" json:"created_at"`
	ExpiresAt             time.Time `gorm:"not null;index" json:"expires_at"`
}

func (ScenarioTranscript) TableName() string { return "scenario_transcripts" }

// ToolInvocation 记录副作用工具的授权、派发、未知与确认状态。
type ToolInvocation struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           uint      `gorm:"not null;index" json:"tenant_id"`
	TranscriptID       uint      `gorm:"not null;index" json:"transcript_id"`
	ToolName           string    `gorm:"type:varchar(128);not null" json:"tool_name"`
	ToolIdempotencyKey string    `gorm:"type:varchar(191);not null;default:''" json:"tool_idempotency_key,omitempty"`
	RiskClass          string    `gorm:"type:varchar(32);not null" json:"risk_class"`
	Status             string    `gorm:"type:varchar(32);not null;default:'planned';index" json:"status"`
	AuthorizationHash  string    `gorm:"type:char(64);not null" json:"authorization_hash"`
	ResourceVersion    string    `gorm:"type:varchar(191);not null;default:''" json:"resource_version"`
	RequestHash        string    `gorm:"type:char(64);not null" json:"request_hash"`
	ResultEnc          []byte    `gorm:"type:bytea" json:"-"`
	ResultHash         string    `gorm:"type:char(64);not null;default:''" json:"result_hash"`
	ExternalReceiptID  string    `gorm:"type:varchar(191);not null;default:''" json:"external_receipt_id,omitempty"`
	AttemptCount       int       `gorm:"not null;default:1" json:"attempt_count"`
	LastErrorCode      string    `gorm:"type:varchar(64);not null;default:''" json:"last_error_code,omitempty"`
	CreatedAt          time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ToolInvocation) TableName() string { return "tool_invocations" }

const (
	LLMSessionRetentionStateless = "stateless"
	LLMSessionRetentionNative    = "native"

	TranscriptModelStep  = "model_step"
	TranscriptToolCall   = "tool_call"
	TranscriptToolResult = "tool_result"
	TranscriptHandoff    = "handoff"
	TranscriptCompaction = "compaction"

	ToolRiskRead      = "read"
	ToolRiskWrite     = "write"
	ToolRiskHighWrite = "high_write"

	ToolInvocationPlanned    = "planned"
	ToolInvocationAuthorized = "authorized"
	ToolInvocationDispatched = "dispatched"
	ToolInvocationUnknown    = "unknown"
	ToolInvocationConfirmed  = "confirmed"
)
