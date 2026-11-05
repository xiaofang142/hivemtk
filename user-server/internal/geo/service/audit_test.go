package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func auditServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoAuditRun{}, &model.GeoAuditItem{})
	if db == nil {
		t.Fatal("测试库不可达：审计服务用例判不了")
	}
	return db
}

// auditFixtureSite 一个最小的可审计站点：robots 放行 AI 爬虫、llms.txt 合规、
// 文章含 H1/H2/列表/表格/FAQ/量化数据/外链/JSON-LD。
func auditFixtureSite() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html lang="zh-CN"><head>
<title>HiveMtk GEO 审计夹具</title>
<meta name="description" content="HiveMtk 是面向企业的 AI 销售与智能客服平台，适合需要私域增长的中小团队，这是最新版本的产品说明。">
<meta property="og:title" content="HiveMtk"><meta property="og:description" content="AI 销售平台">
<link rel="canonical" href="https://example.com/">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"HiveMtk","sameAs":["https://github.com/xiaofang142/hivemtk"],"logo":"https://example.com/logo.png"}</script>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"HiveMtk GEO 审计夹具","author":{"@type":"Person","name":"HiveMtk 团队"},"datePublished":"2026-01-01"}</script>
</head><body>
<nav>首页 产品 价格</nav>
<main>
<h1>HiveMtk 是什么</h1>
<p>HiveMtk 是面向企业的 AI 销售与智能客服平台。我们的实践覆盖 13 个渠道，服务了 200% 增长的客户团队。</p>
<h2>核心功能</h2>
<ul><li>AI 销售引擎</li><li>私域 SCRM</li><li>RAG 知识库</li></ul>
<table><tr><th>版本</th><th>能力</th></tr><tr><td>v2</td><td>多模型验证</td></tr></table>
<h2>常见问题</h2>
<p><strong>Q: 适合什么人？</strong></p><p>A: 适合需要私域增长的中小团队。</p>
<p>相比上一代，v2 增加了 Schema 与 llms.txt 支持。参考 <a href="https://example.com/docs">官方文档</a> 与 <a href="/pricing">价格页</a>。</p>
</main>
<footer>备案号 000000</footer>
</body></html>`)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nUser-agent: OAI-SearchBot\nAllow: /\nUser-agent: PerplexityBot\nAllow: /\nUser-agent: Claude-SearchBot\nAllow: /\nSitemap: http://example.com/sitemap.xml\n")
	})
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "# HiveMtk\n\n> AI 销售与智能客服平台\n\n## 产品\n- [GEO](https://example.com/geo)\n- [定价](https://example.com/pricing)\n\n## 文档\n- [指南](https://example.com/docs)\n\n更多内容见站点地图。")
	})
	return httptest.NewServer(mux)
}

func TestHtmlToAuditMarkdown_KeepsStructureDropsChrome(t *testing.T) {
	html := `<html><body><nav>导航</nav><main><h1>标题</h1><p>第一段<br>第二行</p>
