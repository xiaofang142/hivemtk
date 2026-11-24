package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestLeadMiningClueStatusTransition(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{"new", "qualified", true},
		{"new", "won", false},
		{"qualified", "following", true},
		{"qualified", "new", false},
		{"duplicate", "qualified", false},
		{"false_positive", "new", true},
		{"following", "won", true},
		{"won", "qualified", false},
	}
	for _, tc := range cases {
		if got := CanTransitionLeadMiningClueStatus(tc.from, tc.to); got != tc.ok {
			t.Fatalf("transition %s->%s got %v want %v", tc.from, tc.to, got, tc.ok)
		}
	}
}

func TestSelectLeadMiningNotifyRoutesPriority(t *testing.T) {
	routes := []model.LeadMiningNotifyRoute{
		{RouteID: "r1", Channel: "wechat", AccountID: "", NotifyChannel: "telegram", Target: "1:2", Enabled: true},
		{RouteID: "r2", Channel: "wechat", AccountID: "acc1", NotifyChannel: "telegram", Target: "1:3", Enabled: true},
		{RouteID: "r3", Channel: "", AccountID: "", NotifyChannel: "feishu", Target: "1:ou_x", Enabled: true},
	}
	got := selectLeadMiningNotifyRoutes(routes, "wechat", "acc1")
	if len(got) != 1 || got[0].RouteID != "r2" {
		t.Fatalf("expected account exact route r2, got %+v", got)
	}
	got = selectLeadMiningNotifyRoutes(routes, "wechat", "acc2")
	if len(got) != 1 || got[0].RouteID != "r1" {
		t.Fatalf("expected channel default r1, got %+v", got)
	}
	got = selectLeadMiningNotifyRoutes(routes, "telegram", "acc2")
	if len(got) != 1 || got[0].RouteID != "r3" {
		t.Fatalf("expected global default r3, got %+v", got)
	}
}

func TestLeadMiningExternalMessageDesensitize(t *testing.T) {
	clue := &model.Clue{SourceChannel: "wechat", LeadRole: "demand", IntentScore: 88, Summary: "联系 13800138000 或 test@example.com"}
	msg := buildLeadMiningExternalMessage(clue)
	if msg == "" {
		t.Fatal("empty msg")
	}
	if stringsContains(msg, "13800138000") || stringsContains(msg, "test@example.com") {
		t.Fatalf("message not desensitized: %s", msg)
	}
}

func stringsContains(s, sub string) bool { return len(sub) > 0 && (func() bool { return indexOf(s, sub) >= 0 })() }

func indexOf(s, sub string) int {
	// small helper to avoid importing strings in test if not present; but strings is fine
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestLeadMiningExternalDedup(t *testing.T) {
	key := "1:clue:telegram:1:2"
	// reset state for deterministic test
	leadMiningExternalDedupMu.Lock()
	delete(leadMiningExternalDedup, key)
	leadMiningExternalDedupMu.Unlock()
	if !claimLeadMiningExternalDedup(key) {
		t.Fatal("first claim should succeed")
	}
	if claimLeadMiningExternalDedup(key) {
		t.Fatal("second claim should be deduped")
	}
}
