package llm

import "testing"

func TestResolveProviderCapabilitiesKnownModels(t *testing.T) {
	tests := []struct {
		model         string
		window        int
		maxInput      int
		protocolLimit int
		maxOutput     int
		cacheMode     string
		sessionMode   string
		nativeAllowed bool
	}{
		{model: "gpt-4o", window: 128000, maxInput: 128000, maxOutput: 16384, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeResponse, nativeAllowed: true},
		{model: "gpt-5.6-luna", window: 1050000, maxInput: 922000, maxOutput: 128000, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeResponse, nativeAllowed: true},
		{model: "deepseek-v4-pro", window: 1048576, maxInput: 1048576, maxOutput: 393216, cacheMode: CacheModeImplicit, sessionMode: SessionModeNone, nativeAllowed: false},
		{model: "qwen3.8-max", window: 1000000, maxInput: 991808, protocolLimit: 800000, maxOutput: 131072, cacheMode: CacheModeExplicit, sessionMode: SessionModeNativeResponse, nativeAllowed: true},
		{model: "gemini-4-carbon", window: 1048576, maxInput: 1048576, maxOutput: 65536, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeInteraction, nativeAllowed: true},
		{model: "claude-opus-5.5", window: 1000000, maxInput: 1000000, maxOutput: 128000, cacheMode: CacheModeExplicit, sessionMode: SessionModeNativeMessage, nativeAllowed: true},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := ResolveProviderCapabilities(ProviderConfig{Name: "p", Model: tt.model, APIType: "openai"})
			if got.ContextWindow != tt.window || got.MaxInputTokens != tt.maxInput || got.MaxOutputTokens != tt.maxOutput {
				t.Fatalf("能力上限错误：%+v", got)
			}
			if got.ProtocolInputLimit != tt.protocolLimit {
				t.Fatalf("协议输入上限错误：got %d want %d", got.ProtocolInputLimit, tt.protocolLimit)
			}
			if got.CacheMode != tt.cacheMode || got.SessionMode != tt.sessionMode || got.NativeSessionAllowed != tt.nativeAllowed {
				t.Fatalf("缓存/会话能力错误：%+v", got)
			}
		})
	}
}

func TestResolveProviderCapabilitiesUnknownFallback(t *testing.T) {
	got := ResolveProviderCapabilities(ProviderConfig{Name: "p", Model: "unknown-model", APIType: "openai"})
	if got.ContextWindow != 8192 || got.MaxOutputTokens != 2048 {
		t.Fatalf("未知模型必须保守回退：%+v", got)
	}
	if got.CacheMode != CacheModeNone || got.SessionMode != SessionModeNone || got.NativeSessionAllowed || got.StoreMode != StoreModeStateless {
		t.Fatalf("未知能力不得伪造缓存/native 会话：%+v", got)
	}
	if got.RouteMode != RouteModeDirect {
		t.Fatalf("默认路由必须直连：%q", got.RouteMode)
	}
}

func TestResolveProviderCapabilitiesConfigOverridesAndRegion(t *testing.T) {
	cfg := ProviderConfig{
		Name: "custom", Model: "gpt-4o", APIType: "openai",
		ContextWindow: 200000, MaxInputTokens: 150000, MaxOutputTokens: 8192,
		CacheMode: CacheModeNone, SessionMode: SessionModeNone, NativeSessionAllowed: false,
		AllowedRegions: []string{"cn-north-1"},
	}
	got := ResolveProviderCapabilities(cfg)
	if got.ContextWindow != 200000 || got.MaxInputTokens != 150000 || got.MaxOutputTokens != 8192 {
		t.Fatalf("显式配置必须覆盖模型默认：%+v", got)
	}
	if got.CacheMode != CacheModeNone || got.SessionMode != SessionModeNone || got.NativeSessionAllowed {
		t.Fatalf("显式关闭不得被模型默认打开：%+v", got)
	}
	if len(got.AllowedRegions) != 1 || got.AllowedRegions[0] != "cn-north-1" {
		t.Fatalf("区域白名单丢失：%+v", got.AllowedRegions)
	}
}

func TestProviderCapabilityBudgetFormula(t *testing.T) {
	caps := ResolveProviderCapabilities(ProviderConfig{Name: "p", Model: "qwen3.8-max", APIType: "openai"})
	budget, err := NewContextBudget(ProviderConfig{
		ContextWindow: caps.ContextWindow, MaxInputTokens: caps.MaxInputTokens,
		ProtocolInputLimit: caps.ProtocolInputLimit, MaxOutputTokens: caps.MaxOutputTokens,
		ReasoningReserveTokens: caps.ReasoningReserveTokens,
	}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	want := 1000000 - 4096 - caps.ReasoningReserveTokens - 512
	if budget.EffectiveInput != want && budget.EffectiveInput != 800000 {
		t.Fatalf("预算夹取错误：got %d want %d or protocol limit 800000", budget.EffectiveInput, want)
	}
	if budget.ClampOutput(999999) > caps.MaxOutputTokens {
		t.Fatalf("输出未被模型上限夹取：%d", budget.ClampOutput(999999))
	}
}

func TestResolveProviderCapabilitiesTransportFields(t *testing.T) {
	cfg := ProviderConfig{
		Name: "custom", Model: "gpt-4o", APIType: "openai",
		OutputSemantics: "response_plus_cot", CacheMinTokens: 1024, CacheTTLSeconds: 1800,
		MaxRequestBytes: 8 << 20, RequestTimeoutSeconds: 90, SupportsIdempotencyKey: true,
	}
	got := ResolveProviderCapabilities(cfg)
	if got.OutputSemantics != "response_plus_cot" || got.CacheMinTokens != 1024 || got.CacheTTLSeconds != 1800 {
		t.Fatalf("输出/缓存能力未映射：%+v", got)
	}
	if got.MaxRequestBytes != 8<<20 || got.RequestTimeoutSeconds != 90 || !got.SupportsIdempotencyKey {
		t.Fatalf("传输能力未映射：%+v", got)
	}
}