<ul><li>要点一</li></ul><table><tr><th>列</th><td>值</td></tr></table>
<p><a href="/pricing">价格</a></p></main><footer>页脚</footer><script>alert(1)</script></body></html>`
	got := htmlToAuditMarkdown(html)
	for _, want := range []string{"# 标题", "- ", "| 列 | 值 |", "[价格](/pricing)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown 缺少 %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"导航", "页脚", "alert"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("Markdown 不应包含 %q:\n%s", unwanted, got)
		}
	}
}

func TestAuditService_RunSiteAuditPersists(t *testing.T) {
	db := auditServiceTestDB(t)
	site := auditFixtureSite()
	defer site.Close()

	svc := NewAuditService(repository.NewGeoAuditRepositoryWithDB(db), nil)
	run, view, err := svc.RunSiteAudit(context.Background(), site.URL, "HiveMtk", model.GeoAuditSourceManual, "tester")
	if err != nil {
		t.Fatalf("站点审计失败: %v", err)
	}
	if run.ID == "" || run.RubricVersion != GeoSiteAuditRubricV3 {
		t.Fatalf("run 字段不完整: %+v", run)
	}
	if run.MaxScore != 112 || run.Score <= 0 {
		t.Fatalf("站点审计分数异常: %d/%d", run.Score, run.MaxScore)
	}
	if len(view.Categories) != 10 {
		t.Fatalf("rubric v3 应有 10 类，实际 %d", len(view.Categories))
	}
	gotRun, items, err := svc.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if gotRun.ID != run.ID || len(items) == 0 {
		t.Fatalf("落库不完整: run=%+v items=%d", gotRun, len(items))
	}
	if items[0].RunID != run.ID || items[0].TenantID == 0 {
		t.Fatalf("item 归属字段未填: %+v", items[0])
	}
}

func TestAuditService_RunContentAuditFromURL(t *testing.T) {
	db := auditServiceTestDB(t)
	site := auditFixtureSite()
	defer site.Close()

	svc := NewAuditService(repository.NewGeoAuditRepositoryWithDB(db), nil)
	run, view, err := svc.RunContentAudit(context.Background(), ContentAuditInput{URL: site.URL, Brand: "HiveMtk"}, model.GeoAuditSourceAPI, "tester")
	if err != nil {
		t.Fatalf("URL 内容体检失败: %v", err)
	}
	if run.Kind != model.GeoAuditKindContent || run.RubricVersion != GeoAuditRubricContentV2 {
		t.Fatalf("内容体检 rubric 错误: %+v", run)
	}
	if len(view.Categories) != 1 || len(view.Categories[0].Checks) != 29 {
		t.Fatalf("29 因子应落 29 项，实际 %d 类 / %d 项", len(view.Categories), len(view.Categories[0].Checks))
	}
	if run.MaxScore <= 0 || run.Score <= 0 {
		t.Fatalf("内容体检分数异常: %d/%d", run.Score, run.MaxScore)
	}
	// 夹具页有 H1/列表/FAQ/外链/JSON-LD，至少这些应通过。
	passed := map[string]bool{}
	for _, ck := range view.Categories[0].Checks {
		passed[ck.Check] = ck.Pass
	}
	for _, name := range []string{"H1标题存在", "列表/表格", "FAQ问答段", "Schema JSON-LD"} {
		if !passed[name] {
			t.Errorf("%q 应通过", name)
		}
	}
	if !strings.Contains(run.Summary, "robots.txt/llms.txt") {
		t.Errorf("URL 体检应包含真实 robots/llms 检查说明: %s", run.Summary)
	}
}

func TestAuditService_Compare(t *testing.T) {
	db := auditServiceTestDB(t)
	repo := repository.NewGeoAuditRepositoryWithDB(db)
	ctx := context.Background()
	svc := NewAuditService(repo, nil)

	base := newSeededRun("https://example.com/p", "v2", 60, time.Now().Add(-2*time.Hour))
	baseItems := []*model.GeoAuditItem{
		{Category: "robots", CategoryLabel: "AI 爬虫访问", Check: "ROBOTS-EXISTS", Score: 0, Max: 4, Pass: false, Detail: "404", SortOrder: 0},
		{Category: "schema", CategoryLabel: "结构化数据", Check: "SCHEMA-ORG", Score: 4, Max: 4, Pass: true, SortOrder: 1},
	}
	if err := repo.Create(ctx, base, baseItems); err != nil {
		t.Fatalf("写基线失败: %v", err)
	}
	next := newSeededRun("https://example.com/p", "v2", 72, time.Now())
	nextItems := []*model.GeoAuditItem{
		{Category: "robots", CategoryLabel: "AI 爬虫访问", Check: "ROBOTS-EXISTS", Score: 4, Max: 4, Pass: true, SortOrder: 0},
		{Category: "schema", CategoryLabel: "结构化数据", Check: "SCHEMA-ORG", Score: 0, Max: 4, Pass: false, Detail: "缺少 Organization", SortOrder: 1},
	}
	if err := repo.Create(ctx, next, nextItems); err != nil {
		t.Fatalf("写复查失败: %v", err)
	}

	cmp, err := svc.Compare(ctx, base.ID, next.ID)
	if err != nil {
		t.Fatalf("对比失败: %v", err)
	}
	if !cmp.Comparable || cmp.ScoreDelta != 12 {
		t.Fatalf("对比结果异常: %+v", cmp)
	}
	if cmp.Fixed != 1 || cmp.Regressed != 1 {
		t.Fatalf("修复/回退计数错误: fixed=%d regressed=%d", cmp.Fixed, cmp.Regressed)
	}
	status := map[string]string{}
	for _, it := range cmp.Items {
		status[it.Check] = it.Status
	}
	if status["ROBOTS-EXISTS"] != "fixed" || status["SCHEMA-ORG"] != "regressed" {
		t.Fatalf("逐项状态错误: %+v", status)
	}

	// 不同 rubric 版本不可比。item 行会被 GORM 回填主键，不能跨 run 复用同一批指针。
	v3 := newSeededRun("https://example.com/p", "v3", 80, time.Now().Add(time.Hour))
	v3Items := []*model.GeoAuditItem{
		{Category: "robots", CategoryLabel: "AI 爬虫访问", Check: "ROBOTS-EXISTS", Score: 4, Max: 4, Pass: true, SortOrder: 0},
		{Category: "schema", CategoryLabel: "结构化数据", Check: "SCHEMA-ORG", Score: 4, Max: 4, Pass: true, SortOrder: 1},
	}
	if err := repo.Create(ctx, v3, v3Items); err != nil {
		t.Fatalf("写 v3 失败: %v", err)
	}
	cmp, err = svc.Compare(ctx, next.ID, v3.ID)
	if err != nil {
		t.Fatalf("跨版本对比失败: %v", err)
	}
	if cmp.Comparable || !strings.Contains(cmp.Reason, "版本不同") {
		t.Fatalf("跨版本应不可比: %+v", cmp)
	}
}

func TestAuditService_BuildReport(t *testing.T) {
	db := auditServiceTestDB(t)
	repo := repository.NewGeoAuditRepositoryWithDB(db)
	svc := NewAuditService(repo, nil)
	ctx := context.Background()

	run := newSeededRun("https://example.com/report", "v2", 72, time.Now())
	items := []*model.GeoAuditItem{
		{Category: "robots", CategoryLabel: "AI 爬虫访问", Check: "ROBOTS-EXISTS", Score: 4, Max: 4, Pass: true, SortOrder: 0},
		{Category: "schema", CategoryLabel: "结构化数据", Check: "SCHEMA-ORG", Score: 0, Max: 4, Pass: false, Detail: "缺少 Organization", Fix: "补 Organization JSON-LD", SortOrder: 1},
	}
	if err := repo.Create(ctx, run, items); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	html, err := svc.BuildReportHTML(ctx, run.ID)
	if err != nil {
		t.Fatalf("生成 HTML 失败: %v", err)
	}
	for _, want := range []string{"GEO 体检报告", "https://example.com/report", "ROBOTS-EXISTS", "SCHEMA-ORG", "补 Organization JSON-LD"} {
		if !strings.Contains(html, want) {
			t.Fatalf("报告缺少 %q", want)
		}
	}

	csv, err := svc.BuildReportCSV(ctx, run.ID)
	if err != nil {
		t.Fatalf("生成 CSV 失败: %v", err)
	}
	if !strings.HasPrefix(csv, "\ufeff") {
		t.Fatal("CSV 应带 UTF-8 BOM")
	}
	if !strings.Contains(csv, "ROBOTS-EXISTS") || !strings.Contains(csv, "SCHEMA-ORG") {
		t.Fatalf("CSV 缺少检查项:\n%s", csv)
	}
}

func newSeededRun(url, version string, score int, createdAt time.Time) *model.GeoAuditRun {
	return &model.GeoAuditRun{
		Kind: model.GeoAuditKindSite, Scope: model.GeoAuditScopePage, URL: url,
		Brand: "HiveMtk", RubricVersion: version, Score: score, MaxScore: 100, Grade: AuditGrade(score),
		Categories: datatypes.JSON([]byte(`[]`)), Fixes: datatypes.JSON([]byte(`[]`)),
		Summary: "seeded", Source: model.GeoAuditSourceManual, PageCount: 1, CreatedAt: createdAt,
	}
}
