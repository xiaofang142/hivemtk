package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
)

// 降级/兜底文案是发给客户看的，其中「…」形式的转人工暗号必须真的能被转人工关键词表识别：
// 客户照文案回复一个匹配器不认的词（例如裸「人工」，表里只有「转人工」「人工客服」等），
// veto 不会触发、会话不会锁给人工，兜底回复反而把客户关在死路里。
// 文案（llm 包的两个池 + 本包 emptyReplyFallback）与关键词表（本包 NLPKeywords）分处两包，
// llm 反向 import service 会成环，所以守护只能站在这里、跨包取文案。
func TestFallbackCopy_HandoffKeywordIsMatchable(t *testing.T) {
	copySamples := collectFallbackCopies(t)

	if len(copySamples) < 7 {
		t.Fatalf("取到的兜底文案样本过少（%d 条），采样本身失效，本用例等于没判据: %v", len(copySamples), copySamples)
	}
	quoted := 0
	for _, s := range copySamples {
		for _, token := range quotedTokens(s) {
			quoted++
			if MatchTransferKeywords(token) || MatchExplicitKeywords(token) {
				continue
			}
			t.Errorf("兜底文案 %q 让客户回复「%s」，但该词不在转人工关键词表里（客户照做也转不了人工）", s, token)
		}
	}
	if quoted == 0 {
		t.Fatal("一条「…」暗号都没取到：文案被整体改没，或被引号形状绕过，采样失效")
	}

	fallback := (&SalesEngine{}).emptyReplyFallback()
	if !MatchTransferKeywords(extractFirstQuoted(fallback)) {
		t.Errorf("emptyReplyFallback 的暗号不在关键词表里: %q", fallback)
	}
}

// TestFallbackCopy_ScenarioPoolsAreFullySampled 采样的自证：轮换池里每一条都得被取到，
// 否则「所有文案都过判据」可能只测了池子第一格。
func TestFallbackCopy_ScenarioPoolsAreFullySampled(t *testing.T) {
	cases := []struct {
		scenario llm.DispatchScenario
		want     int
	}{
		{llm.ScenarioSOPReply, 2},
		{llm.ScenarioObjection, 2},
		{llm.ScenarioFriendlyChat, 2},
		{llm.DispatchScenario("no_such_scenario_falls_to_default"), 2},
	}
	for _, tc := range cases {
		seen := map[string]struct{}{}
		for i := 0; i < fallbackCopySampleCalls; i++ {
			seen[llm.TemplateReplyFor(tc.scenario)] = struct{}{}
		}
		if len(seen) < tc.want {
			t.Errorf("场景 %q 只采到 %d 条不同文案，池子至少 %d 条 ⇒ 采样没盖住池子", tc.scenario, len(seen), tc.want)
		}
	}
}

const fallbackCopySampleCalls = 64

func collectFallbackCopies(t *testing.T) []string {
	t.Helper()
	scenarios := []llm.DispatchScenario{
		llm.ScenarioIntentRecognize,
		llm.ScenarioSOPReply,
		llm.ScenarioObjection,
		llm.ScenarioFriendlyChat,
		llm.ScenarioLongSummary,
		llm.ScenarioHighQuality,
		llm.ScenarioLowCost,
		// 未知场景走通用池，是线上最常见的降级出口
		llm.DispatchScenario("unknown_scenario"),
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(scenarios)*2)
	for _, sc := range scenarios {
		for i := 0; i < fallbackCopySampleCalls; i++ {
			s := llm.TemplateReplyFor(sc)
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// quotedTokens 取出文案里所有「…」内的片段（中文引号成对出现；未闭合的按到行尾截）。
func quotedTokens(s string) []string {
	var tokens []string
	for {
		lo := strings.Index(s, "「")
		if lo < 0 {
			return tokens
		}
		rest := s[lo+len("「"):]
		hi := strings.Index(rest, "」")
		if hi < 0 {
			return append(tokens, strings.TrimSpace(rest))
		}
		if token := strings.TrimSpace(rest[:hi]); token != "" {
			tokens = append(tokens, token)
		}
		s = rest[hi+len("」"):]
	}
}

func extractFirstQuoted(s string) string {
	if toks := quotedTokens(s); len(toks) > 0 {
		return toks[0]
	}
	return ""
}
