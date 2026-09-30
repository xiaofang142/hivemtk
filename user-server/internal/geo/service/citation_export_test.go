package service

import (
	"strings"
	"testing"
)

func TestBuildDomainsCSV(t *testing.T) {
	res := CitationDomainResult{Days: 7, Total: 3, Domains: []CitationDomainStat{
		{Domain: "example.com", Category: "own", Count: 2, QueryCount: 1, EngineCount: 2},
	}}
	out := BuildDomainsCSV(&res)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("期望2行,得%d: %q", len(lines), out)
	}
	if lines[0] != "domain,category,count,query_count,engine_count" {
		t.Fatalf("表头错误: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "example.com,own,2,1,2") {
		t.Fatalf("数据行错误: %q", lines[1])
	}
}

func TestBuildMatrixCSV_QuoteComma(t *testing.T) {
	res := CitationMatrixResult{Days: 7, Domains: []string{"a.com"}, Rows: []CitationMatrixRow{
		{Query: "甲,乙", Domains: map[string]int{"a.com": 3}, Total: 3},
	}}
	out := BuildMatrixCSV(&res)
	if !strings.Contains(out, `"甲,乙",3,3`) {
		t.Fatalf("逗号未被引号包裹: %q", out)
	}
	empty := BuildMatrixCSV(&CitationMatrixResult{})
	if !strings.HasPrefix(empty, "query,total") {
		t.Fatalf("空矩阵表头错误: %q", empty)
	}
}
