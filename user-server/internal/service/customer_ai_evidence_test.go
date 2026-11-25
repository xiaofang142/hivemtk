package service

import (
	"strings"
	"testing"
)

// TestResolveAISuggestionsEvidence_NoRefsNoFallback 无引用不得回退"全部事实"兜底：
// 没有可核验证据时画像/分层结论必须降级为不输出，并把降级原因写在结果里。
func TestResolveAISuggestionsEvidence_NoRefsNoFallback(t *testing.T) {
	ev := map[string]AIEvidence{"name": {Type: "customer_field", Ref: "name", Snippet: "张三", Source: "customers", OccurredAt: "2026-01-01T00:00:00Z"}}
	got := resolveAISuggestions("profile", "基于全部事实的画像", 0.9, nil, nil, ev, "prompt-v1", "m1")
	if len(got) != 1 {
		t.Fatalf("无引用应产出 1 条降级记录，实际 %d", len(got))
	}
	if got[0].Kind != "profile_degraded" || len(got[0].Evidence) != 0 {
		t.Fatalf("无引用应降级且不带证据，实际 %+v", got[0])
	}
}

// TestResolveAISuggestionsEvidence_ValidRefsHaveSourceTime 有效引用必须带来源与时间，模型分数与规则分数分开标记。
func TestResolveAISuggestionsEvidence_ValidRefsHaveSourceTime(t *testing.T) {
	ev := map[string]AIEvidence{
		"tag": {Type: "customer_field", Ref: "tag", Snippet: "高意向", Source: "customers", OccurredAt: "2026-01-01T00:00:00Z"},
	}
	got := resolveAISuggestions("opportunity", "有采购意向", 0.73, []string{"tag"}, nil, ev, "prompt-v1", "m1")
	if len(got) != 1 || got[0].Kind != "opportunity" {
		t.Fatalf("有据结论应保留，实际 %+v", got)
	}
	if len(got[0].Evidence) != 1 || got[0].Evidence[0].Source == "" || got[0].Evidence[0].OccurredAt == "" {
		t.Fatalf("证据缺来源/时间：%+v", got[0].Evidence)
	}
	if got[0].ConfidenceSource != "model_estimate" || got[0].RuleScore != 1 {
		t.Fatalf("模型分数与规则分数应分离：%+v", got[0])
	}
}

// TestResolveAISuggestionsEvidence_ForgedAndUnauthorizedRefs 伪造引用与越权引用一律降级，不得借其他 facts 兜底。
func TestResolveAISuggestionsEvidence_ForgedAndUnauthorizedRefs(t *testing.T) {
	ev := map[string]AIEvidence{"tag": {Type: "customer_field", Ref: "tag", Snippet: "高意向", Source: "customers", OccurredAt: "2026-01-01T00:00:00Z"}}
	for _, refs := range [][]string{{"not_exists"}, {"secret_field"}} {
		got := resolveAISuggestions("segment", "凭空结论", 0.99, refs, nil, ev, "prompt-v1", "m1")
		if len(got) != 1 || !strings.HasSuffix(got[0].Kind, "_degraded") {
			t.Fatalf("伪造/越权引用必须降级，refs=%v 实际 %+v", refs, got)
		}
	}
}

// TestResolveAISuggestionsEvidence_RuleScoreIsMeasured 规则分数只能来自证据覆盖度，不能复用模型 confidence 伪装实测准确率。
func TestResolveAISuggestionsEvidence_RuleScoreIsMeasured(t *testing.T) {
	ev := map[string]AIEvidence{
		"tag":      {Type: "customer_field", Ref: "tag", Snippet: "高意向", Source: "customers", OccurredAt: "2026-01-01T00:00:00Z"},
		"industry": {Type: "customer_field", Ref: "industry", Snippet: "电子", Source: "customers", OccurredAt: "2026-01-01T00:00:00Z"},
	}
	got := resolveAISuggestions("opportunity", "有采购意向", 0.9, []string{"tag", "forged"}, nil, ev, "prompt-v1", "m1")
	if len(got) != 1 {
		t.Fatalf("部分有效引用应保留 1 条，实际 %d", len(got))
	}
	if got[0].RuleScore <= 0 || got[0].RuleScore >= 1 {
		t.Fatalf("规则分数应为可解释的覆盖度(0,1)，实际 %v", got[0].RuleScore)
	}
	if got[0].Evidence[0].Ref != "tag" {
		t.Fatalf("伪造引用不得混入证据：%+v", got[0].Evidence)
	}
}

// TestResolveAISuggestionsEvidence_StaleEvidenceRejected 失效证据（已过期引用）不得作为有效证据。
func TestResolveAISuggestionsEvidence_StaleEvidenceRejected(t *testing.T) {
	ev := map[string]AIEvidence{"tag": {Type: "customer_field", Ref: "tag", Snippet: "旧标签", Stale: true}}
	got := resolveAISuggestions("profile", "旧画像", 0.8, []string{"tag"}, nil, ev, "prompt-v1", "m1")
	if len(got) != 1 || !strings.HasSuffix(got[0].Kind, "_degraded") {
		t.Fatalf("失效证据必须降级，实际 %+v", got)
	}
}
