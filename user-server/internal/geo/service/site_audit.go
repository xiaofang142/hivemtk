package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ─────────────────────────────────────────────────────────────────────────────
// GEO 站点审计 v2（rubric v2 九类百分制）
//
// 吸纳自开源项目 geo-optimizer-skill v4.19.8（MIT，Auriti Labs）的审计语义与分值表，
// 以 Go 原生移植（无 Python 运行时依赖）。与既有 RunGEOAudit（25 因子内容审计）互补：
//   - RunGEOAudit：输入文章文本，审「内容可引用性」；
//   - RunSiteAudit（本文件）：活体抓取一个 URL，审「站点对 AI 引擎的技术可达性」。
//
// 出域纪律：抓取只 GET 用户显式提供的 URL 及其同域 robots.txt / llms.txt /
// llms-full.txt / ai.txt / summary.json（≤8 请求，各 10s 超时、2MB 上限）；
// 请求不携带任何品牌配置数据；无用户触发时零出站请求。
// ─────────────────────────────────────────────────────────────────────────────

// rubric v2 各类满分（与 geo-optimizer-skill SKILL.md 评分表逐项一致）
const (
	siteAuditMaxGoogleAI   = 20
	siteAuditMaxRobots     = 14
	siteAuditMaxSchema     = 14
	siteAuditMaxContent    = 14
	siteAuditMaxBrand      = 12
	siteAuditMaxMeta       = 11
	siteAuditMaxLLMs       = 6
	siteAuditMaxSignals    = 6
	siteAuditMaxAIDisc     = 3
	siteAuditTotalMaxScore = siteAuditMaxGoogleAI + siteAuditMaxRobots + siteAuditMaxSchema +
		siteAuditMaxContent + siteAuditMaxBrand + siteAuditMaxMeta + siteAuditMaxLLMs +
		siteAuditMaxSignals + siteAuditMaxAIDisc
)

// siteFetch 抓取边界
const (
	siteFetchTimeout   = 10 * time.Second
	siteFetchMaxBytes  = 2 << 20 // 2MB
	siteAuditUserAgent = "HiveMtk-GeoAudit/1.0 (+https://xapptool.cn)"
)

// SiteResources 一次站点审计抓取到的全部资源（评分函数的输入）
type SiteResources struct {
	FinalURL   string // 重定向后的最终 URL
	StatusCode int
	Body       string
	Header     http.Header

	RobotsStatus   int    // 0=未请求；200/404 等
	Robots         string // robots.txt 文本（缺失为空）
	LLMsStatus     int
	LLMsTxt        string
	LLMsFullStatus int
	LLMsFullTxt    string
	AITxtStatus    int
	AITxt          string
	SummaryStatus  int
	SummaryJSON    string
	MarkdownOK     bool // Accept: text/markdown 重取时返回非 HTML
}

// SiteAuditCheck 单项检查结果
type SiteAuditCheck struct {
	Check  string `json:"check"`
	Score  int    `json:"score"`
	Max    int    `json:"max"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
	Source string `json:"source,omitempty"` // Google 官方文档出处（google_ai 类携带）
	Fix    string `json:"fix,omitempty"`    // 未通过时的修复建议
}

// SiteAuditCategory 分类得分
type SiteAuditCategory struct {
	Category string           `json:"category"`
	Label    string           `json:"label"`
	Score    int              `json:"score"`
	Max      int              `json:"max"`
	Checks   []SiteAuditCheck `json:"checks"`
}

// GeoSiteAuditReport 站点审计报告（rubric v2）
type GeoSiteAuditReport struct {
	URL        string              `json:"url"`
	Score      int                 `json:"score"`
	Grade      string              `json:"grade"`
	MaxScore   int                 `json:"max_score"`
	Categories []SiteAuditCategory `json:"categories"`
	Fixes      []SiteAuditFix      `json:"fixes"`
	Summary    string              `json:"summary"`
}

// SiteAuditFix 修复建议（按分值降序，前端可直接渲染为任务清单）
type SiteAuditFix struct {
	Category   string `json:"category"`
	Check      string `json:"check"`
	Max        int    `json:"max"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion"`
}

// ── 活体抓取 ─────────────────────────────────────────────────────────────────

var siteAuditClient = &http.Client{Timeout: siteFetchTimeout}

