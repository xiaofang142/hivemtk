package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"hivemtk-user/internal/geo/model"
)

const (
	defaultSiteAuditPages = 10
	maxSiteAuditPages     = 20
)

// runFullSiteAudit 按 sitemap 逐页审计后聚合。
//
// 出站边界：首页走 fetchSiteResources（页 + robots/llms 等，≤8 请求），
// 其余页只抓页面本身并复用首页的 robots/llms 结果；页数硬上限 20。
// sitemap 发现失败不报错，退化为单页并在 summary 里明说。
func (s *AuditService) runFullSiteAudit(ctx context.Context, rawURL, brand string, maxPages int, source, createdBy string) (*model.GeoAuditRun, *AuditReportView, error) {
	if err := siteAuditHostValidator(rawURL); err != nil {
		return nil, nil, err
	}
	urls, note, err := discoverSiteAuditURLs(ctx, rawURL, maxPages)
	if err != nil {
		return nil, nil, err
	}
	baseRes, err := fetchSiteResources(ctx, urls[0])
	if err != nil {
		return nil, nil, err
	}

	pages := make([]SiteAuditPageResult, 0, len(urls))
	success := 0
	for i, u := range urls {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		res := baseRes
		if i > 0 {
			status, final, header, body, fetchErr := siteFetch(ctx, u, "")
			if fetchErr != nil {
				pages = append(pages, SiteAuditPageResult{URL: u, Error: fetchErr.Error()})
				continue
			}
			copied := *baseRes
			copied.FinalURL = final
			copied.StatusCode = status
			copied.Header = header
			copied.Body = body
			res = &copied
		}
		rep := RunSiteAuditFromResources(res, brand)
		view := siteReportView(rep, brand)
		pages = append(pages, SiteAuditPageResult{
			URL: rep.URL, Score: rep.Score, MaxScore: rep.MaxScore, Grade: rep.Grade,
			Categories: view.Categories, Fixes: view.Fixes,
		})
		success++
	}
	if success == 0 {
		return nil, nil, fmt.Errorf("全部 %d 个页面抓取失败，未生成审计报告", len(urls))
	}

	agg := aggregateSiteAuditView(rawURL, brand, pages, note)
	run, err := s.persist(ctx, agg, len(urls), source, createdBy)
	if err != nil {
		return nil, nil, err
	}
	return run, agg, nil
}

func discoverSiteAuditURLs(ctx context.Context, rawURL string, maxPages int) ([]string, string, error) {
	if maxPages <= 0 {
		maxPages = defaultSiteAuditPages
	}
	if maxPages > maxSiteAuditPages {
		maxPages = maxSiteAuditPages
	}
	base, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, "", fmt.Errorf("仅支持 http/https 站点 URL")
	}
	root := base.Scheme + "://" + base.Host

	urls := []string{rawURL}
	for _, sm := range sitemapCandidates(ctx, root, base.Host) {
		locs, smErr := fetchSitemapLocs(ctx, sm, 0, base.Host)
		if smErr != nil {
			continue
		}
		urls = append(urls, locs...)
	}

	seen := map[string]bool{}
	out := make([]string, 0, maxPages)
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		// sitemap 常把首页写成 https://host/，而用户输入是 https://host；
		// 尾斜杠不参与去重，否则同一页会被审计两次。
		key := strings.TrimRight(u, "/")
		if u == "" || seen[key] {
			continue
		}
		pu, pErr := url.Parse(u)
		if pErr != nil || !sameAuditHost(pu.Host, base.Host) || (pu.Scheme != "http" && pu.Scheme != "https") {
			continue
		}
		if isAuditAssetURL(pu.Path) {
			continue
		}
		seen[key] = true
		out = append(out, u)
		if len(out) >= maxPages {
			break
		}
	}
	note := ""
	if len(out) <= 1 {
		note = "未发现可用 sitemap，已按单页审计。"
	}
	return out, note, nil
}

func sitemapCandidates(ctx context.Context, root, allowedHost string) []string {
	out := make([]string, 0, 4)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		// robots.txt 里的 Sitemap 行可能指向任意域，只接受本站主机，
		// 避免把审计抓取变成对第三方站点的探测。
		if pu, err := url.Parse(u); err != nil || !sameAuditHost(pu.Host, allowedHost) {
			return
		}
		for _, existing := range out {
			if existing == u {
				return
			}
		}
		out = append(out, u)
	}
	body, code, err := fetchAuditFile(ctx, siteAuditClient, root+"/robots.txt")
	if err == nil && code == 200 {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToLower(trimmed), "sitemap:") {
				continue
			}
			add(strings.TrimSpace(trimmed[len("sitemap:"):]))
		}
	}
	add(root + "/sitemap.xml")
	add(root + "/sitemap_index.xml")
	return out
}

type sitemapDoc struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

func fetchSitemapLocs(ctx context.Context, sitemapURL string, depth int, allowedHost string) ([]string, error) {
	if pu, err := url.Parse(strings.TrimSpace(sitemapURL)); err != nil || !sameAuditHost(pu.Host, allowedHost) {
		return nil, fmt.Errorf("sitemap 域名不属于本站: %s", sitemapURL)
	}
	body, code, err := fetchAuditFile(ctx, siteAuditClient, sitemapURL)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("sitemap %s HTTP %d", sitemapURL, code)
	}
	var doc sitemapDoc
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fmt.Errorf("解析 sitemap 失败: %w", err)
	}
	out := make([]string, 0, len(doc.URLs))
	for _, item := range doc.URLs {
		if loc := strings.TrimSpace(item.Loc); loc != "" {
			out = append(out, loc)
		}
	}
	if len(doc.Sitemaps) > 0 && depth < 1 {
		for i, sm := range doc.Sitemaps {
			if i >= 3 {
				break
			}
			locs, childErr := fetchSitemapLocs(ctx, strings.TrimSpace(sm.Loc), depth+1, allowedHost)
			if childErr == nil {
				out = append(out, locs...)
			}
		}
	}
	return out, nil
}

