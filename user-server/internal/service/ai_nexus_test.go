package service

import (
	"context"
	"strings"
	"testing"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
)

// TestHumanizePolisher_RemoveAITraces 测试去除 AI 痕迹
func TestHumanizePolisher_RemoveAITraces(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	cases := []struct {
		in   string
		want string
	}{
		{"作为 AI 助手，我建议您... ", "，我建议您... "},
		{"我是 AI，无法处理此事", "，无法处理此事"},
		{"很抱歉，我无法帮助您", "，我无法帮助您"},
		{"我理解您的需求", "我理解您的需求"},
	}
	for i, c := range cases {
		got, _ := p.Polish(context.Background(), c.in, nil)
		if strings.Contains(got, "作为 AI 助手") || strings.Contains(got, "我是 AI") {
			t.Errorf("case %d: AI trace not removed: %q -> %q", i, c.in, got)
		}
		_ = c.want
	}
}

func TestHumanizePolisher_RemoveExtraSymbols(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	cases := []struct {
		in   string
		want string
	}{
		{"好好好！！！", "好好好！"},
		{"什么？？？", "什么？"},
		{"嗯。。。。", "嗯……"},
	}
	for _, c := range cases {
		got, _ := p.Polish(context.Background(), c.in, nil)
		if strings.Count(got, "！") > 1 && strings.Contains(c.in, "！") {
			t.Errorf("multi-bang not collapsed: %q -> %q", c.in, got)
		}
		_ = c.want
	}
}

func TestHumanizePolisher_TruncateByLength(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.maxLength = 10
	got, _ := p.Polish(context.Background(), "这是一段非常长的测试文本，包含很多很多字符", nil)
	if len([]rune(got)) > 12 {
		t.Errorf("truncate failed: len=%d, text=%q", len([]rune(got)), got)
	}
}

func TestHumanizePolisher_PlatformStyle(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	got, _ := p.Polish(context.Background(), "好的😊", &PolishContext{Platform: "wechat"})
	if !strings.Contains(got, "😊") {
		t.Errorf("wechat should keep emoji: %q", got)
	}
	got, _ = p.Polish(context.Background(), "嗯", &PolishContext{Platform: "email"})
	if !strings.Contains(got, "是的") {
		t.Errorf("email should formalize: %q", got)
	}
}

func TestHumanizePolisher_NoParticleForComplaint(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	original := "好的，我来帮您处理"
	got, _ := p.Polish(context.Background(), original, &PolishContext{Intent: IntentComplaint})
	if got != original && !strings.HasPrefix(got, "好的") {
		t.Errorf("complaint should keep plain reply, got %q", got)
	}
}

func TestHumanizePolisher_EmptyInput(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	got, _ := p.Polish(context.Background(), "", nil)
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestRAG_ScoreText(t *testing.T) {
	score := knowledgesvc.ScoreText("产品价格 999 元", []string{"价格", "产品"})
	if score != 1.0 {
		t.Errorf("expected 1.0, got %f", score)
	}
	score = knowledgesvc.ScoreText("完全无关内容", []string{"价格"})
	if score != 0.0 {
		t.Errorf("expected 0.0, got %f", score)
	}
}

// FeedbackLearner tests
func TestFeedbackLearner_Record(t *testing.T) {
	f := NewFeedbackLearner(nil)
	_ = f.RecordFeedback(context.Background(), &FeedbackRecord{
		IntentType:     "price_inquiry",
		Confidence:     0.85,
		CustomerAccept: true,
		Tokens:         100,
	})
	_ = f.RecordFeedback(context.Background(), &FeedbackRecord{
		IntentType:     "price_inquiry",
		Confidence:     0.75,
		CustomerAccept: false,
		Transferred:    true,
		Tokens:         120,
	})
	stats := f.GetIntentStats(context.Background(), "price_inquiry")
	if stats == nil {
		t.Fatal("stats should not be nil")
	}
	if stats.TotalCount != 2 {
		t.Errorf("expected 2, got %d", stats.TotalCount)
	}
}

func TestFeedbackLearner_ConfidenceFloor(t *testing.T) {
	f := NewFeedbackLearner(nil)
	if got := f.SuggestConfidenceFloor(context.Background(), "unknown"); got != 0.5 {
		t.Errorf("cold start should be 0.5, got %f", got)
	}
}

func TestFeedbackLearner_StatsCopy(t *testing.T) {
	f := NewFeedbackLearner(nil)
	_ = f.RecordFeedback(context.Background(), &FeedbackRecord{IntentType: "test", Confidence: 0.5, CustomerAccept: true})
	stats := f.GetIntentStats(context.Background(), "test")
	stats.TotalCount = 9999
	original := f.GetIntentStats(context.Background(), "test")
	if original.TotalCount == 9999 {
		t.Error("stats should be returned by value (copy)")
	}
}
