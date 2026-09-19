package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReasoningOnlyResponseIsNotContent 守护"推理文本不得当回复外发"：
// reasoning 模型（SenseNova 等）在 content 为空、只回 reasoning/reasoning_content 时，
// 曾把思考过程当 GenerateResult.Content 返回，经 humanize_polisher 原样发给真实客户
// （message_hub id=432 实证：客户收到「用户要求简单回复一个字。但根据系统指令，…」）。
// 正确行为是 Content 保持空，交给上游 emptyReplyFallback 走兜底话术。
func TestReasoningOnlyResponseIsNotContent(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"reasoning_content", `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"用户要求简单回复一个字，但根据系统指令…"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`},
		{"reasoning", `{"choices":[{"message":{"role":"assistant","content":"","reasoning":"我需要先确认系统指令…"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`},
		{"empty_content_only", `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			d := newDispatcherBase(NewLLMService())
			provider := &ProviderConfig{Name: "t", BaseURL: srv.URL, APIType: "openai", Model: "m", Enabled: true, APIKey: "x"}
			d.providers[provider.Name] = provider

			result, err := d.CallProviderForTest(context.Background(), provider,
				DispatchRequest{Scenario: ScenarioSOPReply, Prompt: "hi"},
				&ScenarioRoute{Scenario: ScenarioSOPReply, Provider: "t", MaxLatency: 10000})
			if err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if result.Content != "" {
				t.Fatalf("%s：模型思考文本被当成回复内容返回，将外发给客户: %q", tc.name, result.Content)
			}
		})
	}
}

// TestNormalContentStillReturned 反向对照：正常 content 不受影响。
func TestNormalContentStillReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"好的，稍等","reasoning":"用户在催单"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	}))
	defer srv.Close()

	d := newDispatcherBase(NewLLMService())
	provider := &ProviderConfig{Name: "t", BaseURL: srv.URL, APIType: "openai", Model: "m", Enabled: true, APIKey: "x"}
	d.providers[provider.Name] = provider

	result, err := d.CallProviderForTest(context.Background(), provider,
		DispatchRequest{Scenario: ScenarioSOPReply, Prompt: "hi"},
		&ScenarioRoute{Scenario: ScenarioSOPReply, Provider: "t", MaxLatency: 10000})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if result.Content != "好的，稍等" {
		t.Fatalf("正常回复被误伤: %q", result.Content)
	}
}