// fetchSiteResources 抓取目标页与其同域 AI 可达性资源。任何单项失败只降级该项，不中断整体。
func fetchSiteResources(ctx context.Context, rawURL string) (*SiteResources, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, errors.New("URL 必须以 http(s):// 开头")
	}
	res := &SiteResources{}
	status, final, header, body, err := siteFetch(ctx, rawURL, "")
	if err != nil {
		return nil, fmt.Errorf("目标页抓取失败: %w", err)
	}
	res.FinalURL = final
	res.StatusCode = status
	res.Header = header
	res.Body = body

	u, err := url.Parse(final)
	if err != nil {
		return nil, fmt.Errorf("最终 URL 解析失败: %w", err)
	}
	root := u.Scheme + "://" + u.Host

	// markdown 协商：同 URL 带 Accept: text/markdown 重取，返回非 HTML 即视为支持
	_, _, _, mdBody, mdErr := siteFetch(ctx, final, "text/markdown")
	if mdErr == nil {
		lower := strings.ToLower(strings.TrimSpace(mdBody))
		res.MarkdownOK = strings.TrimSpace(mdBody) != "" &&
			!strings.HasPrefix(lower, "<!doctype html") && !strings.HasPrefix(lower, "<html")
	}

	get := func(path string) (int, string) {
		st, _, _, txt, err := siteFetch(ctx, root+path, "")
		if err != nil {
			return 0, ""
		}
		return st, txt
	}
	res.RobotsStatus, res.Robots = get("/robots.txt")
	res.LLMsStatus, res.LLMsTxt = get("/llms.txt")
	res.LLMsFullStatus, res.LLMsFullTxt = get("/llms-full.txt")
	res.AITxtStatus, res.AITxt = get("/ai.txt")
	res.SummaryStatus, res.SummaryJSON = get("/summary.json")
	return res, nil
}

// siteFetch 单请求：超时 + 2MB 上限 + 可选 Accept 头
func siteFetch(ctx context.Context, target, accept string) (status int, finalURL string, header http.Header, body string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, target, nil, "", err
	}
	req.Header.Set("User-Agent", siteAuditUserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := siteAuditClient.Do(req)
	if err != nil {
		return 0, target, nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, siteFetchMaxBytes+1))
	return resp.StatusCode, resp.Request.URL.String(), resp.Header, string(raw[:min(len(raw), siteFetchMaxBytes)]), readErr
}

// ── 评分（纯函数，可离线复用） ───────────────────────────────────────────────

// RunSiteAuditFromResources 对已抓取资源执行 rubric v2 评分。brand 仅用于页内匹配，不出站。
func RunSiteAuditFromResources(res *SiteResources, brand string) *GeoSiteAuditReport {
	page := parseSitePage(res.Body)
	cats := make([]SiteAuditCategory, 0, 9)
	cats = append(cats,
		auditGoogleAI(res, page),
		auditRobots(res),
		auditSchema(res, page),
		auditSiteContent(page),
		auditBrandEntity(res, page, brand),
		auditSiteMeta(page),
		auditLLMs(res),
		auditSiteSignals(res, page),
		auditAIDiscovery(res),
	)

	score := 0
	fixes := make([]SiteAuditFix, 0)
	for _, c := range cats {
		score += c.Score
		for _, ck := range c.Checks {
			if !ck.Pass {
				fixes = append(fixes, SiteAuditFix{Category: c.Label, Check: ck.Check, Max: ck.Max, Detail: ck.Detail, Suggestion: ck.Fix})
			}
		}
	}
	// 分值降序（同分按类别名稳定排序，保证输出可复现）
	for i := 1; i < len(fixes); i++ {
		for j := i; j > 0 && (fixes[j].Max > fixes[j-1].Max || (fixes[j].Max == fixes[j-1].Max && fixes[j].Category < fixes[j-1].Category)); j-- {
			fixes[j], fixes[j-1] = fixes[j-1], fixes[j]
		}
	}
	if len(fixes) > 15 {
		fixes = fixes[:15]
	}

	rep := &GeoSiteAuditReport{
		URL: res.FinalURL, Score: score, MaxScore: siteAuditTotalMaxScore,
		Grade: AuditGrade(score * 100 / siteAuditTotalMaxScore), Categories: cats, Fixes: fixes,
	}
	passed, total := 0, 0
	for _, c := range cats {
		for _, ck := range c.Checks {
			total++
			if ck.Pass {
				passed++
			}
		}
	}
	rep.Summary = fmt.Sprintf("通过 %d/%d 项，rubric v2 得分 %d/%d（%s）。", passed, total, rep.Score, rep.MaxScore, rep.Grade)
	if rep.Score*100/siteAuditTotalMaxScore < 60 {
		rep.Summary += " 建议优先修复分值最高的未通过项。"
	}
	return rep
}

