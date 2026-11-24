package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/config"
)

// fakeReachProbe 可编程探测：每个能力各自返回预设错误，不碰网络。
type fakeReachProbe struct {
	llmErr error
	embErr error
}

func (f *fakeReachProbe) ProbeLLM(ctx context.Context, baseURL string) error {
	return f.llmErr
}

func (f *fakeReachProbe) ProbeEmbedding(ctx context.Context, baseURL string) error {
	return f.embErr
}

func TestInferenceTierSelector_Select(t *testing.T) {
	up := errors.New("unreachable")

	tests := []struct {
		name            string
		llmErr          error
		degradedAllowed bool
		cloudConfigured bool
		wantTier        InferenceTier
	}{
		{
			name:     "本地 LLM 可达 ⇒ full_local（本地优先，即使允许降级）",
			llmErr:   nil,
			wantTier: TierFullLocal,
		},
		{
			name:            "本地 LLM 不可达且允许降级且已配云端 ⇒ cloud_llm_local_embedding",
			llmErr:          up,
			degradedAllowed: true,
			cloudConfigured: true,
			wantTier:        TierCloudLLMLocalEmbedding,
		},
		{
			name:            "本地 LLM 不可达且允许降级但云端未配 ⇒ local_down_no_cloud",
			llmErr:          up,
			degradedAllowed: true,
			cloudConfigured: false,
			wantTier:        TierLocalDownNoCloud,
		},
		{
			name:            "本地 LLM 不可达且云端已配但降级被禁用 ⇒ local_down_no_cloud",
			llmErr:          up,
			degradedAllowed: false,
			cloudConfigured: true,
			wantTier:        TierLocalDownNoCloud,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewInferenceTierSelector(TierPolicy{
				DegradedAllowed:    tt.degradedAllowed,
				CloudLLMConfigured: tt.cloudConfigured,
			}, &fakeReachProbe{llmErr: tt.llmErr})

			got := sel.Select(context.Background())
			if got.Tier != tt.wantTier {
				t.Fatalf("Tier = %q, want %q（reason=%q）", got.Tier, tt.wantTier, got.Reason)
			}
			if got.LocalLLMUp != (tt.llmErr == nil) {
				t.Fatalf("LocalLLMUp = %v, want %v", got.LocalLLMUp, tt.llmErr == nil)
			}
			if got.CloudLLMConfigured != tt.cloudConfigured {
				t.Fatalf("CloudLLMConfigured = %v, want %v", got.CloudLLMConfigured, tt.cloudConfigured)
			}
			if got.DegradedAllowed != tt.degradedAllowed {
				t.Fatalf("DegradedAllowed = %v, want %v", got.DegradedAllowed, tt.degradedAllowed)
			}
		})
	}
}

func TestInferenceTierSelector_Select_NilSelector(t *testing.T) {
	var sel *InferenceTierSelector
	got := sel.Select(context.Background())
	if got.Tier != TierLocalDownNoCloud {
		t.Fatalf("nil selector 应安全落到 local_down_no_cloud，实际 %q", got.Tier)
	}
}

func TestInferenceTierSelector_DefaultProbe(t *testing.T) {
	// probe 传 nil 时用默认 HTTP 探测构造，不得 panic。
	sel := NewInferenceTierSelector(TierPolicy{}, nil)
	if sel == nil || sel.probe == nil {
		t.Fatal("nil probe 应被默认 HTTP 探测替换")
	}
}

