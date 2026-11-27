package model

import (
	"strings"
	"testing"
)

func TestLLMSessionLedgerTableNames(t *testing.T) {
	tests := []struct {
		model     any
		tableName string
	}{
		{model: &LLMSessionState{}, tableName: "llm_session_states"},
		{model: &ScenarioTranscript{}, tableName: "scenario_transcripts"},
		{model: &ToolInvocation{}, tableName: "tool_invocations"},
	}
	for _, tt := range tests {
		modelSchema := parseAgentContextModel(t, tt.model)
		if modelSchema.Table != tt.tableName {
			t.Fatalf("表名错误：got %q, want %q", modelSchema.Table, tt.tableName)
		}
	}
}

func TestLLMSessionStateSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &LLMSessionState{})
	for _, name := range []string{
		"tenant_id", "platform", "channel_account_id", "llm_conversation_id", "scope_hash",
		"bus_llm_session_id_enc", "bus_llm_session_id_blind", "execution_scenario_id",
		"provider", "model", "protocol", "region", "generation", "prompt_prefix_version",
		"context_schema_version", "capability_fingerprint", "retention_mode", "expires_at",
		"last_used_at", "version",
	} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	for _, name := range []string{"provider_continuation_ref_enc"} {
		if requireAgentContextField(t, modelSchema, name).NotNull {
			t.Fatalf("字段 %q 必须允许为空", name)
		}
	}
	for _, name := range []string{"tenant_id", "channel_account_id", "bus_llm_session_id_blind", "execution_scenario_id", "provider", "model", "protocol", "region", "generation"} {
		requireAgentContextIndex(t, modelSchema, name, "uni_llm_session_state")
	}
	if got := requireAgentContextField(t, modelSchema, "generation").DefaultValue; got != "1" {
		t.Fatalf("generation 默认值错误：%q", got)
	}
	if got := requireAgentContextField(t, modelSchema, "retention_mode").DefaultValue; got != LLMSessionRetentionStateless {
		t.Fatalf("retention_mode 默认值错误：%q", got)
	}
}

func TestScenarioTranscriptSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &ScenarioTranscript{})
	for _, name := range []string{"tenant_id", "llm_session_state_id", "snapshot_id", "execution_scenario_id", "generation", "sequence_no", "entry_type", "payload_enc", "payload_hash", "request_idempotency_key", "expires_at"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	for _, name := range []string{"llm_session_state_id", "generation", "sequence_no"} {
		requireAgentContextIndex(t, modelSchema, name, "uni_scenario_transcript_sequence")
	}
	for _, name := range []string{"llm_session_state_id", "request_idempotency_key"} {
		requireAgentContextIndex(t, modelSchema, name, "uni_scenario_transcript_idempotency")
	}
}

func TestToolInvocationSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &ToolInvocation{})
	for _, name := range []string{"tenant_id", "transcript_id", "tool_name", "risk_class", "status", "authorization_hash", "request_hash", "attempt_count"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	if got := requireAgentContextField(t, modelSchema, "status").DefaultValue; got != ToolInvocationPlanned {
		t.Fatalf("status 默认值错误：%q", got)
	}
	if got := requireAgentContextField(t, modelSchema, "attempt_count").DefaultValue; got != "1" {
		t.Fatalf("attempt_count 默认值错误：%q", got)
	}
	if requireAgentContextField(t, modelSchema, "result_enc").NotNull {
		t.Fatal("result_enc 必须允许为空")
	}
}

func TestLLMSessionLedgerEnumValuesStable(t *testing.T) {
	groups := [][]string{
		{LLMSessionRetentionStateless, LLMSessionRetentionNative},
		{TranscriptModelStep, TranscriptToolCall, TranscriptToolResult, TranscriptHandoff, TranscriptCompaction},
		{ToolRiskRead, ToolRiskWrite, ToolRiskHighWrite},
		{ToolInvocationPlanned, ToolInvocationAuthorized, ToolInvocationDispatched, ToolInvocationUnknown, ToolInvocationConfirmed},
	}
	for _, values := range groups {
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			if value == "" || seen[value] {
				t.Fatalf("枚举为空或重复：%q", value)
			}
			if strings.TrimSpace(value) != value {
				t.Fatalf("枚举不得含首尾空白：%q", value)
			}
			seen[value] = true
		}
	}
}
