package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fullScoreHTML 满分页夹具：覆盖全部可页内判定的检查项
const fullScoreHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>开源私域营销操作系统 HiveMTK - AI 客服与智能体</title>
<meta name="description" content="HiveMTK 是开源私域部署的 AI 营销操作系统，七端打通、AI 真自主、数据不出域，2026 年持续演进。这里是一段超过八十字符的描述文本用于满足 meta description 长度检查的达标条件。">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta property="og:title" content="HiveMTK 官网">
<meta property="og:description" content="开源私域营销操作系统">
<meta property="og:image" content="https://example.com/cover.png">
<link rel="canonical" href="https://example.com/">
<link rel="alternate" type="application/rss+xml" href="https://example.com/feed.xml">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"HiveMTK","logo":"https://example.com/logo.png","sameAs":["https://github.com/x/h"]}</script>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"HiveMTK：开源私域营销操作系统","author":{"@type":"Person","name":"HiveMTK Team"}}</script>
</head>
<body>
<h1>HiveMTK：开源私域营销操作系统</h1>
<h2>核心能力</h2>
<p>HiveMTK 覆盖 41 个工具，2026 年实测转化率提升 35%，客户说"再考虑一下"时 AI 能分辨真忙还是婉拒。</p>
<h2>部署方式</h2>
<p>支持本地部署与云端版，参见 <a href="https://github.com/xiaofang142/hivemtk">开源仓库</a> 与 <a href="/docs">使用文档</a>。</p>
<p>了解更多请访问 <a href="/about">关于我们</a> 或 <a href="/contact">联系方式</a>。</p>
<img src="/a.png" alt="架构图">
</body>
</html>`

const fullScoreRobots = `User-agent: *
Disallow: /admin/

User-agent: OAI-SearchBot
Allow: /

User-agent: PerplexityBot
Allow: /

User-agent: Claude-SearchBot
Allow: /

User-agent: GPTBot
Disallow: /

