package service

import (
	"strings"
	"testing"
)

func containsOppStr(s, sub string) bool { return strings.Contains(s, sub) }

func TestSummarizeSOV(t *testing.T) {
	entries := []SOVEntry{
		{Brand: "竞品A", SOV: 40},
		{Brand: "HiveMTK", SOV: 35},
		{Brand: "竞品B", SOV: 10},
	}
	p := summarizeSOV(entries, "hivemtk")
	if p.OwnSOV != 35 {
		t.Errorf("OwnSOV = %v, want 35", p.OwnSOV)
	}
	if p.TopRival != "竞品A" || p.TopRivalSOV != 40 {
		t.Errorf("TopRival = %v/%v, want 竞品A/40", p.TopRival, p.TopRivalSOV)
	}
}

func TestBuildOpportunitiesSOVDrop(t *testing.T) {
	out := BuildOpportunities(OpportunityInputs{
		HasSOVData: true,
		Cur:        sovPoint{OwnSOV: 30, TopRival: "竞品A", TopRivalSOV: 40},
		Prev:       sovPoint{OwnSOV: 38},
	})
	if len(out) < 2 {
		t.Fatalf("want >=2 opportunities (drop+rival), got %d", len(out))
	}
	if out[0].Priority != "high" {
		t.Errorf("first opportunity priority = %s, want high", out[0].Priority)
	}
}

func TestBuildOpportunitiesCitedAndAlerts(t *testing.T) {
	out := BuildOpportunities(OpportunityInputs{
		CitedDomains: []CitationDomainStat{
			{Domain: "rival.com", Category: "competitor", Count: 12, QueryCount: 5, EngineCount: 2},
			{Domain: "own.com", Category: "own", Count: 99},
		},
		UnreadAlerts:     3,
		RecentNegAlerts:  []string{"HiveMTK 差评"},
		LowScoreArticles: []string{"旧文章A"},
	})
	types := map[string]bool{}
	for _, o := range out {
		types[o.Type] = true
	}
	for _, want := range []string{"cited_competitor", "negative_alert", "low_score_content"} {
		if !types[want] {
			t.Errorf("missing opportunity type %s (got %+v)", want, out)
		}
	}
	// 自有域不应生成机会
	for _, o := range out {
		if o.Type == "cited_competitor" && containsOppStr(o.Title, "own.com") {
			t.Errorf("own domain should not produce opportunity: %v", o)
		}
	}
}

func TestBuildOpportunitiesEmpty(t *testing.T) {
	if out := BuildOpportunities(OpportunityInputs{}); len(out) != 0 {
		t.Errorf("empty input should yield no opportunities, got %d", len(out))
	}
}
