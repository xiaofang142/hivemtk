package agent_runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func TestCheckpointContextIdentityValidation(t *testing.T) {
	payload := CustomerMessagePayload{
		MessageID: "hub:42", ChannelType: "wecom", AccountID: "acc-1",
		CustomerID: "c-1", SessionID: "wecom:acc-1:conv-1", Content: "你好",
		MessageType: "text", Timestamp: time.Unix(1700000000, 0),
	}
	identity := checkpointContextIdentity(payload)
	if identity.SnapshotID == "" || identity.MessageWatermark == "" || identity.ContextSchemaVersion == "" {
		t.Fatalf("checkpoint identity 字段不完整: %+v", identity)
	}
	if again := checkpointContextIdentity(payload); again != identity {
		t.Fatalf("checkpoint identity 必须确定性: %+v vs %+v", identity, again)
	}
	if err := ValidateCheckpointContext(checkpointState{
		ContextSchemaVersion: identity.ContextSchemaVersion,
		SnapshotID:           identity.SnapshotID,
		MessageWatermark:     identity.MessageWatermark,
	}, identity); err != nil {
		t.Fatalf("相同 identity 应通过: %v", err)
	}
	stale := []checkpointState{
		{ContextSchemaVersion: "old-schema", SnapshotID: identity.SnapshotID, MessageWatermark: identity.MessageWatermark},
		{ContextSchemaVersion: identity.ContextSchemaVersion, SnapshotID: "stale-snapshot", MessageWatermark: identity.MessageWatermark},
		{ContextSchemaVersion: identity.ContextSchemaVersion, SnapshotID: identity.SnapshotID, MessageWatermark: "hub:7"},
	}
	for i, st := range stale {
		if err := ValidateCheckpointContext(st, identity); err == nil {
			t.Fatalf("陈旧 checkpoint %d 必须拒绝续跑", i)
		}
	}
}

func TestLocalLLMClientChatWithContextBindsExecutionHeaders(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = map[string]string{
			"scenario":    r.Header.Get("X-LLM-Execution-Scenario"),
			"session":     r.Header.Get("X-LLM-Session-Ref"),
			"idempotency": r.Header.Get("X-LLM-Idempotency-Key"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	client := NewLocalLLMClient(LocalLLMConfig{BaseURL: srv.URL, DefaultModel: "fake-model"})
	callCtx := RuntimeLLMCallContext{
		ExecutionScenarioID: "agent_runtime",
		SessionRef:          "llmsess_abc",
		IdempotencyKey:      "idem-1",
	}
	content, err := client.ChatWithContext(context.Background(), []model.AssetBundleMessage{{Role: "user", Content: "hi"}}, "", 0.2, callCtx)
	if err != nil {
		t.Fatalf("ChatWithContext 失败: %v", err)
	}
	if content != "ok" {
		t.Fatalf("响应内容错误: %q", content)
	}
	if got["scenario"] != "agent_runtime" || got["session"] != "llmsess_abc" || !strings.HasPrefix(got["idempotency"], "idem-1") {
		t.Fatalf("上下文头未正确绑定: %+v", got)
	}
}
