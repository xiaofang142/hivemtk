package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestStatsByDomainFlagsSelfSite 自家站的判定必须落在"基址 + 路径边界"上，而不是 host。
//
// GitHub Pages 项目页形态下 host 是 xiaofang142.github.io，同一 host 上还住着别人的项目：
// 只比 host 会把 someone-else 的爬虫访问算成自家权威源，自家站反而认不出来。
// 因此聚合键是"站点标识"= 自家取 基址的 host+路径，其他取 host。
func TestStatsByDomainFlagsSelfSite(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "https://xiaofang142.github.io/hivemtk")

	testdb := testutil.NewTestDB(t, &model.GeoCrawlerVisit{})
	repo := NewGeoCrawlerVisitRepository(testdb)
	ctx := context.Background()

	visits := []*model.GeoCrawlerVisit{
		{Keyword: "GEO优化", Engine: "GPTBot", Path: "https://xiaofang142.github.io/hivemtk/features"},
		{Keyword: "GEO优化", Engine: "GPTBot", Path: "https://xiaofang142.github.io/hivemtk/"},
		{Keyword: "别人的项目", Engine: "GPTBot", Path: "https://xiaofang142.github.io/someone-else/"},
		{Keyword: "竞品", Engine: "GPTBot", Path: "https://weibanzhushou.com/product"},
	}
	if err := repo.BulkCreate(ctx, visits); err != nil {
		t.Fatalf("写入爬虫访问失败: %v", err)
	}

	rows, err := repo.StatsByDomain(ctx, 1)
	if err != nil {
		t.Fatalf("StatsByDomain 失败: %v", err)
	}
	byDomain := map[string]DomainStatRow{}
	for _, r := range rows {
		byDomain[r.Domain] = r
	}

	self, ok := byDomain["xiaofang142.github.io/hivemtk"]
	if !ok {
		t.Fatalf("自家站未按基址成行，实际行集=%v", domainKeys(byDomain))
	}
	if !self.IsSelfSite {
		t.Errorf("基址下的访问必须判为自家站，实际 IsSelfSite=false")
	}
	if self.SourceLevel != "A" {
		t.Errorf("自家站源等级应为 A，实际 %q", self.SourceLevel)
	}
	if self.VisitCount != 2 {
		t.Errorf("自家两条路径应折叠进同一站点行（visit=2），实际 %d", self.VisitCount)
	}

	other, ok := byDomain["xiaofang142.github.io"]
	if !ok {
		t.Fatalf("同 host 的他人项目页未单独成行（被按 host 折叠了？），实际行集=%v", domainKeys(byDomain))
	}
	if other.IsSelfSite {
		t.Errorf("同 host 不同项目页不得判为自家站")
	}
	if other.VisitCount != 1 {
		t.Errorf("他人项目页应独立成一行（visit=1），实际 %d", other.VisitCount)
	}

	comp, ok := byDomain["weibanzhushou.com"]
	if !ok {
		t.Fatalf("竞品站未成行，实际行集=%v", domainKeys(byDomain))
	}
	if comp.IsSelfSite {
		t.Errorf("竞品站不得判为自家站")
	}
	if comp.SourceLevel != "B" {
		t.Errorf("竞品 weibanzhushou.com 等级应为 B，实际 %q", comp.SourceLevel)
	}

	n, err := repo.ActiveDomains(ctx, 1)
	if err != nil {
		t.Fatalf("ActiveDomains 失败: %v", err)
	}
	if n != 3 {
		t.Errorf("活跃站点数应为 3（自家 + 同 host 他人 + 竞品），实际 %d", n)
	}
}

// TestSelfSiteNotKeyedByHost 已下线域名彻底出局：历史数据里的 hive.* 不再算自家，
// 也不再享有静态表里的 A 级。
//
// 下面那个 hive.xapptool.cn 字面量是"被断言的对象"而不是"被推荐的地址"：本用例要证的
// 恰恰是它不再享有任何自家待遇，换成别的死域就等于把要防的那次回流改成防不住。
// 因此它在 scripts/check-no-xapptool.sh 里按具体路径白名单放行（见该脚本 WHITELIST 与本文件同路径），
// 而不是豁免整个 geo 目录。2026-09-21 实测：本文件是全仓唯一还写着这个域的源码文件。
func TestSelfSiteNotKeyedByHost(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "https://xiaofang142.github.io/hivemtk")

	testdb := testutil.NewTestDB(t, &model.GeoCrawlerVisit{})
	repo := NewGeoCrawlerVisitRepository(testdb)
	ctx := context.Background()

	if err := repo.Create(ctx, &model.GeoCrawlerVisit{
		Keyword: "历史数据", Engine: "ClaudeBot", Path: "https://hive.xapptool.cn/docs",
	}); err != nil {
		t.Fatalf("写入历史访问失败: %v", err)
	}
	rows, err := repo.StatsByDomain(ctx, 1)
	if err != nil {
		t.Fatalf("StatsByDomain 失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应只有 1 行历史域名，实际 %d 行", len(rows))
	}
	if rows[0].IsSelfSite {
		t.Errorf("已下线域名不应判为自家站")
	}
	if rows[0].SourceLevel == "A" {
		t.Errorf("已下线域名不应再享有 A 级静态映射，实际 level=%q", rows[0].SourceLevel)
	}
}

func domainKeys(m map[string]DomainStatRow) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
