package service

import "testing"

// 有效性闸必须阻断降级/证据不全的高分判定，同时不把来源局部覆盖当成必然无效。
func TestLeadEligibility_Gate(t *testing.T) {
	cases := []struct {
		name string
		jd   LeadJudgement
		full bool
		want bool
	}{
		{"完整证据高分放行", LeadJudgement{IsLead: true, IntentScore: 80}, true, true},
		{"降级高分阻断", LeadJudgement{IsLead: true, IntentScore: 95, Degraded: true}, true, false},
		{"证据不全阻断", LeadJudgement{IsLead: true, IntentScore: 90}, false, false},
		{"React降级阻断", LeadJudgement{IsLead: true, IntentScore: 90, ReactOutcome: &LeadReactOutcome{Degraded: true}}, true, false},
		{"负例阻断", LeadJudgement{IsLead: false, IntentScore: 90}, true, false},
		{"低分阻断", LeadJudgement{IsLead: true, IntentScore: 30}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := leadSideEffectsEligible(&tc.jd, tc.full, 60)
			if got != tc.want {
				t.Fatalf("期望 eligible=%v，实际 %v", tc.want, got)
			}
		})
	}
}