// sitePage 页面解析产物（从 HTML 提取一次、各类检查共用）
type sitePage struct {
	title             string
	description       string
	robotsMeta        string
	viewport          bool
	og                int
	canonical         string
	lang              string
	h1                int
	h2                int
	text              string
	textLen           int
	jsonLDs           []string
	linkHrefs         []string
	samePageHashLinks int
	imgs              int
	imgsWithAlt       int
	hasRSS            bool
	hasAboutOrContact bool
}

var siteAuditScriptRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)

func parseSitePage(body string) *sitePage {
	p := &sitePage{}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return p
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "html":
				for _, a := range n.Attr {
					if a.Key == "lang" && strings.TrimSpace(a.Val) != "" {
						p.lang = strings.TrimSpace(a.Val)
					}
				}
			case "title":
				p.title = strings.TrimSpace(textContentOf(n))
			case "meta":
				name, prop, content := "", "", ""
				for _, a := range n.Attr {
					switch a.Key {
					case "name":
						name = strings.ToLower(a.Val)
					case "property":
						prop = strings.ToLower(a.Val)
					case "content":
						content = a.Val
					}
				}
				switch name {
				case "description":
					p.description = content
				case "robots":
					p.robotsMeta = strings.ToLower(content)
				case "viewport":
					p.viewport = content != ""
				}
				if prop == "og:title" || prop == "og:description" || prop == "og:image" {
					p.og++
				}
			case "link":
				rel, href := "", ""
				for _, a := range n.Attr {
					switch a.Key {
					case "rel":
						rel = strings.ToLower(a.Val)
					case "href":
						href = a.Val
					}
				}
				if rel == "canonical" && href != "" {
					p.canonical = href
				}
				if rel == "alternate" && (strings.Contains(href, "rss") || strings.Contains(href, "feed") || strings.Contains(href, "atom")) {
					p.hasRSS = true
				}
			case "h1":
				p.h1++
			case "h2":
				p.h2++
			case "img":
				p.imgs++
				for _, a := range n.Attr {
					if a.Key == "alt" && strings.TrimSpace(a.Val) != "" {
						p.imgsWithAlt++
						break
					}
				}
			case "a":
				for _, a := range n.Attr {
					if a.Key == "href" {
						h := strings.TrimSpace(a.Val)
						if h == "" || h == "#" || strings.HasPrefix(h, "javascript:") {
							break
						}
						if strings.HasPrefix(h, "#") || strings.HasPrefix(h, "/#") {
							p.samePageHashLinks++
						}
						p.linkHrefs = append(p.linkHrefs, h)
						l := strings.ToLower(h)
						if strings.Contains(l, "about") || strings.Contains(l, "contact") || strings.Contains(l, "关于") || strings.Contains(l, "联系") {
							p.hasAboutOrContact = true
						}
						break
					}
				}
			case "script":
				for _, a := range n.Attr {
					if a.Key == "type" && strings.Contains(strings.ToLower(a.Val), "ld+json") {
						p.jsonLDs = append(p.jsonLDs, textContentOf(n))
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	cleaned := siteAuditScriptRe.ReplaceAllString(body, " ")
	p.text = html.UnescapeString(cleaned)
	p.textLen = len([]rune(p.text))
	return p
}

func textContentOf(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return b.String()
}

// ── google_ai（20 分） ───────────────────────────────────────────────────────

const (
	gSrcTechnical = "https://developers.google.com/search/docs/essentials/technical"
	gSrcMeta      = "https://developers.google.com/search/docs/crawling-indexing/robots-meta-tag"
	gSrcCanonical = "https://developers.google.com/search/docs/crawling-indexing/consolidate-duplicate-urls"
	gSrcDates     = "https://developers.google.com/search/docs/appearance/publication-dates"
	gSrcHelpful   = "https://developers.google.com/search/docs/fundamentals/creating-helpful-content"
	gSrcJS        = "https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics"
	gSrcViewport  = "https://developers.google.com/search/docs/appearance/page-experience"
	gSrcSitemap   = "https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap"
)

func auditGoogleAI(res *SiteResources, p *sitePage) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "google_ai", Label: "Google AI 就绪", Max: siteAuditMaxGoogleAI}
	add := func(check string, score, max int, pass bool, detail, fix, source string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix, Source: source})
	}
	// G-INDEX：最终 200 + 无 noindex（meta robots / X-Robots-Tag）
	noindex := strings.Contains(p.robotsMeta, "noindex") || strings.Contains(strings.ToLower(res.Header.Get("X-Robots-Tag")), "noindex")
	giScore, giPass, giDetail := 5, true, "无 noindex 指令"
	if res.StatusCode != 200 {
		giScore, giPass = 0, false
		giDetail = fmt.Sprintf("最终状态码 %d（需 200）", res.StatusCode)
	} else if noindex {
		giScore, giPass = 0, false
		giDetail = "检测到 noindex（meta robots 或 X-Robots-Tag）"
	}
	add("G-INDEX", giScore, 5, giPass, giDetail, "移除 noindex，保证页面最终状态 200", gSrcTechnical)

	// G-SNIPPET：nosnippet / max-snippet:0
	bad := strings.Contains(p.robotsMeta, "nosnippet") || strings.Contains(p.robotsMeta, "max-snippet:0")
	add("G-SNIPPET", boolScore(!bad, 5), 5, !bad,
		map[bool]string{true: "无 nosnippet 限制", false: "检测到 nosnippet 或 max-snippet:0"}[!bad],
		"移除 nosnippet / max-snippet:0，保留摘要生成能力", gSrcMeta)

	// G-CANONICAL：唯一绝对地址 canonical
	canonicalOK := p.canonical != "" && strings.HasPrefix(p.canonical, "http")
	add("G-CANONICAL", boolScore(canonicalOK, 3), 3, canonicalOK,
		map[bool]string{true: "canonical=" + p.canonical, false: "缺少绝对地址 canonical"}[canonicalOK],
		"在 <head> 放置唯一、绝对地址且指向最终 URL 的 canonical", gSrcCanonical)

	// G-DATES：可见近期日期信号（与 datePublished 一致性属人工复核项）
	yearOK := siteHasRecentYear(p.text)
	add("G-DATES", boolScore(yearOK, 2), 2, yearOK,
		map[bool]string{true: "存在近期日期信号", false: "未见可见日期"}[yearOK],
		"JSON-LD datePublished 与可见日期一致且不写未来时间", gSrcDates)

	// G-BYLINE：作者信号（JSON-LD author 或可见署名）
	authorOK := siteJSONLDHasKey(p.jsonLDs, "author") || strings.Contains(p.text, "作者") || strings.Contains(p.text, "Author")
	add("G-BYLINE", boolScore(authorOK, 2), 2, authorOK,
		map[bool]string{true: "存在作者信号", false: "未见作者信息"}[authorOK],
		"补充可见作者署名或 JSON-LD author（可链到作者页）", gSrcHelpful)

	// G-LINKS：内链可爬（真实 href，无纯 # 路由）
	linksOK := len(p.linkHrefs) >= 2 && p.samePageHashLinks == 0
	add("G-LINKS", boolScore(linksOK, 1), 1, linksOK,
		fmt.Sprintf("%d 个链接（#/ 路由 %d 个）", len(p.linkHrefs), p.samePageHashLinks),
		"站内导航用真实 <a href>，避免纯 #/ 前端路由", gSrcJS)

	// G-VIEWPORT
	add("G-VIEWPORT", boolScore(p.viewport, 1), 1, p.viewport,
		map[bool]string{true: "存在 meta viewport", false: "缺少 meta viewport"}[p.viewport],
		"补充 <meta name=\"viewport\">", gSrcViewport)

	// G-SITEMAP：robots.txt 声明 Sitemap
	smOK := res.Robots != "" && siteRobotsDeclaresSitemap(res.Robots)
	add("G-SITEMAP", boolScore(smOK, 1), 1, smOK,
		map[bool]string{true: "robots.txt 已声明 Sitemap", false: "robots.txt 未声明 Sitemap"}[smOK],
		"robots.txt 追加一行 Sitemap: https://<域名>/sitemap.xml", gSrcSitemap)

	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── robots（14 分） ──────────────────────────────────────────────────────────