// sameAuditHost 判断两个主机是否属于同一站点（容忍 www 前缀差异）。
func sameAuditHost(a, b string) bool {
	normalize := func(h string) string {
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h)), "www.")
	}
	return normalize(a) != "" && normalize(a) == normalize(b)
}

func isAuditAssetURL(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".css", ".js", ".pdf", ".zip", ".mp4", ".mp3"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

type siteCheckAgg struct {
	score, max, passPages, totalPages int
	detail, fix, source               string
}

// aggregateSiteAuditView 把逐页结果聚合成一份全站报告：
// 分类分与检查项都按页求和；检查项的 pass 口径是“所有被审计页都通过”。
func aggregateSiteAuditView(rawURL, brand string, pages []SiteAuditPageResult, note string) *AuditReportView {
	catOrder := make([]string, 0)
	catLabels := map[string]string{}
	catScore := map[string]int{}
	catMax := map[string]int{}
	checkOrder := map[string][]string{}
	checkAgg := map[string]map[string]*siteCheckAgg{}

	totalScore, totalMax, okPages, failedPages := 0, 0, 0, 0
	for _, page := range pages {
		if page.Error != "" {
			failedPages++
			continue
		}
		okPages++
		totalScore += page.Score
		totalMax += page.MaxScore
		for _, cat := range page.Categories {
			if _, ok := checkAgg[cat.Category]; !ok {
				checkAgg[cat.Category] = map[string]*siteCheckAgg{}
				catOrder = append(catOrder, cat.Category)
			}
			if catLabels[cat.Category] == "" {
				catLabels[cat.Category] = cat.Label
			}
			catScore[cat.Category] += cat.Score
			catMax[cat.Category] += cat.Max
			for _, ck := range cat.Checks {
				agg, ok := checkAgg[cat.Category][ck.Check]
				if !ok {
					agg = &siteCheckAgg{}
					checkAgg[cat.Category][ck.Check] = agg
					checkOrder[cat.Category] = append(checkOrder[cat.Category], ck.Check)
				}
				agg.score += ck.Score
				agg.max += ck.Max
				agg.totalPages++
				if ck.Pass {
					agg.passPages++
				}
				if agg.detail == "" && ck.Detail != "" {
					agg.detail = ck.Detail
				}
				if agg.fix == "" && ck.Fix != "" {
					agg.fix = ck.Fix
				}
				if agg.source == "" && ck.Source != "" {
					agg.source = ck.Source
				}
			}
		}
	}

	cats := make([]SiteAuditCategory, 0, len(catOrder))
	fixes := make([]AuditFixView, 0)
	type fixWithLost struct {
		AuditFixView
		lost int
	}
	fixTmp := make([]fixWithLost, 0)
	for _, cat := range catOrder {
		c := SiteAuditCategory{
			Category: cat, Label: catLabels[cat],
			Score: catScore[cat], Max: catMax[cat],
			Checks: make([]SiteAuditCheck, 0, len(checkOrder[cat])),
		}
		for _, name := range checkOrder[cat] {
			agg := checkAgg[cat][name]
			pass := agg.totalPages > 0 && agg.score == agg.max
			detail := fmt.Sprintf("%d/%d 页通过", agg.passPages, agg.totalPages)
			if agg.detail != "" && agg.passPages == agg.totalPages {
				detail = agg.detail
			}
			c.Checks = append(c.Checks, SiteAuditCheck{
				Check: name, Score: agg.score, Max: agg.max, Pass: pass,
				Detail: detail, Fix: agg.fix, Source: agg.source,
			})
			if !pass {
				fixTmp = append(fixTmp, fixWithLost{
					AuditFixView{
						Category: c.Label, Check: name, Weight: agg.max,
						Detail: detail, Suggestion: agg.fix,
					},
					agg.max - agg.score,
				})
			}
		}
		cats = append(cats, c)
	}
	sort.SliceStable(fixTmp, func(i, j int) bool {
		if fixTmp[i].lost != fixTmp[j].lost {
			return fixTmp[i].lost > fixTmp[j].lost
		}
		return fixTmp[i].Check < fixTmp[j].Check
	})
	for _, f := range fixTmp {
		fixes = append(fixes, f.AuditFixView)
	}

	score, maxScore := totalScore, totalMax
	grade := AuditGrade(0)
	if maxScore > 0 {
		grade = AuditGrade(score * 100 / maxScore)
	}
	summary := fmt.Sprintf("全站 %d 页（成功 %d，失败 %d），总分 %d/%d（%s）。",
		len(pages), okPages, failedPages, score, maxScore, grade)
	if note != "" {
		summary += " " + note
	}
	return &AuditReportView{
		Kind: model.GeoAuditKindSite, Scope: model.GeoAuditScopeSite,
		URL: rawURL, Brand: brand, RubricVersion: GeoSiteAuditRubricV3,
		Score: score, MaxScore: maxScore, Grade: grade, Summary: summary,
		Categories: cats, Fixes: fixes, Pages: pages,
	}
}
