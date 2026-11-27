package model

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func parseAgentContextModel(t *testing.T, value any) *schema.Schema {
	t.Helper()
	modelSchema, err := schema.Parse(value, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("解析 schema 失败：%v", err)
	}
	return modelSchema
}

func requireAgentContextField(t *testing.T, modelSchema *schema.Schema, name string) *schema.Field {
	t.Helper()
	field := modelSchema.LookUpField(name)
	if field == nil {
		t.Fatalf("schema 缺少字段 %q", name)
	}
	return field
}

func requireAgentContextIndex(t *testing.T, modelSchema *schema.Schema, fieldName, indexName string) {
	t.Helper()
	field := requireAgentContextField(t, modelSchema, fieldName)
	for _, setting := range strings.Split(field.Tag.Get("gorm"), ";") {
		if strings.HasPrefix(setting, "uniqueIndex:"+indexName) {
			return
		}
	}
	t.Fatalf("字段 %q 未参与唯一索引 %q：%q", fieldName, indexName, field.Tag.Get("gorm"))
}

func TestAgentContextModelTableNames(t *testing.T) {
	tests := []struct {
		model     any
		tableName string
	}{
		{model: &ConversationMembership{}, tableName: "conversation_memberships"},
		{model: &ConversationMessageDisposition{}, tableName: "conversation_message_dispositions"},
		{model: &MessageTombstone{}, tableName: "message_tombstones"},
	}
	for _, tt := range tests {
		modelSchema := parseAgentContextModel(t, tt.model)
		if modelSchema.Table != tt.tableName {
			t.Fatalf("表名错误：got %q, want %q", modelSchema.Table, tt.tableName)
		}
	}
}

func TestConversationMembershipSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &ConversationMembership{})
	for _, name := range []string{"tenant_id", "platform", "channel_account_id", "group_id", "member_id", "evidence_source", "evidence_ref"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	for _, name := range []string{"joined_at", "left_at"} {
		field := requireAgentContextField(t, modelSchema, name)
		if field.NotNull {
			t.Fatalf("字段 %q 必须允许为空以表达未知/在群状态", name)
		}
	}
	field := requireAgentContextField(t, modelSchema, "display_name")
	if field.Unique {
		t.Fatalf("display_name 只能作为展示快照，不能参与身份唯一约束")
	}
}

func TestConversationMessageDispositionSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &ConversationMessageDisposition{})
	status := requireAgentContextField(t, modelSchema, "status")
	if status.DefaultValue != ConversationDispositionPending {
		t.Fatalf("status 默认值错误：got %q, want %q", status.DefaultValue, ConversationDispositionPending)
	}
	version := requireAgentContextField(t, modelSchema, "version")
	if version.DefaultValue != "1" {
		t.Fatalf("version 默认值错误：got %q, want 1", version.DefaultValue)
	}
	for _, name := range []string{"tenant_id", "platform", "channel_account_id", "conversation_id", "message_id", "message_cursor", "bus_llm_session_id_blind"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	for _, name := range []string{"tenant_id", "platform", "channel_account_id", "conversation_id", "message_id"} {
		requireAgentContextIndex(t, modelSchema, name, "uni_agent_conv_disposition")
	}
	late := requireAgentContextField(t, modelSchema, "late")
	if late.DefaultValue != "false" {
		t.Fatalf("late 默认值错误：got %q, want false", late.DefaultValue)
	}
}

func TestMessageTombstoneSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &MessageTombstone{})
	for _, name := range []string{"tenant_id", "platform", "channel_account_id", "conversation_id", "message_id", "tombstone_type", "effective_at", "source_cursor"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	for _, name := range []string{"tenant_id", "platform", "channel_account_id", "message_id", "tombstone_type"} {
		requireAgentContextIndex(t, modelSchema, name, "uni_agent_msg_tombstone")
	}
}

func TestAgentContextEnumValuesStable(t *testing.T) {
	statuses := []string{
		ConversationDispositionPending,
		ConversationDispositionProcessing,
		ConversationDispositionReplied,
		ConversationDispositionSkipped,
		ConversationDispositionMerged,
	}
	seenStatus := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		if status == "" || seenStatus[status] {
			t.Fatalf("disposition 状态为空或重复：%q", status)
		}
		seenStatus[status] = true
	}

	tombstones := []string{
		MessageTombstoneEdited,
		MessageTombstoneDeleted,
		MessageTombstoneRecalled,
		MessageTombstoneRedacted,
	}
	seenTombstone := make(map[string]bool, len(tombstones))
	for _, kind := range tombstones {
		if kind == "" || seenTombstone[kind] {
			t.Fatalf("tombstone 类型为空或重复：%q", kind)
		}
		seenTombstone[kind] = true
	}
}
