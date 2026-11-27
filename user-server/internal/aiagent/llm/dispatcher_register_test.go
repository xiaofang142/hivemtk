package llm

import (
	"testing"

	"hivemtk-user/internal/config"
)

func TestRegisterLocalProviderCapabilities(t *testing.T) {
	d := newDispatcherBase(&LLMService{})
	d.registerLocalProvider(config.InferenceLLMConfig{
		Model:   "test-model",
		BaseURL: "http://127.0.0.1:11434/v1",
		Capabilities: config.InferenceProviderCapabilities{
			ContextWindow:          131072,
			MaxInputTokens:         120000,
			MaxOutputTokens:        8192,
			ReasoningReserveTokens: 1024,
			OutputSemantics:        "response_only",
			CacheMode:              "automatic_prefix",
			CacheMinTokens:         1024,
			CacheTTLSeconds:        1800,
			SessionMode:            "native_response",
			NativeSessionAllowed:   true,
			StoreMode:              "stateless",
			AllowedRegions:         []string{"cn-north-1"},
			SupportsIdempotencyKey: true,
			MaxRequestBytes:        8 << 20,
			RequestTimeoutSeconds:  90,
			RouteMode:              "direct",
			ReliabilityTier:        "primary",
		},
	})

	provider := d.providers["default"]
	if provider == nil {
		t.Fatal("default provider 未注册")
	}
	if provider.ContextWindow != 131072 || provider.MaxInputTokens != 120000 || provider.MaxOutputTokens != 8192 {
		t.Fatalf("上下文能力映射错误: context=%d input=%d output=%d", provider.ContextWindow, provider.MaxInputTokens, provider.MaxOutputTokens)
	}
	if provider.CacheMode != "automatic_prefix" || provider.SessionMode != "native_response" {
		t.Fatalf("缓存/会话能力映射错误: cache=%s session=%s", provider.CacheMode, provider.SessionMode)
	}
	if provider.RouteMode != "direct" || provider.StoreMode != "stateless" || provider.Protocol != "openai" {
		t.Fatalf("路由/存储/协议默认值错误: route=%s store=%s protocol=%s", provider.RouteMode, provider.StoreMode, provider.Protocol)
	}
}

func TestRegisterCloudProviderCapabilities(t *testing.T) {
	d := newDispatcherBase(&LLMService{})
	d.registerCloudProvidersFromConfig(config.InferenceLLMConfig{
		CloudProviders: []config.InferenceCloudProviderConfig{
			{
				Name:    "custom",
				APIKey:  "secret",
				BaseURL: "https://example.com/v1",
				APIType: "openai",
				Model:   "custom-model",
				Enabled: true,
				Capabilities: config.InferenceProviderCapabilities{
					ContextWindow:   262144,
					MaxOutputTokens: 16384,
					CacheMode:       "none",
					SessionMode:     "none",
					RouteMode:       "platform_proxy",
					StoreMode:       "stateless",
				},
			},
		},
	})

	provider := d.providers["custom"]
	if provider == nil {
		t.Fatal("custom provider 未注册")
	}
	if provider.ContextWindow != 262144 || provider.MaxOutputTokens != 16384 {
		t.Fatalf("云端 provider 能力映射错误: context=%d output=%d", provider.ContextWindow, provider.MaxOutputTokens)
	}
	if provider.RouteMode != "platform_proxy" {
		t.Fatalf("云端 provider 路由错误: %s", provider.RouteMode)
	}
}

func TestProviderCapabilityDefaults(t *testing.T) {
	d := newDispatcherBase(&LLMService{})
	d.registerLocalProvider(config.InferenceLLMConfig{
		Model:   "test-model",
		BaseURL: "http://localhost:11434/v1",
	})

	provider := d.providers["default"]
	if provider == nil {
		t.Fatal("default provider 未注册")
	}
	if provider.Protocol != "openai" || provider.RouteMode != "direct" {
		t.Fatalf("协议/路由默认值错误: protocol=%s route=%s", provider.Protocol, provider.RouteMode)
	}
	if provider.CacheMode != "none" || provider.SessionMode != "none" || provider.StoreMode != "stateless" {
		t.Fatalf("缓存/会话/存储默认值错误: cache=%s session=%s store=%s", provider.CacheMode, provider.SessionMode, provider.StoreMode)
	}
}
