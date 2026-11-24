package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// D11/D12：高分+完整证据自动入库(new)，低分或证据不完整进候选(candidate)。
func TestClassifyLeadStatus(t *testing.T) {
	cfg := &model.LeadMiningConfig{MinIntentScore: 50}
	cases := []struct {
		name string
		jd   *LeadJudgement
		want string
	}{
		{"高分完整证据", &LeadJudgement{IsLead: true, IntentScore: 80}, "new"},
		{"低分", &LeadJudgement{IsLead: true, IntentScore: 30}, "candidate"},
		{"证据降级", &LeadJudgement{IsLead: true, IntentScore: 90, Degraded: true}, "candidate"},
		{"非线索", &LeadJudgement{IsLead: false, IntentScore: 90}, ""},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyLeadStatus(c.jd, cfg); got != c.want {
				t.Fatalf("%s: 期望 %s，实际 %s", c.name, c.want, got)
			}
		})
	}
	if got := classifyLeadStatus(&LeadJudgement{IsLead: true, IntentScore: 80}, nil); got != "candidate" {
		t.Fatalf("无配置时保守待审，实际 %s", got)
	}
}

// D14：已推进状态（含 notified/converted/ignored 终态）不得被后续低分判定降级。
func TestPromoteStatus_NeverDowngrades(t *testing.T) {
	cfg := &model.LeadMiningConfig{MinIntentScore: 50}
	low := &LeadJudgement{IsLead: true, IntentScore: 10}
	for _, current := range []string{"new", "notified", "following", "qualified", "converted", "won", "ignored", "invalid"} {
		if got := promoteStatus(current, low, cfg); got != current {
			t.Fatalf("状态 %s 被降级为 %s", current, got)
		}
	}
	if got := promoteStatus("candidate", &LeadJudgement{IsLead: true, IntentScore: 80}, cfg); got != "new" {
		t.Fatalf("candidate 高分应自动转正为 new，实际 %s", got)
	}
}
