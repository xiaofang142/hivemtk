package agent_runtime

import (
	"strings"
	"testing"
)

func TestStaleCheckpointContextForcesRebuild(t *testing.T) {
	payload := CustomerMessagePayload{
		MessageID: "hub:42", SessionID: "llmsess_root",
		ChannelType: "telegram", AccountID: "11", Content: "客户消息",
	}
	expected := checkpointContextIdentity(payload)
	fresh := checkpointState{
		ContextSchemaVersion: expected.ContextSchemaVersion,
		SnapshotID:           expected.SnapshotID,
		MessageWatermark:     expected.MessageWatermark,
		SessionRef:           expected.SessionRef,
	}
	if err := ValidateCheckpointContext(fresh, expected); err != nil {
		t.Fatalf("新格式一致 checkpoint 应有效: %v", err)
	}
	stale := fresh
	stale.MessageWatermark = "hub:41"
	err := ValidateCheckpointContext(stale, expected)
	if err == nil || !strings.Contains(err.Error(), "stale checkpoint context") {
		t.Fatalf("陈旧水位必须触发重建, got %v", err)
	}
	legacy := checkpointState{Version: 1, Fingerprint: "old"}
	if err := ValidateCheckpointContext(legacy, expected); err != nil {
		t.Fatalf("旧格式 checkpoint 保持源兼容: %v", err)
	}
}
