package service

import (
	"strings"
	"testing"
)

func TestLegacyBusinessNoteNeverRenderedAsConfirmedFact(t *testing.T) {
	note, ok := DecodeBusinessNote("note:lead_mining:客户预算 50 万")
	if !ok {
		t.Fatal("旧备注应可解码为兼容背景")
	}
	if note.Attribute != BusinessNoteLegacyUnverified {
		t.Fatalf("旧备注必须分类为 legacy_unverified，实际 %q", note.Attribute)
	}
	if CanStateBusinessNoteAsFact(note) {
		t.Fatal("旧备注不得陈述为既成事实")
	}
	block := (SharedBusinessInput{Notes: []BusinessNote{note}}).PromptBlock()
	if !strings.Contains(block, "待核实历史备注") || !strings.Contains(block, "不得当作已确认事实") {
		t.Fatalf("旧备注缺少降级提示：%s", block)
	}
	if strings.Contains(block, "属性=既成事实") {
		t.Fatalf("旧备注被错误渲染为既成事实：%s", block)
	}
}

func TestBusinessNoteAttributeClassification(t *testing.T) {
	tests := []struct {
		attribute string
		want      string
		fact      bool
	}{
		{attribute: BusinessNoteFact, want: BusinessNoteFact, fact: true},
		{attribute: BusinessNoteSuggestion, want: BusinessNoteSuggestion},
		{attribute: BusinessNoteLegacyUnverified, want: BusinessNoteLegacyUnverified},
		{attribute: "unknown", want: BusinessNoteLegacyUnverified},
		{attribute: "", want: BusinessNoteLegacyUnverified},
	}
	for _, tt := range tests {
		t.Run(tt.attribute, func(t *testing.T) {
			got := NormalizeBusinessNoteAttribute(tt.attribute)
			if got != tt.want {
				t.Fatalf("属性分类错误：got %q want %q", got, tt.want)
			}
			if CanStateBusinessNoteAsFact(BusinessNote{Attribute: got}) != tt.fact {
				t.Fatalf("事实陈述权限错误：attribute=%q", got)
			}
		})
	}
}

func TestEncodeBusinessNotePreservesExplicitLegacyClassification(t *testing.T) {
	tag := EncodeBusinessNote(BusinessNote{
		Source: ContextNoteNamespaceLeadMining, At: 10, Attribute: BusinessNoteLegacyUnverified, Text: "待核实预算",
	})
	got, ok := DecodeBusinessNote(tag)
	if !ok || got.Attribute != BusinessNoteLegacyUnverified || got.Text != "待核实预算" {
		t.Fatalf("显式待核实属性丢失：tag=%q got=%+v ok=%t", tag, got, ok)
	}
}
