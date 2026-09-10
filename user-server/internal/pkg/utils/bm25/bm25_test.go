package bm25

import (
	"reflect"
	"testing"
)

func TestTokenize_MixedCJKAndLatin(t *testing.T) {
	got := Tokenize("退款How to refund 123！")
	want := []string{"退", "款", "how", "to", "refund", "123"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenize_EmptyAndPunct(t *testing.T) {
	if got := Tokenize(""); len(got) != 0 {
		t.Fatalf("empty text should give no terms, got %v", got)
	}
	if got := Tokenize("!!!???"); len(got) != 0 {
		t.Fatalf("punct-only text should give no terms, got %v", got)
	}
}

func TestScoreText_SubstringHits(t *testing.T) {
	if got := ScoreText("申请退款流程如下", []string{"退款", "流程"}); got != 1.0 {
		t.Fatalf("both terms hit, score = %v, want 1", got)
	}
	if got := ScoreText("申请退款流程如下", []string{"退款", "发票"}); got != 0.5 {
		t.Fatalf("one of two terms hit, score = %v, want 0.5", got)
	}
	if got := ScoreText("无关内容", []string{"退款"}); got != 0 {
		t.Fatalf("no hit, score = %v, want 0", got)
	}
	if got := ScoreText("任意文本", nil); got != 0 {
		t.Fatalf("empty terms should score 0, got %v", got)
	}
}
