package model

import "testing"

func TestConversationContextVersionSchema(t *testing.T) {
	modelSchema := parseAgentContextModel(t, &ConversationContext{})
	for _, name := range []string{"version", "source_cursor", "source_cursor_hash", "summary_version"} {
		field := requireAgentContextField(t, modelSchema, name)
		if !field.NotNull {
			t.Fatalf("字段 %q 必须非空", name)
		}
	}
	if got := requireAgentContextField(t, modelSchema, "version").DefaultValue; got != "1" {
		t.Fatalf("version 默认值错误：got %q, want 1", got)
	}
	if got := requireAgentContextField(t, modelSchema, "summary_version").DefaultValue; got != "1" {
		t.Fatalf("summary_version 默认值错误：got %q, want 1", got)
	}
	if got := requireAgentContextField(t, modelSchema, "source_cursor").DefaultValue; got != "" {
		t.Fatalf("source_cursor 默认值应为空：got %q", got)
	}
	if got := requireAgentContextField(t, modelSchema, "source_cursor_hash").DefaultValue; got != "" {
		t.Fatalf("source_cursor_hash 默认值应为空：got %q", got)
	}
}
