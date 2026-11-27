package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	llmpkg "hivemtk-user/internal/aiagent/llm"
)

type fakeConfigParamPlatform struct {
	getValue  string
	updates   []platformOverrideUpdate
	updateErr error
}

type platformOverrideUpdate struct {
	group    string
	key      string
	newValue string
	actorID  uint
}

func (f *fakeConfigParamPlatform) GetString(_ context.Context, _group, _key, _fallback string) string {
	return f.getValue
}

func (f *fakeConfigParamPlatform) UpdatePlatformDefault(_ context.Context, group, key, newValue string, actorID uint) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updates = append(f.updates, platformOverrideUpdate{
		group: group, key: key, newValue: newValue, actorID: actorID,
	})
	f.getValue = newValue
	return nil
}

func resetGlobalOverrideProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		llmpkg.SetLLMOverrideProvider(nil)
		llmpkg.SetLLMOverrideWriter(nil)
		llmpkg.SetEmbeddingOverrideProvider(nil)
		llmpkg.SetEmbeddingOverrideWriter(nil)
		llmpkg.SetRerankOverrideProvider(nil)
		llmpkg.SetRerankOverrideWriter(nil)
	})
}

func TestWireLLMConfigParamsWritesPlatformDefault(t *testing.T) {
	resetGlobalOverrideProviders(t)
	fake := &fakeConfigParamPlatform{
		getValue: `{"enabled":false,"api_key":"","base_url":"","model":""}`,
	}

	wireLLMConfigParamsWith(fake)

	const actorID = uint(4242)
	if err := llmpkg.SetGlobalLLMOverride(&llmpkg.GlobalLLMOverride{
		Enabled: true,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		Model:   "gemini-flash-lite-latest",
		APIType: "openai",
	}, actorID); err != nil {
		t.Fatalf("SetGlobalLLMOverride 失败：%v", err)
	}

	if len(fake.updates) != 1 {
		t.Fatalf("平台默认写次数 = %d，期望 1", len(fake.updates))
	}
	got := fake.updates[0]
	if got.group != "llm" || got.key != "global_override" {
		t.Fatalf("写入坐标 = %s.%s，期望 llm.global_override", got.group, got.key)
	}
	if got.actorID != actorID {
		t.Errorf("审计操作者 = %d，期望 %d", got.actorID, actorID)
	}
	if !strings.Contains(got.newValue, "gemini-flash-lite-latest") {
		t.Errorf("写入值不含目标模型：%q", got.newValue)
	}
	readBack := llmpkg.GetGlobalLLMOverride()
	if readBack == nil || readBack.Model != "gemini-flash-lite-latest" {
		t.Fatalf("写完立即读回失败：got %+v", readBack)
	}
}

func TestWireEmbeddingConfigParamsWritesPlatformDefault(t *testing.T) {
	resetGlobalOverrideProviders(t)
	fake := &fakeConfigParamPlatform{
		getValue: `{"enabled":false,"api_key":"","base_url":"","model":""}`,
	}

	wireEmbeddingConfigParamsWith(fake)

	const actorID = uint(4243)
	if err := llmpkg.SetGlobalEmbeddingOverride(&llmpkg.GlobalEmbeddingOverride{
		Enabled:  true,
		BaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai/",
		Model:    "gemini-embedding-2",
		Protocol: "openai",
	}, actorID); err != nil {
		t.Fatalf("SetGlobalEmbeddingOverride 失败：%v", err)
	}

	if len(fake.updates) != 1 {
		t.Fatalf("平台默认写次数 = %d，期望 1", len(fake.updates))
	}
	got := fake.updates[0]
	if got.group != "embedding" || got.key != "global_override" {
		t.Fatalf("写入坐标 = %s.%s，期望 embedding.global_override", got.group, got.key)
	}
	if got.actorID != actorID {
		t.Errorf("审计操作者 = %d，期望 %d", got.actorID, actorID)
	}
	if !strings.Contains(got.newValue, "gemini-embedding-2") {
		t.Errorf("写入值不含目标模型：%q", got.newValue)
	}
}

func TestWireRerankConfigParamsWritesPlatformDefault(t *testing.T) {
	resetGlobalOverrideProviders(t)
	fake := &fakeConfigParamPlatform{
		getValue: `{"enabled":false,"api_key":"","base_url":"","model":""}`,
	}

	wireRerankConfigParamsWith(fake)

	const actorID = uint(4244)
	if err := llmpkg.SetGlobalRerankOverride(&llmpkg.GlobalRerankOverride{
		Enabled:  true,
		BaseURL:  "https://openrouter.ai/api",
		Model:    "nvidia/nemotron-3.5-rerank-8b:free",
		Protocol: "openai",
	}, actorID); err != nil {
		t.Fatalf("SetGlobalRerankOverride 失败：%v", err)
	}

	if len(fake.updates) != 1 {
		t.Fatalf("平台默认写次数 = %d，期望 1", len(fake.updates))
	}
	got := fake.updates[0]
	if got.group != "rerank" || got.key != "global_override" {
		t.Fatalf("写入坐标 = %s.%s，期望 rerank.global_override", got.group, got.key)
	}
	if got.actorID != actorID {
		t.Errorf("审计操作者 = %d，期望 %d", got.actorID, actorID)
	}
	if !strings.Contains(got.newValue, "nvidia/nemotron-3.5-rerank-8b:free") {
		t.Errorf("写入值不含目标模型：%q", got.newValue)
	}
	readBack := llmpkg.GetGlobalRerankOverride()
	if readBack == nil || readBack.Model != "nvidia/nemotron-3.5-rerank-8b:free" {
		t.Fatalf("写完立即读回失败：got %+v", readBack)
	}
}

func TestGlobalOverrideWriterPropagatesError(t *testing.T) {
	resetGlobalOverrideProviders(t)
	wantErr := errors.New("platform default write failed")
	fake := &fakeConfigParamPlatform{
		getValue:  `{"enabled":false}`,
		updateErr: wantErr,
	}

	wireLLMConfigParamsWith(fake)
	err := llmpkg.SetGlobalLLMOverride(&llmpkg.GlobalLLMOverride{
		Enabled: true, BaseURL: "https://example.invalid/v1", Model: "m-test",
	}, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("writer 错误未透传：got %v, want %v", err, wantErr)
	}
}
