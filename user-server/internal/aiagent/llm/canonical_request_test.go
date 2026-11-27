package llm

import (
	"encoding/hex"
	"testing"
)

func TestCanonicalPromptRequestIsStable(t *testing.T) {
	base := buildCanonicalRequest("gpt-test", "system-v1")
	addTool := func(r *CanonicalPromptRequest, name string) {
		r.Tools = append(r.Tools, CanonicalTool{Name: name, SchemaJSON: `{"type":"object","properties":{"q":{"type":"string"}}}`})
	}
	addTool(base, "one")
	addTool(base, "two")
	base.StableMessages = append(base.StableMessages, CanonicalMessage{Role: "developer", Content: "stable policy"})
	base.DynamicMessages = append(base.DynamicMessages, CanonicalMessage{Role: "user", Content: "current question"})

	other := buildCanonicalRequest("gpt-test", "system-v1")
	addTool(other, "one")
	addTool(other, "two")
	other.StableMessages = append(other.StableMessages, CanonicalMessage{Role: "developer", Content: "stable policy"})
	other.DynamicMessages = append(other.DynamicMessages, CanonicalMessage{Role: "user", Content: "different question"})

	if base.PrefixHash() != other.PrefixHash() {
		t.Fatalf("动态尾消息变化不应改变稳定前缀 hash：%q vs %q", base.PrefixHash(), other.PrefixHash())
	}
	if base.FullRequestHash() == other.FullRequestHash() {
		t.Fatal("完整请求 hash 必须覆盖动态消息")
	}
}

func TestCanonicalPromptRequestReflectsSilentPrefixChanges(t *testing.T) {
	base := buildCanonicalRequest("gpt-test", "system-v1")

	systemChanged := buildCanonicalRequest("gpt-test", "system-v2")
	if base.PrefixHash() == systemChanged.PrefixHash() {
		t.Fatal("system 提示词变化必须改变前缀 hash")
	}

	toolOrder := buildCanonicalRequest("gpt-test", "system-v1")
	toolOrder.Tools = append(toolOrder.Tools, CanonicalTool{Name: "a"}, CanonicalTool{Name: "b"})
	base.Tools = append(base.Tools, CanonicalTool{Name: "b"}, CanonicalTool{Name: "a"})
	if base.PrefixHash() == toolOrder.PrefixHash() {
		t.Fatal("工具顺序变化必须改变前缀 hash")
	}

	modelChanged := buildCanonicalRequest("gpt-test-v2", "system-v1")
	if base.PrefixHash() == modelChanged.PrefixHash() {
		t.Fatal("模型变化必须改变前缀 hash")
	}
}

func TestCanonicalPromptRequestHandlesMapOrder(t *testing.T) {
	a := buildCanonicalRequest("gpt-test", "system-v1")
	a.Metadata["a"] = "1"
	a.Metadata["z"] = "2"
	b := buildCanonicalRequest("gpt-test", "system-v1")
	b.Metadata["z"] = "2"
	b.Metadata["a"] = "1"
	if a.PrefixHash() != b.PrefixHash() {
		t.Fatal("map 插入顺序不得影响稳定前缀 hash")
	}
}

func TestCanonicalPromptRequestRejectsEmptyModel(t *testing.T) {
	r := buildCanonicalRequest("", "system-v1")
	if _, err := r.PrefixHashBytes(); err == nil {
		t.Fatal("空模型必须拒绝，避免生成无意义前缀 hash")
	}
}

func TestPromptCachePolicyFromCapabilities(t *testing.T) {
	tests := []struct {
		name      string
		caps      ProviderConfig
		wantMode  string
		wantCache bool
	}{
		{name: "none", caps: ProviderConfig{CacheMode: "none"}, wantMode: "none"},
		{name: "explicit", caps: ProviderConfig{CacheMode: "explicit", CacheMinTokens: 1024, CacheTTLSeconds: 1800}, wantMode: "explicit", wantCache: true},
		{name: "implicit", caps: ProviderConfig{CacheMode: "implicit", CacheMinTokens: 2048, CacheTTLSeconds: 300}, wantMode: "implicit", wantCache: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := PromptCachePolicyFromCapabilities(tt.caps)
			if policy.Mode != tt.wantMode {
				t.Fatalf("缓存模式错误：got %q, want %q", policy.Mode, tt.wantMode)
			}
			if policy.Enabled != tt.wantCache {
				t.Fatalf("Enabled 错误：got %v, want %v", policy.Enabled, tt.wantCache)
			}
			if tt.wantCache && (policy.MinTokens <= 0 || policy.TTLSeconds <= 0) {
				t.Fatalf("缓存策略缺少阈值/TTL：%+v", policy)
			}
		})
	}
}

func TestCanonicalPromptRequestPrefixHashFormat(t *testing.T) {
	r := buildCanonicalRequest("gpt-test", "system-v1")
	raw, err := r.PrefixHashBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 32 {
		t.Fatalf("prefix hash 必须为 SHA-256，实际 %d bytes", len(raw))
	}
	if got := hex.EncodeToString(raw); got != r.PrefixHash() {
		t.Fatalf("PrefixHash 与字节不一致：%q vs %q", got, r.PrefixHash())
	}
}

func buildCanonicalRequest(model, system string) *CanonicalPromptRequest {
	return &CanonicalPromptRequest{
		Model:        model,
		SystemPrompt: system,
		Metadata:     map[string]string{},
		Tools:        []CanonicalTool{},
	}
}
