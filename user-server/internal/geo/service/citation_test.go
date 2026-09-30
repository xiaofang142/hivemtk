package service

import (
	"encoding/json"
	"testing"

	"hivemtk-user/internal/geo/model"
)

func mkRun(engine, query string, urls []string) *model.GeoProbeRun {
	cites := make([]Citation, 0, len(urls))
	for _, u := range urls {
		cites = append(cites, Citation{URL: u})
	}
	b, _ := json.Marshal(cites)
	return &model.GeoProbeRun{Engine: engine, Query: query, Citations: b}
}

func TestAggregateCitationDomains_Classify(t *testing.T) {
	runs := []*model.GeoProbeRun{
		mkRun("e1", "q1", []string{"https://www.acme.com/a", "https://blog.acme.com/b"}),
		mkRun("e2", "q1", []string{"https://rival.com/x"}),
		mkRun("e1", "q2", []string{"https://news.example.org/y", "https://rival.com/x"}),
		mkRun("e1", "q3", nil),
	}
	stats := AggregateCitationDomains(runs, "acme.com", []string{"rival.com"})
	byDomain := map[string]CitationDomainStat{}
	for _, s := range stats {
		byDomain[s.Domain] = s
	}
	cases := []struct {
		domain   string
		category string
		count    int
	}{
		{"acme.com", "own", 1},
		{"blog.acme.com", "own", 1},
		{"rival.com", "competitor", 2},
		{"news.example.org", "earned", 1},
	}
	for _, c := range cases {
		got, ok := byDomain[c.domain]
		if !ok {
			t.Fatalf("missing domain %s", c.domain)
		}
		if got.Category != c.category {
			t.Errorf("domain %s category=%s want %s", c.domain, got.Category, c.category)
		}
		if got.Count != c.count {
			t.Errorf("domain %s count=%d want %d", c.domain, got.Count, c.count)
		}
	}
	if byDomain["rival.com"].QueryCount != 2 {
		t.Errorf("rival query_count=%d want 2", byDomain["rival.com"].QueryCount)
	}
	if byDomain["rival.com"].EngineCount != 2 {
		t.Errorf("rival engine_count=%d want 2", byDomain["rival.com"].EngineCount)
	}
	// 按 count 降序
	if len(stats) == 0 || stats[0].Domain != "rival.com" {
		t.Errorf("first domain should be rival.com, got %+v", stats)
	}
}

func TestAggregateKeywordDomainMatrix_TopN(t *testing.T) {
	runs := []*model.GeoProbeRun{
		mkRun("e1", "q1", []string{"https://a.com/1", "https://b.com/1"}),
		mkRun("e1", "q1", []string{"https://a.com/2"}),
		mkRun("e1", "q2", []string{"https://b.com/2"}),
	}
	domains, rows := AggregateKeywordDomainMatrix(runs, 1, 1)
	if len(domains) != 1 || domains[0] != "a.com" {
		t.Fatalf("top domain should be [a.com], got %v", domains)
	}
	if len(rows) != 1 || rows[0].Query != "q1" {
		t.Fatalf("top query should be q1, got %+v", rows)
	}
	if rows[0].Domains["a.com"] != 2 || rows[0].Total != 2 {
		t.Errorf("q1 row wrong: %+v", rows[0])
	}
}
