package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
)

func resetChatOverrideSeams(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetLLMOverrideProvider(nil)
		SetLLMOverrideWriter(nil)
	})
}

func TestChatCompletionsEndpointKeepsVersionedOpenAICompatibleBase(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		apiType string
		model   string
		want    string
	}{
		{
			name: "plain_openai_base",
			base: "https://api.openai.com",
			want: "https://api.openai.com/v1/chat/completions",
		},
		{
			name: "v1_base_not_duplicated",
			base: "https://api.example.com/v1",
			want: "https://api.example.com/v1/chat/completions",
		},
		{
			name: "gemini_openai_compatible",
			base: "https://generativelanguage.googleapis.com/v1beta/openai/",
			want: "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		},
		{
			name:    "azure",
			base:    "https://example.openai.azure.com/",
			apiType: "azure",
			model:   "dep",
			want:    "https://example.openai.azure.com/openai/deployments/dep/chat/completions?api-version=2024-02-01",
		},
		{
			name:    "anthropic",
			base:    "https://api.anthropic.com/v1",
			apiType: "anthropic",
			want:    "https://api.anthropic.com/v1/messages",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatCompletionsEndpoint(tt.base, tt.apiType, tt.model); got != tt.want {
				t.Fatalf("endpoint=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestDispatcherDispatchUsesGlobalLLMOverride(t *testing.T) {
	resetChatOverrideSeams(t)

	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer srv.Close()

	raw := fmt.Sprintf(`{"enabled":true,"base_url":%q,"api_key":"k","model":"gemini-flash-lite-latest","api_type":"openai"}`, srv.URL+"/v1beta/openai")
	SetLLMOverrideProvider(func(context.Context) string { return raw })

	cfg := config.AppConfig{}
	cfg.Inference.LLM.BaseURL = "http://127.0.0.1:1"
	cfg.Inference.LLM.Model = "dead-local-model"
	cfg.Inference.LLM.APIKey = "dead-local-key"
	d := NewDispatcherFromConfig(cfg)

	result, err := d.Dispatch(context.Background(), DispatchRequest{
		Scenario:  ScenarioLowCost,
		Prompt:    "hello",
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("Dispatch 失败：%v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("Content=%q, want ok", result.Content)
	}
	if gotPath != "/v1beta/openai/chat/completions" {
		t.Fatalf("path=%q, want /v1beta/openai/chat/completions", gotPath)
	}
	if gotBody["model"] != "gemini-flash-lite-latest" {
		t.Fatalf("model=%v, want gemini-flash-lite-latest; body=%v", gotBody["model"], gotBody)
	}
}

func TestGlobalOverrideProviderCopyIsIsolated(t *testing.T) {
	resetChatOverrideSeams(t)
	SetLLMOverrideProvider(func(context.Context) string {
		return `{"enabled":true,"base_url":"https://gemini.invalid/v1beta/openai","api_key":"k","model":"gemini-flash-lite-latest","api_type":"openai"}`
	})

	registered := &ProviderConfig{
		Name: "default", BaseURL: "https://old.invalid", Model: "old",
		Source: LLMProviderSourcePlatform, RouteMode: RouteModeDirect, Enabled: true,
	}
	applied := providerWithGlobalOverride(registered)
	if applied == registered {
		t.Fatal("全局覆盖必须返回副本，不能改注册表")
	}
	if applied.Source != "self" || applied.RouteMode != RouteModeDirect {
		t.Fatalf("覆盖后 Source=%q RouteMode=%q，期望 self/direct", applied.Source, applied.RouteMode)
	}
	if !strings.Contains(applied.BaseURL, "gemini.invalid") || applied.Model != "gemini-flash-lite-latest" {
		t.Fatalf("覆盖未生效：%+v", applied)
	}
	if registered.Model != "old" || registered.Source != LLMProviderSourcePlatform {
		t.Fatalf("注册表被污染：%+v", registered)
	}
}
