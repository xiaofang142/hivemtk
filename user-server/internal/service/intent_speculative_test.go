package service

import (
	"context"
	"testing"
	"time"
)

func TestRecognizeSpeculative_EmptyText(t *testing.T) {
	rec := &IntentRecognizer{}
	ctx := context.Background()
	result, ch, err := rec.RecognizeSpeculative(ctx, "s1", "c1", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.IntentType != IntentUnknown {
		t.Errorf("expected unknown, got %s", result.IntentType)
	}
	select {
	case r := <-ch:
		if r.IntentType != IntentUnknown {
			t.Errorf("ch result should be unknown, got %s", r.IntentType)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected ch to receive immediately")
	}
}

func TestRecognizeSpeculative_DisabledFlag(t *testing.T) {
	storeIntentEnabled(false)
	defer func() { storeIntentEnabled(true) }()
	rec := &IntentRecognizer{}
	ctx := context.Background()
	result, ch, err := rec.RecognizeSpeculative(ctx, "s1", "c1", "你好")
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "disabled" {
		t.Errorf("expected method=disabled, got %s", result.Method)
	}
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Error("expected ch immediate receive")
	}
}

func TestRecognizeSpeculative_RuleHit(t *testing.T) {
	rec := &IntentRecognizer{}
	ctx := context.Background()
	result, ch, err := rec.RecognizeSpeculative(ctx, "s1", "c1", "你好")
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "rule" {
		t.Errorf("expected method=rule, got %s", result.Method)
	}
	if result.Confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", result.Confidence)
	}
	select {
	case _, ok := <-ch:
		if ok {
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRecognizeSpeculative_RuleMiss_NoDispatcher(t *testing.T) {
	rec := &IntentRecognizer{}
	ctx := context.Background()
	result, ch, err := rec.RecognizeSpeculative(ctx, "s1", "c1", "随便说点啥12345abc")
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "rule_placeholder" {
		t.Errorf("expected method=rule_placeholder, got %s", result.Method)
	}
	select {
	case r := <-ch:
		if r.Method != "rule_placeholder" {
			t.Errorf("expected placeholder, got %s", r.Method)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("expected ch to receive placeholder")
	}
}

func TestRecognizeSpeculative_Channel_BufferIsOne(t *testing.T) {
	rec := &IntentRecognizer{}
	ctx := context.Background()
	_, ch, _ := rec.RecognizeSpeculative(ctx, "s1", "c1", "你好")
	if cap(ch) < 1 {
		t.Errorf("expected ch cap >= 1, got %d", cap(ch))
	}
}
