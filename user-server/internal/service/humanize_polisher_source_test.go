package service

import (
	"context"
	"testing"
)

func TestHumanizePolisherDistinguishesGroupSource(t *testing.T) {
	p := NewHumanizePolisher()
	if !p.shouldAddParticle(context.Background(), &PolishContext{Intent: "other"}) {
		t.Fatalf("单聊短回复应允许自然语气词")
	}
	if p.shouldAddParticle(context.Background(), &PolishContext{Intent: "other", IsGroup: true}) {
		t.Fatalf("群聊回复面向多成员，默认不应注入随意语气词")
	}
	if p.shouldAddParticle(context.Background(), &PolishContext{Intent: IntentComplaint, IsGroup: true}) {
		t.Fatalf("投诉场景群聊也不应注入语气词")
	}
}
