package llm

import (
	"context"
	"testing"
)

func newTestDispatcher() *Dispatcher {
	return &Dispatcher{
		providers:  make(map[string]*ProviderConfig),
		routes:     make(map[DispatchScenario]*ScenarioRoute),
		rpmCounter: make(map[string]*rpmBucket),
	}
}

func TestDispatchCacheSetGet(t *testing.T) {
	d := newTestDispatcher()
	ctx := context.Background()
	d.setCache(ctx, "k1", 10, "v1")
	if c, ok := d.getCache(ctx, "k1"); !ok || c != "v1" {
		t.Fatalf("expected v1, got %q ok=%v", c, ok)
	}
}

func TestDispatchCacheExpiry(t *testing.T) {
	d := newTestDispatcher()
	ctx := context.Background()
	d.setCache(ctx, "k1", 60, "v1")
	if v, ok := d.getCache(ctx, "k1"); !ok || v != "v1" {
		t.Fatal("expected cache hit for k1")
	}
	if _, ok := d.getCache(ctx, "missing"); ok {
		t.Fatal("expected miss for missing key")
	}
}

func TestDispatchCacheZeroTTLNoop(t *testing.T) {
	d := newTestDispatcher()
	ctx := context.Background()
	d.setCache(ctx, "k3", 0, "v3")
	if _, ok := d.getCache(ctx, "k3"); ok {
		t.Fatal("zero TTL should not store")
	}
}

func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 0 {
		t.Fatal("empty text should be 0 tokens")
	}
	if estimateTokens("hello") <= 0 {
		t.Fatal("ascii text should be > 0 tokens")
	}
	if estimateTokens("你好世界") <= 0 {
		t.Fatal("cjk text should be > 0 tokens")
	}
}

func TestCacheKeyStableAndDistinct(t *testing.T) {
	a := CacheKey(ScenarioSOPReply, "  prompt  ")
	b := CacheKey(ScenarioSOPReply, "prompt")
	if a != b {
		t.Fatal("trim should produce same key")
	}
	if CacheKey(ScenarioSOPReply, "x") == CacheKey(ScenarioIntentRecognize, "x") {
		t.Fatal("different scenarios should produce different keys")
	}
}

// TestCacheKeyWithSystem_DistinctBySystemPrompt 回归：system prompt 必须参与缓存 key。
//
// 背景：知识库召回内容注入在 system message（service.renderRAGReferenceBlock），
// 若 key 只覆盖 scenario+user prompt，同一句客户消息在不同知识库上下文下会命中同一条
// 缓存，把「按 A 知识库生成的回复」当成「B 知识库上下文」的结果返回。
func TestCacheKeyWithSystem_DistinctBySystemPrompt(t *testing.T) {
	const userPrompt = "你们的产品怎么收费？"

	a := CacheKeyWithSystem(ScenarioSOPReply, "【知识库参考】:\n1. 标准版 1999 元/年", userPrompt)
	b := CacheKeyWithSystem(ScenarioSOPReply, "【知识库参考】:\n1. 旗舰版 5999 元/年", userPrompt)
	if a == b {
		t.Fatal("不同知识库上下文（system prompt 不同）必须产生不同缓存 key，否则缓存跨知识上下文串味")
	}

	// 同一 system prompt + 同一 user prompt（含首尾空白）应稳定命中同一 key
	c := CacheKeyWithSystem(ScenarioSOPReply, "  persona  ", "  "+userPrompt+"  ")
	d := CacheKeyWithSystem(ScenarioSOPReply, "persona", userPrompt)
	if c != d {
		t.Fatal("trim 后应产生相同 key")
	}

	// 分隔符必须存在：否则 (sp="ab",p="c") 与 (sp="a",p="bc") 拼接后哈希碰撞
	if CacheKeyWithSystem(ScenarioSOPReply, "ab", "c") == CacheKeyWithSystem(ScenarioSOPReply, "a", "bc") {
		t.Fatal("system prompt 与 user prompt 之间必须有分隔符，避免边界歧义导致哈希碰撞")
	}

	// scenario 仍须参与 key
	if CacheKeyWithSystem(ScenarioSOPReply, "sp", "p") == CacheKeyWithSystem(ScenarioIntentRecognize, "sp", "p") {
		t.Fatal("different scenarios should produce different keys")
	}
}