// siteSearchTierBots search 层 AI 爬虫（拦截 = 无资格出现在 AI 搜索答案）
var siteSearchTierBots = []string{"OAI-SearchBot", "PerplexityBot", "Claude-SearchBot"}

// siteTrainingTierBots training 层（禁不禁是策略选择，不打分，只在 detail 里提示）
var siteTrainingTierBots = []string{"GPTBot", "ClaudeBot", "Google-Extended"}

// robotsGroup 一组 User-agent 及其规则
type robotsGroup struct {
	agents      []string
	disallowAll bool
}

// parseRobotsGroups 解析 robots.txt 为组列表（连续 User-agent 行同组，遇到规则行结组）
func parseRobotsGroups(robots string) []robotsGroup {
	groups := []robotsGroup{}
	cur := robotsGroup{}
	sawRule := false
	flush := func() {
		if len(cur.agents) > 0 {
			groups = append(groups, cur)
		}
		cur = robotsGroup{}
		sawRule = false
	}
	for _, line := range strings.Split(robots, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "user-agent":
			if sawRule {
				flush()
			}
			cur.agents = append(cur.agents, val)
		case "disallow":
			sawRule = true
			if val == "/" {
				cur.disallowAll = true
			}
		case "allow":
			sawRule = true
		}
	}
	flush()
	return groups
}

