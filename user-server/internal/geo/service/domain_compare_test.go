package service

import (
	"testing"

	"hivemtk-user/internal/geo/repository"
)

// TestDomainCompareUsesSelfSiteFlag 自家站那一行由仓储算好的 IsSelfSite 决定，
// 不再靠"host 等于某个写死域名"——那个域名已下线，且 Pages 项目页形态下 host 是共享的。
func TestDomainCompareUsesSelfSiteFlag(t *testing.T) {
	rows := []repository.DomainStatRow{
		{Domain: "xiaofang142.github.io/hivemtk", Engine: "GPTBot", VisitCount: 7, SourceLevel: "A", IsSelfSite: true},
		{Domain: "xiaofang142.github.io", Engine: "GPTBot", VisitCount: 3, SourceLevel: "D"},
		{Domain: "weibanzhushou.com", Engine: "GPTBot", VisitCount: 5, SourceLevel: "B"},
	}

	out, selfVisits, compVisits, coverage := computeDomainCompare(rows)

	var selfRows []DomainCompareRow
	for _, r := range out {
		if r.IsHiveMTK {
			selfRows = append(selfRows, r)
		}
	}
	if len(selfRows) != 1 {
		t.Fatalf("恰好一行是自家站，实际 %d 行：%+v", len(selfRows), out)
	}
	if selfRows[0].Domain != "xiaofang142.github.io/hivemtk" {
		t.Errorf("被标成自家的是 %q，应为基址那行", selfRows[0].Domain)
	}
	for _, r := range out {
		if r.Domain == "xiaofang142.github.io" && r.IsHiveMTK {
			t.Errorf("同 host 的他人项目页不得标成自家")
		}
	}
	if selfVisits != 7 {
		t.Errorf("自家访问量应为 7，实际 %d", selfVisits)
	}
	if compVisits != 8 {
		t.Errorf("竞品访问量应为 3+5=8，实际 %d", compVisits)
	}
	if coverage <= 0 || coverage >= 100 {
		t.Errorf("覆盖率应落在 (0,100) 区间，实际 %.2f", coverage)
	}
}

// TestDomainCompareShareIsOrderIndependent share_pct 的分母是全量访问，
// 不能是"遍历到本行为止的累计值"——聚合源是 map，遍历顺序每轮都不同。
func TestDomainCompareShareIsOrderIndependent(t *testing.T) {
	rows := []repository.DomainStatRow{
		{Domain: "a.com", Engine: "GPTBot", VisitCount: 10, SourceLevel: "A"},
		{Domain: "b.com", Engine: "GPTBot", VisitCount: 30, SourceLevel: "B"},
		{Domain: "c.com", Engine: "ClaudeBot", VisitCount: 60, SourceLevel: "C", IsSelfSite: true},
	}

	shareOf := func(in []repository.DomainStatRow) map[string]float64 {
		out, _, _, _ := computeDomainCompare(in)
		m := map[string]float64{}
		for _, r := range out {
			m[r.Domain] = r.SharePct
		}
		return m
	}

	first := shareOf(rows)
	// 反序喂入：同一批数据不得因为 map 遍历顺序不同给出不同的占比。
	reversed := shareOf([]repository.DomainStatRow{rows[2], rows[1], rows[0]})

	for _, domain := range []string{"a.com", "b.com", "c.com"} {
		if first[domain] != reversed[domain] {
			t.Errorf("%s 的 share_pct 随输入顺序变化：%v vs %v", domain, first[domain], reversed[domain])
		}
	}
	if got := first["a.com"]; got != 10 {
		t.Errorf("a.com 占 10/100 应为 10%%，实际 %.2f", got)
	}
	if got := first["c.com"]; got != 60 {
		t.Errorf("c.com 占 60/100 应为 60%%，实际 %.2f", got)
	}
	var sum float64
	for _, v := range first {
		sum += v
	}
	if sum < 99.99 || sum > 100.01 {
		t.Errorf("各行占比之和应为 100，实际 %.2f", sum)
	}
}
