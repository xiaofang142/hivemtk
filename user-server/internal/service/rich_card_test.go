package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

func TestMarshalRichCardRoundTrip(t *testing.T) {
	card := &model.RichCard{
		Type:        model.CardTypeProduct,
		Title:       "测试商品",
		Description: "描述",
		Fields:      map[string]string{"价格": "99", "规格": "256G"},
		Buttons:     []model.CardButton{{Text: "购买", URL: "https://x.com"}},
	}
	s, err := model.MarshalRichCard(card)
	if err != nil {
		t.Fatal(err)
	}
	out, err := model.UnmarshalRichCard(s)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != card.Title || out.Fields["价格"] != "99" || len(out.Buttons) != 1 {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestBuildTelegramCardText(t *testing.T) {
	card := &model.RichCard{
		Type:        model.CardTypeProduct,
		Title:       "iPhone 15",
		Description: "A17 芯片",
		Fields:      map[string]string{"价格": "5999"},
	}
	text := buildTelegramCardText(card)
	if !strings.Contains(text, "<b>iPhone 15</b>") {
		t.Fatalf("title should be bold: %s", text)
	}
	if !strings.Contains(text, "价格") || !strings.Contains(text, "5999") {
		t.Fatalf("fields missing: %s", text)
	}
	if strings.Contains(text, "&") && !strings.Contains(text, "&amp;") {
	}
}

func TestBuildTelegramCardKeyboard(t *testing.T) {
	card := &model.RichCard{
		Title: "x",
		Buttons: []model.CardButton{
			{Text: "购买", URL: "https://x.com"},
			{Text: "详情", URL: "https://x.com/d"},
		},
	}
	kb := buildTelegramCardKeyboard(card, "")
	if len(kb) != 2 {
		t.Fatalf("expected 2 keyboard rows, got %d", len(kb))
	}
	if kb[0][0].Text != "购买" || kb[0][0].URL != "https://x.com" {
		t.Fatalf("unexpected keyboard: %+v", kb)
	}
}

func TestBuildTelegramCardKeyboard_Empty(t *testing.T) {
	if kb := buildTelegramCardKeyboard(&model.RichCard{Title: "x"}, ""); kb != nil {
		t.Fatalf("expected nil keyboard when no buttons, got %+v", kb)
	}
}
