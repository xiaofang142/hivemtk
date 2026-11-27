package llm

// 能力枚举值；未显式登记的模型一律保守降级，不伪造厂商能力。
const (
	CacheModeNone                 = "none"
	CacheModeImplicit             = "implicit"
	CacheModeExplicit             = "explicit"
	CacheModeProviderSessionCache = "provider_session_cache"

	SessionModeNone              = "none"
	SessionModeNativeResponse    = "native_response"
	SessionModeNativeMessage     = "native_message"
	SessionModeNativeInteraction = "native_interaction"
	SessionModeProviderCache     = "provider_session_cache"

	StoreModeStateless = "stateless"
	StoreModeNative    = "native"
)

type knownProviderCapabilities struct {
	contextWindow          int
	maxInputTokens         int
	protocolInputLimit     int
	maxOutputTokens        int
	reasoningReserveTokens int
	cacheMode              string
	sessionMode            string
	nativeSessionAllowed   bool
}

var knownProviderModels = map[string]knownProviderCapabilities{
	"gpt-4o":          {contextWindow: 128000, maxInputTokens: 128000, maxOutputTokens: 16384, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeResponse, nativeSessionAllowed: true},
	"gpt-5.6-luna":    {contextWindow: 1050000, maxInputTokens: 922000, maxOutputTokens: 128000, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeResponse, nativeSessionAllowed: true},
	"deepseek-v4-pro": {contextWindow: 1048576, maxInputTokens: 1048576, maxOutputTokens: 393216, cacheMode: CacheModeImplicit, sessionMode: SessionModeNone},
	"qwen3.8-max":     {contextWindow: 1000000, maxInputTokens: 991808, protocolInputLimit: 800000, maxOutputTokens: 131072, reasoningReserveTokens: 4096, cacheMode: CacheModeExplicit, sessionMode: SessionModeNativeResponse, nativeSessionAllowed: true},
	"gemini-4-carbon": {contextWindow: 1048576, maxInputTokens: 1048576, maxOutputTokens: 65536, cacheMode: CacheModeImplicit, sessionMode: SessionModeNativeInteraction, nativeSessionAllowed: true},
	"claude-opus-5.5": {contextWindow: 1000000, maxInputTokens: 1000000, maxOutputTokens: 128000, cacheMode: CacheModeExplicit, sessionMode: SessionModeNativeMessage, nativeSessionAllowed: true},
}

// ProviderCapabilities 是解析后的运行时能力；未知模型不使用厂商名推断。
type ProviderCapabilities struct {
	ContextWindow          int
	MaxInputTokens         int
	ProtocolInputLimit     int
	MaxOutputTokens        int
	ReasoningReserveTokens int
	OutputSemantics        string
	CacheMode              string
	CacheMinTokens         int
	CacheTTLSeconds        int
	SessionMode            string
	StoreMode              string
	NativeSessionAllowed   bool
	SupportsIdempotencyKey bool
	MaxRequestBytes        int64
	RequestTimeoutSeconds  int
	RouteMode              string
	AllowedRegions         []string
}

// ResolveProviderCapabilities 从显式配置或已知模型注册表解析能力；显式配置优先。
func ResolveProviderCapabilities(cfg ProviderConfig) ProviderCapabilities {
	defaults := knownProviderModels[cfg.Model]
	explicit := cfg.ContextWindow > 0 || cfg.MaxInputTokens > 0 || cfg.ProtocolInputLimit > 0 ||
		cfg.MaxOutputTokens > 0 || cfg.ReasoningReserveTokens > 0 || cfg.CacheMode != "" ||
		cfg.SessionMode != "" || cfg.StoreMode != "" || len(cfg.AllowedRegions) > 0

	caps := ProviderCapabilities{
		ContextWindow:          8192,
		MaxInputTokens:         8192,
		MaxOutputTokens:        2048,
		OutputSemantics:        "response_only",
		CacheMode:              CacheModeNone,
		SessionMode:            SessionModeNone,
		StoreMode:              StoreModeStateless,
		RouteMode:              RouteModeDirect,
		NativeSessionAllowed:   false,
		SupportsIdempotencyKey: cfg.SupportsIdempotencyKey,
		MaxRequestBytes:        cfg.MaxRequestBytes,
		RequestTimeoutSeconds:  cfg.RequestTimeoutSeconds,
		AllowedRegions:         append([]string{}, cfg.AllowedRegions...),
	}
	if !explicit && defaults.contextWindow > 0 {
		caps.ContextWindow = defaults.contextWindow
		caps.MaxInputTokens = defaults.maxInputTokens
		caps.ProtocolInputLimit = defaults.protocolInputLimit
		caps.MaxOutputTokens = defaults.maxOutputTokens
		caps.ReasoningReserveTokens = defaults.reasoningReserveTokens
		caps.CacheMode = defaults.cacheMode
		caps.SessionMode = defaults.sessionMode
		caps.NativeSessionAllowed = defaults.nativeSessionAllowed
	}
	if cfg.ContextWindow > 0 {
		caps.ContextWindow = cfg.ContextWindow
	}
	if cfg.MaxInputTokens > 0 {
		caps.MaxInputTokens = cfg.MaxInputTokens
	}
	if cfg.ProtocolInputLimit > 0 {
		caps.ProtocolInputLimit = cfg.ProtocolInputLimit
	}
	if cfg.MaxOutputTokens > 0 {
		caps.MaxOutputTokens = cfg.MaxOutputTokens
	}
	if cfg.ReasoningReserveTokens > 0 {
		caps.ReasoningReserveTokens = cfg.ReasoningReserveTokens
	}
	if cfg.OutputSemantics != "" {
		caps.OutputSemantics = cfg.OutputSemantics
	}
	if cfg.CacheMode != "" {
		caps.CacheMode = cfg.CacheMode
	}
	if cfg.CacheMinTokens > 0 {
		caps.CacheMinTokens = cfg.CacheMinTokens
	}
	if cfg.CacheTTLSeconds > 0 {
		caps.CacheTTLSeconds = cfg.CacheTTLSeconds
	}
	if cfg.MaxRequestBytes > 0 {
		caps.MaxRequestBytes = cfg.MaxRequestBytes
	}
	if cfg.RequestTimeoutSeconds > 0 {
		caps.RequestTimeoutSeconds = cfg.RequestTimeoutSeconds
	}
	if cfg.SessionMode != "" {
		caps.SessionMode = cfg.SessionMode
	}
	if cfg.StoreMode != "" {
		caps.StoreMode = cfg.StoreMode
	}
	if explicit {
		caps.NativeSessionAllowed = cfg.NativeSessionAllowed
	}
	if cfg.RouteMode != "" {
		caps.RouteMode = cfg.RouteMode
	}
	return caps
}