// siteBotAllowed 判定某 bot 是否被允许抓根路径：bot 专属组优先，缺失回落 * 组；
// 根 Disallow: / 视为禁止。explicit=true 表示命中了该 bot 的专属组。
func siteBotAllowed(robots, bot string) (allowed bool, explicit bool) {
	if strings.TrimSpace(robots) == "" {
		return false, false // robots 缺失由 ROBOTS-EXISTS 单独扣分
	}
	groups := parseRobotsGroups(robots)
	pick := func(agent string) *robotsGroup {
		for i := range groups {
			for _, a := range groups[i].agents {
				if strings.EqualFold(a, agent) {
					return &groups[i]
				}
			}
		}
		return nil
	}
	if g := pick(bot); g != nil {
		return !g.disallowAll, true
	}
	if g := pick("*"); g != nil {
		return !g.disallowAll, false
	}
	return true, false
}

func siteRobotsDeclaresSitemap(robots string) bool {
	for _, line := range strings.Split(robots, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "sitemap:") {
			return true
		}
	}
	return false
}

func auditRobots(res *SiteResources) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "robots", Label: "AI 爬虫访问", Max: siteAuditMaxRobots}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	robotsOK := res.RobotsStatus == 200 && strings.TrimSpace(res.Robots) != ""
	add("ROBOTS-EXISTS", boolScore(robotsOK, 4), 4, robotsOK,
		fmt.Sprintf("robots.txt HTTP %d", res.RobotsStatus),
		"部署 robots.txt（可用 POST /geo/techconfig/robots 生成）")
	for _, bot := range siteSearchTierBots {
		allowed, explicit := siteBotAllowed(res.Robots, bot)
		detail := "未显式声明，按默认规则放行"
		if explicit {
			detail = map[bool]string{true: "显式放行", false: "显式 Disallow: /"}[allowed]
		}
		add("ROBOTS-ALLOW-"+bot, boolScore(allowed, 2), 2, allowed, detail,
			"robots.txt 为 "+bot+" 增加 User-agent 组并 Allow: /")
	}
	trainingBlocked := 0
	for _, bot := range siteTrainingTierBots {
		if allowed, _ := siteBotAllowed(res.Robots, bot); !allowed {
			trainingBlocked++
		}
	}
	smOK := res.Robots != "" && siteRobotsDeclaresSitemap(res.Robots)
	add("ROBOTS-SITEMAP", boolScore(smOK, 4), 4, smOK,
		map[bool]string{true: "已声明 Sitemap", false: "未声明 Sitemap"}[smOK],
		"robots.txt 追加 Sitemap: 行")
	if trainingBlocked > 0 {
		// 提示性信息（不打分）：training 层禁止是合法策略，不影响 AI 搜索可见性
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: "ROBOTS-TRAINING-POLICY", Score: 0, Max: 0, Pass: true,
			Detail: fmt.Sprintf("已禁止 %d 个 training 层爬虫（GPTBot/ClaudeBot/Google-Extended）——这是策略选择，不影响 AI 搜索可见性", trainingBlocked)})
	}
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── schema（14 分） ──────────────────────────────────────────────────────────

var siteValuableSchemas = []string{"WebSite", "Organization", "FAQPage", "Article", "Product", "BreadcrumbList", "HowTo", "Review"}