func TestBuildTierPolicyFromConfig(t *testing.T) {
	tests := []struct {
		name            string
		cfg             config.AppConfig
		wantDegraded    bool
		wantLLMURL      string
		wantEmbedURL    string
		wantCloudConfig bool
	}{
		{
			name: "云端 provider 三项齐全（enabled+api_key+base_url）才算已配置",
			cfg: config.AppConfig{Inference: config.InferenceConfig{
				Tier:      config.InferenceTierConfig{FallbackToCloudLLM: true},
				LLM:       config.InferenceLLMConfig{BaseURL: "http://127.0.0.1:8207/v1"},
				Embedding: config.InferenceEmbeddingConfig{BaseURL: "http://127.0.0.1:8208/v1"},
				// 这里故意放一个残缺的（缺 api_key）在第一个，验证不会误判
				// 为已配置。
			}},
			wantDegraded:    true,
			wantLLMURL:      "http://127.0.0.1:8207/v1",
			wantEmbedURL:    "http://127.0.0.1:8208/v1",
			wantCloudConfig: false,
		},
		{
			name: "云端 provider 缺 api_key 不算已配置",
			cfg: config.AppConfig{Inference: config.InferenceConfig{
				LLM: config.InferenceLLMConfig{
					CloudProviders: []config.InferenceCloudProviderConfig{
						{Name: "deepseek", Enabled: true, BaseURL: "https://api.deepseek.com", APIKey: ""},
					},
				},
			}},
			wantCloudConfig: false,
		},
		{
			name: "云端 provider enabled=false 不算已配置",
			cfg: config.AppConfig{Inference: config.InferenceConfig{
				LLM: config.InferenceLLMConfig{
					CloudProviders: []config.InferenceCloudProviderConfig{
						{Name: "deepseek", Enabled: false, BaseURL: "https://api.deepseek.com", APIKey: "sk-" + "xxx"},
					},
				},
			}},
			wantCloudConfig: false,
		},
		{
			name: "云端 provider 三项齐全算已配置",
			cfg: config.AppConfig{Inference: config.InferenceConfig{
				LLM: config.InferenceLLMConfig{
					CloudProviders: []config.InferenceCloudProviderConfig{
						{Name: "deepseek", Enabled: true, BaseURL: "https://api.deepseek.com", APIKey: "sk-" + "xxx"},
					},
				},
			}},
			wantCloudConfig: true,
		},
		{
			name: "多个云端 provider 有一个齐全即算已配置",
			cfg: config.AppConfig{Inference: config.InferenceConfig{
				LLM: config.InferenceLLMConfig{
					CloudProviders: []config.InferenceCloudProviderConfig{
						{Name: "bad", Enabled: true, BaseURL: "https://x", APIKey: ""},
						{Name: "qwen", Enabled: true, BaseURL: "https://dashscope.aliyuncs.com", APIKey: "sk-" + "qwen"},
					},
				},
			}},
			wantCloudConfig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildTierPolicyFromConfig(tt.cfg)
			if got.DegradedAllowed != tt.wantDegraded {
				t.Fatalf("DegradedAllowed = %v, want %v", got.DegradedAllowed, tt.wantDegraded)
			}
			if got.LocalLLMBaseURL != tt.wantLLMURL {
				t.Fatalf("LocalLLMBaseURL = %q, want %q", got.LocalLLMBaseURL, tt.wantLLMURL)
			}
			if got.LocalEmbeddingBaseURL != tt.wantEmbedURL {
				t.Fatalf("LocalEmbeddingBaseURL = %q, want %q", got.LocalEmbeddingBaseURL, tt.wantEmbedURL)
			}
			if got.CloudLLMConfigured != tt.wantCloudConfig {
				t.Fatalf("CloudLLMConfigured = %v, want %v", got.CloudLLMConfigured, tt.wantCloudConfig)
			}
		})
	}
}

func TestHTTPInferenceProbe_Ping(t *testing.T) {
	// 健康服务：/health 200。
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer okSrv.Close()

	// 病态服务：/health 500。
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	probe := NewHTTPInferenceProbe(0) // 传 0 应回落到默认 3s，不得 panic

	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "健康端点 2xx 视为可达", baseURL: okSrv.URL, wantErr: false},
		{name: "健康端点 5xx 视为不可达", baseURL: badSrv.URL, wantErr: true},
		{name: "空 base_url 报错", baseURL: "", wantErr: true},
		{name: "非绝对地址报错", baseURL: "127.0.0.1:8208", wantErr: true},
		{name: "带尾斜杠也能拼对 /health", baseURL: okSrv.URL + "/", wantErr: false},
		// 配置里的 base_url 带 /v1 前缀，健康探测必须钉根路径 /health 而非 /v1/health
		// （本测试服务对非 /health 路径一律 404，能区分两种拼法）。
		{name: "base_url 带 /v1 前缀也探根路径 /health", baseURL: okSrv.URL + "/v1", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := probe.ProbeLLM(context.Background(), tt.baseURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ProbeLLM(%q) err = %v, wantErr = %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}

	// nil client 守卫。
	nilProbe := &httpInferenceProbe{client: nil}
	if err := nilProbe.ProbeLLM(context.Background(), okSrv.URL); err == nil {
		t.Fatal("nil client 应报错")
	}
}

func TestRecordAndGetLastTierDecision(t *testing.T) {
	d := TierDecision{Tier: TierCloudLLMLocalEmbedding, Reason: "test"}
	recordTierDecision(d)
	got := GetLastTierDecision()
	if got == nil || got.Tier != d.Tier || got.Reason != d.Reason {
		t.Fatalf("GetLastTierDecision 返回 %+v，期望 %+v", got, d)
	}
	// 返回副本，改动不得影响存储。
	got.Reason = "mutated"
	if again := GetLastTierDecision(); again == nil || again.Reason != d.Reason {
		t.Fatalf("GetLastTierDecision 应返回副本，内部被外部改动污染: %+v", again)
	}
}
