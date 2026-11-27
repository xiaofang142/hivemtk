package llm

import (
	"context"
	"testing"
)

func TestDispatchCallProviderUsesRouteModeAndContextBudget(t *testing.T) {
	d := newDispatcherBase(nil)

	invalidRoute := &ProviderConfig{Name: "p", APIKey: "k", Model: "m", RouteMode: "bogus"}
	if _, err := d.callProvider(context.Background(), invalidRoute, DispatchRequest{Prompt: "hi"}, nil); err == nil {
		t.Fatal("非法 RouteMode 必须在调用模型前 fail-closed")
	}

	tooSmall := &ProviderConfig{Name: "p", APIKey: "k", Model: "m", ContextWindow: 1024, MaxOutputTokens: 2048}
	if _, err := d.callProvider(context.Background(), tooSmall, DispatchRequest{Prompt: "hi"}, nil); err == nil {
		t.Fatal("上下文预算不足必须在调用模型前 fail-closed")
	}
}

func TestApplyNormalizedProviderUsageUsesProviderAdapter(t *testing.T) {
	raw := []byte(`{"id":"req-1","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":7},"cache_creation_input_tokens":3}}`)
	var resp chatResponse
	resp.Usage.PromptTokens = 10
	cfg := &LLMConfig{}
	applyNormalizedProviderUsage(&resp, raw, cfg)
	if resp.Usage.CachedInputTokens != 7 || resp.Usage.CacheCreationTokens != 3 {
		t.Fatalf("provider adapter 未归一 cached usage: %+v", resp.Usage)
	}
	if resp.ID != "req-1" || cfg.ProviderContinuationRef != "req-1" {
		t.Fatalf("provider adapter 未归一请求/续接 ID: id=%q cont=%q", resp.ID, cfg.ProviderContinuationRef)
	}
}

func TestShouldUsePlatformProxy(t *testing.T) {
	if !shouldUsePlatformProxy(&LLMConfig{Source: LLMProviderSourcePlatform}) {
		t.Fatal("Source=platform 必须走代理")
	}
	if !shouldUsePlatformProxy(&LLMConfig{RouteMode: RouteModeProxy, Source: "self"}) {
		t.Fatal("显式 RouteMode=proxy 必须走代理")
	}
	if shouldUsePlatformProxy(&LLMConfig{RouteMode: RouteModeDirect, Source: "self"}) {
		t.Fatal("Source=self + direct 不得走代理")
	}
}