func auditSchema(res *SiteResources, p *sitePage) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "schema", Label: "结构化数据", Max: siteAuditMaxSchema}
	add := func(check string, score, max int, pass bool, detail, fix, source string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix, Source: source})
	}
	valid := 0
	types := map[string]bool{}
	names := []string{}
	for _, raw := range p.jsonLDs {
		var node map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &node); err != nil {
			continue
		}
		valid++
		collectSchemaTypesAndNames(node, types, &names)
	}
	add("SCHEMA-PRESENT", boolScore(valid > 0, 4), 4, valid > 0,
		fmt.Sprintf("%d 段 JSON-LD，可解析 %d 段", len(p.jsonLDs), valid),
		"在 <head> 注入合法 JSON-LD（type=application/ld+json）", gSrcMeta)

	valuable := 0
	for _, t := range siteValuableSchemas {
		if types[t] {
			valuable++
		}
	}
	add("SCHEMA-TYPES", boolScore(valuable > 0, 4), 4, valuable > 0,
		fmt.Sprintf("有价值类型 %d 个", valuable),
		"补充 WebSite/Organization/FAQPage/Article 等结构化类型", "")

	orgOK := types["Organization"] || types["WebSite"]
	add("SCHEMA-ORG", boolScore(orgOK, 4), 4, orgOK,
		map[bool]string{true: "存在 Organization/WebSite", false: "缺少 Organization/WebSite"}[orgOK],
		"注入 Organization JSON-LD（name/logo/sameAs）", "")

	visible := false
	for _, n := range names {
		if n != "" && strings.Contains(p.text, n) {
			visible = true
			break
		}
	}
	add("G-SD-VISIBLE", boolScore(visible, 2), 2, visible,
		map[bool]string{true: "JSON-LD name/headline 出现在可见文本", false: "JSON-LD 名称未出现在可见文本"}[visible],
		"让结构化数据的 name/headline 与页面可见标题一致（SP 政策要求）",
		"https://developers.google.com/search/docs/appearance/structured-data/sd-policies")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

func collectSchemaTypesAndNames(node map[string]any, types map[string]bool, names *[]string) {
	if t, _ := node["@type"].(string); t != "" {
		types[t] = true
	}
	if n, _ := node["name"].(string); n != "" {
		*names = append(*names, n)
	}
	if h, _ := node["headline"].(string); h != "" {
		*names = append(*names, h)
	}
	if g, _ := node["@graph"].([]any); g != nil {
		for _, item := range g {
			if m, ok := item.(map[string]any); ok {
				collectSchemaTypesAndNames(m, types, names)
			}
		}
	}
}

// ── content（14 分） ─────────────────────────────────────────────────────────

func auditSiteContent(p *sitePage) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "content", Label: "内容质量", Max: siteAuditMaxContent}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	hOK := p.h1 == 1 && p.h2 >= 1
	add("CONTENT-HEADINGS", boolScore(hOK, 2), 2, hOK,
		fmt.Sprintf("h1=%d h2=%d", p.h1, p.h2), "一个 h1 概括主题，正文用 h2 分节")
	wOK := p.textLen >= 500
	add("CONTENT-LENGTH", boolScore(wOK, 3), 3, wOK, fmt.Sprintf("正文约 %d 字符", p.textLen), "扩写至 500 字符以上")
	hasStats := siteHasStats(p.text)
	add("CONTENT-EVIDENCE", boolScore(hasStats, 3), 3, hasStats,
		map[bool]string{true: "含量化数据", false: "无百分比/数值等量化证据"}[hasStats],
		"补充百分比、数值等量化表述（Princeton GEO：Add Statistics）")
	extLinks := 0
	for _, h := range p.linkHrefs {
		if strings.HasPrefix(h, "http") {
			extLinks++
		}
	}
	extOK := extLinks >= 1
	add("CONTENT-CITATIONS", boolScore(extOK, 2), 2, extOK,
		fmt.Sprintf("外链 %d 个", extLinks), "为关键论断补充权威外部来源（Princeton GEO：Cite Sources）")
	intOK := len(p.linkHrefs) >= 2
	add("CONTENT-LINKS", boolScore(intOK, 2), 2, intOK, fmt.Sprintf("链接总数 %d", len(p.linkHrefs)), "补充站内互链形成主题簇")
	altOK := p.imgs == 0 || p.imgsWithAlt*2 >= p.imgs
	add("CONTENT-IMG-ALT", boolScore(altOK, 2), 2, altOK,
		fmt.Sprintf("img %d 张，带 alt %d 张", p.imgs, p.imgsWithAlt), "为图片补齐 alt 文本")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── brand_entity（12 分） ────────────────────────────────────────────────────