Sitemap: https://example.com/sitemap.xml
`

func fullScoreResources(body string) *SiteResources {
	return &SiteResources{
		FinalURL: "https://example.com/", StatusCode: 200, Body: body, Header: http.Header{},
		RobotsStatus: 200, Robots: fullScoreRobots,
		LLMsStatus: 200, LLMsTxt: "# HiveMTK\n\n> 开源私域营销操作系统\n\n## Docs\n\n- [官网](https://example.com/)\n",
		LLMsFullStatus: 200, LLMsFullTxt: "# HiveMTK\n\n全文\n",
		AITxtStatus: 200, AITxt: "# ai.txt\n",
		SummaryStatus: 200, SummaryJSON: `{"name":"HiveMTK"}`,
		MarkdownOK: true,
	}
}

func catByLabel(rep *GeoSiteAuditReport, category string) *SiteAuditCategory {
	for i := range rep.Categories {
		if rep.Categories[i].Category == category {
			return &rep.Categories[i]
		}
	}
	return nil
}

func checkByName(c *SiteAuditCategory, check string) *SiteAuditCheck {
	for i := range c.Checks {
		if c.Checks[i].Check == check {
			return &c.Checks[i]
		}
	}
	return nil
}

// T009-1 满分夹具：页内可判定的项全部通过，总分为满分
func TestSiteAuditFullScore(t *testing.T) {
	rep := RunSiteAuditFromResources(fullScoreResources(fullScoreHTML), "HiveMTK")
	if rep.Score != rep.MaxScore {
		t.Fatalf("expected full score %d, got %d\nsummary=%s", rep.MaxScore, rep.Score, rep.Summary)
		for _, c := range rep.Categories {
			for _, ck := range c.Checks {
				if !ck.Pass {
					t.Errorf("expected pass: %s/%s detail=%s", c.Category, ck.Check, ck.Detail)
				}
			}
		}
	}
	if len(rep.Fixes) != 0 {
		t.Fatalf("expected no fixes, got %d: %+v", len(rep.Fixes), rep.Fixes)
	}
}

// T009-2 逐类缺陷：robots 禁 PerplexityBot / 缺 llms / 缺 viewport / 品牌缺失
func TestSiteAuditDefects(t *testing.T) {
	res := fullScoreResources(strings.ReplaceAll(fullScoreHTML, "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">", ""))
	res.Robots = strings.Replace(fullScoreRobots, "User-agent: PerplexityBot\nAllow: /\n", "User-agent: PerplexityBot\nDisallow: /\n", 1)
	res.LLMsStatus, res.LLMsTxt, res.LLMsFullStatus, res.LLMsFullTxt = 404, "", 404, ""
	rep := RunSiteAuditFromResources(res, "HiveMTK")

	if rep.Score >= rep.MaxScore {
		t.Fatalf("expected deductions, got %d/%d", rep.Score, rep.MaxScore)
	}
	robotsCat := catByLabel(rep, "robots")
	if got := checkByName(robotsCat, "ROBOTS-ALLOW-PerplexityBot"); got == nil || got.Pass {
		t.Fatalf("PerplexityBot should fail: %+v", got)
	}
	if got := checkByName(robotsCat, "ROBOTS-ALLOW-OAI-SearchBot"); got == nil || !got.Pass {
		t.Fatalf("OAI-SearchBot should pass: %+v", got)
	}
	// training 层禁用只提示不打分：GPTBot 禁止不应产生 fixes
	for _, f := range rep.Fixes {
		if strings.Contains(f.Check, "GPTBot") {
			t.Fatalf("training-tier bot must not generate fix: %+v", f)
		}
	}
	if got := checkByName(catByLabel(rep, "google_ai"), "G-VIEWPORT"); got == nil || got.Pass {
		t.Fatalf("G-VIEWPORT should fail: %+v", got)
	}
	if got := checkByName(catByLabel(rep, "llms"), "LLMS-EXISTS"); got == nil || got.Pass {
		t.Fatalf("LLMS-EXISTS should fail: %+v", got)
	}
	if len(rep.Fixes) == 0 {
		t.Fatal("expected fixes")
	}
	// fixes 按分值降序
	for i := 1; i < len(rep.Fixes); i++ {
		if rep.Fixes[i].Max > rep.Fixes[i-1].Max {
			t.Fatalf("fixes not sorted by max desc at %d: %+v", i, rep.Fixes)
		}
	}
}

// T009-3 robots 三层分级语义：无 * 组无专属组 → 放行；* 组禁根 → 未专属声明时按 * 禁止
func TestSiteBotAllowed(t *testing.T) {
	cases := []struct {
		name     string
		robots   string
		bot      string
		allowed  bool
		explicit bool
	}{
		{"专属放行压过星号禁根", "User-agent: *\nDisallow: /\n\nUser-agent: OAI-SearchBot\nAllow: /\n", "OAI-SearchBot", true, true},
		{"星号禁根回落生效", "User-agent: *\nDisallow: /\n", "PerplexityBot", false, false},
		{"专属禁根", "User-agent: PerplexityBot\nDisallow: /\n", "PerplexityBot", false, true},
		{"空 robots 不判定", "", "OAI-SearchBot", false, false},
		{"无任何组默认放行", "Sitemap: https://e.com/s.xml\n", "Claude-SearchBot", true, false},
		{"大小写不敏感", "user-agent: oai-searchbot\ndisallow: /\n", "OAI-SearchBot", false, true},
	}
	for _, tc := range cases {
		gotAllowed, gotExplicit := siteBotAllowed(tc.robots, tc.bot)
		if gotAllowed != tc.allowed || gotExplicit != tc.explicit {
			t.Fatalf("%s: got (%v,%v) want (%v,%v)", tc.name, gotAllowed, gotExplicit, tc.allowed, tc.explicit)
		}
	}
}

// T009-4 llms.txt 结构校验
func TestValidLLMsStructure(t *testing.T) {
	good := []string{
		"# Site\n\n> desc\n\n## Docs\n\n- [a](https://x)\n",
		"# Site\n\n## Docs\n",
	}
	bad := []string{
		"## Docs\n",      // 无 H1
		"# Site\nbody\n", // H1 后跟普通段落
		"",               // 空
	}
	for _, g := range good {
		if !validLLMsStructure(g) {
			t.Fatalf("expected valid: %q", g)
		}
	}
	for _, b := range bad {
		if validLLMsStructure(b) {
			t.Fatalf("expected invalid: %q", b)
		}
	}
}

// T009-5 恶意输入不 panic：坏 JSON-LD、空 body、超长行
func TestSiteAuditMalformed(t *testing.T) {
	res := fullScoreResources(`<html><head><script type="application/ld+json">{bad json</script></head><body>` +
		strings.Repeat("<p>字</p>", 5000) + `</body></html>`)
	res.RobotsStatus, res.Robots = 404, ""
	rep := RunSiteAuditFromResources(res, "H")
	if rep == nil || rep.Score < 0 || rep.Score > rep.MaxScore {
		t.Fatalf("report out of range: %+v", rep)
	}
	empty := RunSiteAuditFromResources(&SiteResources{FinalURL: "https://x/"}, "")
	if empty == nil {
		t.Fatal("nil report for empty resources")
	}
}

// T009-6 未提供品牌名时品牌类整体放行不扣分
func TestSiteAuditBrandSkipped(t *testing.T) {
	rep := RunSiteAuditFromResources(fullScoreResources(fullScoreHTML), "")
	be := catByLabel(rep, "brand_entity")
	if be.Score != be.Max {
		t.Fatalf("brand category should be fully credited, got %d/%d", be.Score, be.Max)
	}
}

// T006 活体抓取：本地 httptest 全资源 + 请求预算（≤8）与 2MB 上限
func TestFetchSiteResources(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/":
			if r.Header.Get("Accept") == "text/markdown" {
				_, _ = w.Write([]byte("# 页面 Markdown 版\n"))
				return
			}
			_, _ = w.Write([]byte(fullScoreHTML))
		case "/robots.txt":
			_, _ = w.Write([]byte(fullScoreRobots))
		case "/llms.txt":
			_, _ = w.Write([]byte("# T\n\n> d\n"))
		case "/llms-full.txt":
			_, _ = w.Write([]byte("# T\n\nfull\n"))
		case "/ai.txt":
			_, _ = w.Write([]byte("ok"))
		case "/summary.json":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res, err := fetchSiteResources(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if hits > 8 {
		t.Fatalf("request budget exceeded: %d", hits)
	}
	if res.StatusCode != 200 || res.RobotsStatus != 200 || !res.MarkdownOK {
		t.Fatalf("unexpected resources: %+v", res)
	}
	rep := RunSiteAuditFromResources(res, "")
	if rep.Score <= 0 {
		t.Fatalf("expected positive score, got %d", rep.Score)
	}

	// 不可达 URL → 返回错误（400 语义由 controller 层转换）
	if _, err := fetchSiteResources(context.Background(), "http://127.0.0.1:1/"); err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