func auditBrandEntity(res *SiteResources, p *sitePage, brand string) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "brand_entity", Label: "品牌实体", Max: siteAuditMaxBrand}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	if strings.TrimSpace(brand) == "" {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: "BRAND-SKIPPED", Score: siteAuditMaxBrand, Max: siteAuditMaxBrand, Pass: true,
			Detail: "未提供品牌名，品牌类检查整体放行（不影响总分）"})
		cat.Score = cat.Max
		return cat
	}
	inTitle := strings.Contains(p.title, brand)
	add("BRAND-IN-TITLE", boolScore(inTitle, 3), 3, inTitle,
		map[bool]string{true: "title 含品牌名", false: "title 不含品牌名"}[inTitle], "title 加入品牌名")
	inBody := strings.Contains(p.text, brand)
	add("BRAND-IN-BODY", boolScore(inBody, 3), 3, inBody,
		map[bool]string{true: "正文含品牌名", false: "正文不含品牌名"}[inBody], "正文自然融入品牌名 2-6 次")
	add("BRAND-IDENTITY-PAGES", boolScore(p.hasAboutOrContact, 2), 2, p.hasAboutOrContact,
		map[bool]string{true: "存在 about/contact 入口", false: "缺少关于/联系页面入口"}[p.hasAboutOrContact],
		"提供关于我们/联系方式页面入口（实体可信信号）")
	sameAs := false
	for _, raw := range p.jsonLDs {
		var node map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &node) != nil {
			continue
		}
		if v, _ := node["sameAs"].([]any); len(v) > 0 {
			sameAs = true
		}
		if _, ok := node["logo"]; ok && schemaTypesHas(node, "Organization") {
			sameAs = true
		}
	}
	add("BRAND-SAMEAS", boolScore(sameAs, 4), 4, sameAs,
		map[bool]string{true: "存在 sameAs/logo 实体锚点", false: "缺少 sameAs 实体锚点"}[sameAs],
		"Organization JSON-LD 补充 sameAs（社媒/百科一致地址）")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

func schemaTypesHas(node map[string]any, want string) bool {
	if t, _ := node["@type"].(string); t == want {
		return true
	}
	if g, _ := node["@graph"].([]any); g != nil {
		for _, item := range g {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["@type"].(string); t == want {
					return true
				}
			}
		}
	}
	return false
}

// ── meta（11 分） ────────────────────────────────────────────────────────────

func auditSiteMeta(p *sitePage) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "meta", Label: "元信息", Max: siteAuditMaxMeta}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	tl := len([]rune(p.title))
	titleOK := tl > 0 && tl <= 80
	add("META-TITLE", boolScore(titleOK, 3), 3, titleOK, fmt.Sprintf("title %d 字符", tl), "title 控制在 80 字符内且含关键词")
	dl := len([]rune(p.description))
	descOK := dl >= 80 && dl <= 300
	add("META-DESCRIPTION", boolScore(descOK, 4), 4, descOK, fmt.Sprintf("meta description %d 字符", dl), "meta description 控制在 80-300 字符")
	add("META-OG", boolScore(p.og > 0, 4), 4, p.og > 0,
		fmt.Sprintf("Open Graph 标签 %d 个", p.og),
		"补充 og:title/og:description/og:image")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── llms（6 分） ─────────────────────────────────────────────────────────────

func auditLLMs(res *SiteResources) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "llms", Label: "llms.txt", Max: siteAuditMaxLLMs}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	llmsOK := res.LLMsStatus == 200 && strings.TrimSpace(res.LLMsTxt) != ""
	add("LLMS-EXISTS", boolScore(llmsOK, 3), 3, llmsOK,
		fmt.Sprintf("/llms.txt HTTP %d", res.LLMsStatus),
		"部署 /llms.txt（可用 POST /geo/techconfig/llms-txt 生成）")
	structOK := llmsOK && validLLMsStructure(res.LLMsTxt)
	add("LLMS-STRUCTURE", boolScore(structOK, 2), 2, structOK,
		map[bool]string{true: "H1+blockquote/H2 结构合法", false: "结构不合法（需 H1 开头，建议 blockquote 描述 + H2 分节）"}[structOK],
		"llms.txt 遵循 llmstxt.org 规范：H1 → blockquote → H2 sections")
	fullOK := res.LLMsFullStatus == 200 && strings.TrimSpace(res.LLMsFullTxt) != ""
	add("LLMS-FULL", boolScore(fullOK, 1), 1, fullOK,
		fmt.Sprintf("/llms-full.txt HTTP %d", res.LLMsFullStatus),
		"补充 /llms-full.txt 全文版")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// validLLMsStructure 与 renderLLMsTxt 生成端对齐的最小结构校验：H1 开头，次非空行为 blockquote 或 H2
func validLLMsStructure(txt string) bool {
	lines := strings.Split(strings.TrimSpace(txt), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		return false
	}
	for _, l := range lines[1:] {
		s := strings.TrimSpace(l)
		if s == "" {
			continue
		}
		return strings.HasPrefix(s, ">") || strings.HasPrefix(s, "##")
	}
	return false
}

// ── signals（6 分） ──────────────────────────────────────────────────────────

func auditSiteSignals(res *SiteResources, p *sitePage) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "signals", Label: "时效与信号", Max: siteAuditMaxSignals}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	add("SIGNALS-LANG", boolScore(p.lang != "", 2), 2, p.lang != "",
		map[bool]string{true: "html lang=" + p.lang, false: "html 缺少 lang 属性"}[p.lang != ""],
		"<html> 标注 lang")
	add("SIGNALS-RSS", boolScore(p.hasRSS, 2), 2, p.hasRSS,
		map[bool]string{true: "存在 RSS/Atom feed", false: "未发现 RSS/Atom feed"}[p.hasRSS],
		"提供 RSS/Atom 订阅源（新鲜度信号）")
	fresh := siteHasRecentYear(p.text)
	add("SIGNALS-FRESHNESS", boolScore(fresh, 2), 2, fresh,
		map[bool]string{true: "含近两年年份", false: "未见近期年份"}[fresh],
		"标注内容更新年份（2025/2026）")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── ai_discovery（3 分） ─────────────────────────────────────────────────────

func auditAIDiscovery(res *SiteResources) SiteAuditCategory {
	cat := SiteAuditCategory{Category: "ai_discovery", Label: "AI 发现端点", Max: siteAuditMaxAIDisc}
	add := func(check string, score, max int, pass bool, detail, fix string) {
		cat.Checks = append(cat.Checks, SiteAuditCheck{Check: check, Score: score, Max: max, Pass: pass, Detail: detail, Fix: fix})
	}
	aiOK := res.AITxtStatus == 200 && strings.TrimSpace(res.AITxt) != ""
	add("AI-TXT", boolScore(aiOK, 1), 1, aiOK, fmt.Sprintf("/ai.txt HTTP %d", res.AITxtStatus), "部署 /ai.txt（AI 爬虫偏好声明）")
	sumOK := res.SummaryStatus == 200 && strings.TrimSpace(res.SummaryJSON) != ""
	add("SUMMARY-JSON", boolScore(sumOK, 1), 1, sumOK, fmt.Sprintf("/summary.json HTTP %d", res.SummaryStatus), "部署 /summary.json 站点摘要")
	add("MARKDOWN-NEGOTIATION", boolScore(res.MarkdownOK, 1), 1, res.MarkdownOK,
		map[bool]string{true: "支持 Accept: text/markdown", false: "不支持 markdown 协商"}[res.MarkdownOK],
		"对 Accept: text/markdown 返回 Markdown 版本（可选进阶）")
	cat.Score = sumChecks(cat.Checks)
	return cat
}

// ── 小工具 ───────────────────────────────────────────────────────────────────

func sumChecks(cs []SiteAuditCheck) int {
	n := 0
	for _, c := range cs {
		n += c.Score
	}
	return n
}

func boolScore(pass bool, score int) int {
	if pass {
		return score
	}
	return 0
}

var siteYearRe = regexp.MustCompile(`20(2[4-9]|3\d)`)

func siteHasRecentYear(text string) bool { return siteYearRe.MatchString(text) }

var siteStatsRe = regexp.MustCompile(`(\d+(\.\d+)?\s*%|\d+\s*倍|\d+\.\d+\s*[万亿]|\b\d{2,}\b)`)

func siteHasStats(text string) bool { return siteStatsRe.MatchString(text) }

func siteJSONLDHasKey(jsonLDs []string, key string) bool {
	for _, raw := range jsonLDs {
		var node map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &node) != nil {
			continue
		}
		if _, ok := node[key]; ok {
			return true
		}
		if g, _ := node["@graph"].([]any); g != nil {
			for _, item := range g {
				if m, ok := item.(map[string]any); ok {
					if _, ok := m[key]; ok {
						return true
					}
				}
			}
		}
	}
	return false
}

// RunSiteAudit 活体审计入口：抓取 + 评分
func (s *TechConfigService) RunSiteAudit(ctx context.Context, rawURL, brand string) (*GeoSiteAuditReport, error) {
	res, err := fetchSiteResources(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return RunSiteAuditFromResources(res, brand), nil
}
