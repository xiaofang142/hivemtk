package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"

	xhtml "golang.org/x/net/html"
)

// 审计 rubric 版本。总分分母随版本变化，compare 只在同版本内成立。
const (
	GeoSiteAuditRubricV2    = "v2"         // 历史报告（9 类 / 100 分），保留用于对比
	GeoSiteAuditRubricV3    = "v3"         // 新增 product_clarity（10 类 / 112 分）
	GeoAuditRubricContentV1 = "content-v1" // 历史内容报告（25 因子 / 88 分权重）
	GeoAuditRubricContentV2 = "content-v2" // 新增产品叙事四问（29 因子 / 100 分权重）
)

// AuditFixView 审计修复项的归一形状（站点 rubric 与 25 因子共用）。
type AuditFixView struct {
	Category   string `json:"category"`
	Check      string `json:"check"`
	Weight     int    `json:"weight"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion"`
}

// AuditReportView 一次体检的归一报告（落库快照与 API 响应共用）。
type AuditReportView struct {
	Kind          string                `json:"kind"`
	Scope         string                `json:"scope"`
	URL           string                `json:"url"`
	Brand         string                `json:"brand"`
	RubricVersion string                `json:"rubric_version"`
	Score         int                   `json:"score"`
	MaxScore      int                   `json:"max_score"`
	Grade         string                `json:"grade"`
	Summary       string                `json:"summary"`
	Categories    []SiteAuditCategory   `json:"categories"`
	Fixes         []AuditFixView        `json:"fixes"`
	Pages         []SiteAuditPageResult `json:"pages,omitempty"`
}

// SiteAuditPageResult 全站审计的逐页结果。
type SiteAuditPageResult struct {
	URL        string              `json:"url"`
	Score      int                 `json:"score"`
	MaxScore   int                 `json:"max_score"`
	Grade      string              `json:"grade"`
	Categories []SiteAuditCategory `json:"categories,omitempty"`
	Fixes      []AuditFixView      `json:"fixes,omitempty"`
	Error      string              `json:"error,omitempty"`
}

// ContentAuditInput 内容体检入参。Content 为空且 URL 非空时自动抓取正文。
type ContentAuditInput struct {
	URL          string
	Title        string
	Content      string
	MetaDesc     string
	SchemaJSONLD string
	Brand        string
	Live         bool
}

// AuditCompareCategory 分类分对比。
type AuditCompareCategory struct {
	Category  string `json:"category"`
	Label     string `json:"label"`
	FromScore int    `json:"from_score"`
	ToScore   int    `json:"to_score"`
	FromMax   int    `json:"from_max"`
	ToMax     int    `json:"to_max"`
	Delta     int    `json:"delta"`
}

// AuditCompareItem 逐检查项对比。
type AuditCompareItem struct {
	Category  string `json:"category"`
	Check     string `json:"check"`
	Status    string `json:"status"` // same_pass / still_failing / fixed / regressed / added / removed
	FromScore int    `json:"from_score"`
	ToScore   int    `json:"to_score"`
	Max       int    `json:"max"`
	Detail    string `json:"detail,omitempty"`
}

// AuditCompareResult 两次体检的同口径对比。Comparable=false 时只返回原因。
type AuditCompareResult struct {
	Comparable  bool                   `json:"comparable"`
	Reason      string                 `json:"reason,omitempty"`
	From        *model.GeoAuditRun     `json:"from"`
	To          *model.GeoAuditRun     `json:"to"`
	ScoreDelta  int                    `json:"score_delta"`
	Categories  []AuditCompareCategory `json:"categories"`
	Items       []AuditCompareItem     `json:"items"`
	Fixed       int                    `json:"fixed"`
	Regressed   int                    `json:"regressed"`
	Added       int                    `json:"added"`
	Removed     int                    `json:"removed"`
	StillFailed int                    `json:"still_failing"`
}

// AuditService 审计中心的编排层：
//  1. 调既有 rubric（站点 v2 / 内容 25 因子）；
//  2. 归一化后落 run+item；
//  3. 提供历史、对比与可打印报告。
//
// AuditRunQuery 仓储查询 DTO 的对外别名：controller 只依赖 service 层。
type AuditRunQuery = repository.AuditRunQuery

type AuditService struct {
	repo   repository.GeoAuditRepository
	tech   *TechConfigService
	alerts *AlertService
}

func NewAuditService(repo repository.GeoAuditRepository, alerts *AlertService) *AuditService {
	return &AuditService{repo: repo, tech: NewTechConfigService(), alerts: alerts}
}

// RunSiteAudit 站点体检：scope=page 单页；scope=site 按 sitemap 逐页审计后聚合。
func (s *AuditService) RunSiteAudit(ctx context.Context, rawURL, brand, scope string, maxPages int, source, createdBy string) (*model.GeoAuditRun, *AuditReportView, error) {
	if normalizeAuditScope(scope) == model.GeoAuditScopeSite {
		return s.runFullSiteAudit(ctx, rawURL, brand, maxPages, source, createdBy)
	}
	report, err := s.tech.RunSiteAudit(ctx, rawURL, brand)
	if err != nil {
		return nil, nil, err
	}
	view := siteReportView(report, brand)
	run, err := s.persist(ctx, view, 1, source, createdBy)
	if err != nil {
		return nil, nil, err
	}
	return run, view, nil
}

// RunContentAudit 对给定内容跑 25 因子；Content 为空时从 URL 抓取。
func (s *AuditService) RunContentAudit(ctx context.Context, in ContentAuditInput, source, createdBy string) (*model.GeoAuditRun, *AuditReportView, error) {
	var report *GeoAuditReport
	var err error
	if strings.TrimSpace(in.Content) == "" {
		if strings.TrimSpace(in.URL) == "" {
			return nil, nil, errors.New("content 与 url 至少提供一个")
		}
		report, err = s.auditFromURL(ctx, in.URL, in.Brand)
	} else if in.Live && strings.TrimSpace(in.URL) != "" {
		report = s.tech.RunGEOAuditLive(ctx, in.URL, in.Title, in.Content, in.MetaDesc, in.SchemaJSONLD, in.Brand)
	} else {
		report = s.tech.RunGEOAudit(in.URL, in.Title, in.Content, in.MetaDesc, in.SchemaJSONLD, in.Brand)
	}
	if err != nil {
		return nil, nil, err
	}
	view := contentReportView(report, in.Brand)
	run, err := s.persist(ctx, view, 1, source, createdBy)
	if err != nil {
		return nil, nil, err
	}
	return run, view, nil
}

// auditFromURL 抓一次页面（复用站点审计的出站边界），把 HTML 转成
// 可引用性审计需要的 Markdown 形状，并用真实 robots/llms 结果覆盖
// RunGEOAudit 里两个“默认放行”的因子。
func (s *AuditService) auditFromURL(ctx context.Context, rawURL, brand string) (*GeoAuditReport, error) {
	if err := siteAuditHostValidator(rawURL); err != nil {
		return nil, err
	}
	res, err := fetchSiteResources(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	page := parseSitePage(res.Body)
	content := htmlToAuditMarkdown(res.Body)
	title := strings.TrimSpace(page.title)
	if title != "" && !strings.HasPrefix(strings.TrimSpace(content), "# ") {
		content = "# " + title + "\n\n" + content
	}
	schema := strings.Join(page.jsonLDs, "\n\n")
	report := s.tech.RunGEOAudit(res.FinalURL, title, content, page.description, schema, brand)

	robotsPass, robotsDetail := auditRobotsPass(res)
	llmsPass, llmsDetail := auditLLMsPass(res)
	report.Factors = overrideFactor(report.Factors, "robots.txt 允许 GPTBot", robotsPass, robotsDetail)
	report.Factors = overrideFactor(report.Factors, "llms.txt 可用", llmsPass, llmsDetail)
	report.Fixes = buildFixes(report.Factors)
	report.Score = auditScore(report.Factors)
	report.Grade = AuditGrade(report.Score)
	report.Summary = fmt.Sprintf("通过 %d/%d 项，加权得分 %d/100（含 robots.txt/llms.txt 真实检查）。", countPass(report.Factors), countChecked(report.Factors), report.Score)
	report.URL = res.FinalURL
	if brand != "" {
		report.Summary += " 品牌：" + brand + "。"
	}
	return report, nil
}

func auditRobotsPass(res *SiteResources) (bool, string) {
	switch {
	case res.RobotsStatus == 200 && strings.TrimSpace(res.Robots) != "":
		return true, fmt.Sprintf("robots.txt HTTP %d，%d 字符", res.RobotsStatus, len([]rune(res.Robots)))
	case res.RobotsStatus == 404:
		return false, "站点无 robots.txt（404），AI 爬虫默认可达但建议显式放行"
	case res.RobotsStatus == 0:
		return false, "robots.txt 未请求到"
	default:
		return false, fmt.Sprintf("robots.txt 返回 %d", res.RobotsStatus)
	}
}

func auditLLMsPass(res *SiteResources) (bool, string) {
	switch {
	case res.LLMsStatus == 200 && strings.TrimSpace(res.LLMsTxt) != "":
		n := len([]rune(res.LLMsTxt))
		if n < 100 {
			return false, fmt.Sprintf("/llms.txt 过短（%d 字符），建议补充站点索引", n)
		}
		return true, fmt.Sprintf("/llms.txt 可达（约 %d 字符）", n)
	case res.LLMsStatus == 404:
		return false, "站点无 /llms.txt（可用 POST /geo/techconfig/llms-txt 生成）"
	default:
		return false, fmt.Sprintf("/llms.txt 返回 %d", res.LLMsStatus)
	}
}

func siteReportView(rep *GeoSiteAuditReport, brand string) *AuditReportView {
	fixes := make([]AuditFixView, 0, len(rep.Fixes))
	for _, f := range rep.Fixes {
		fixes = append(fixes, AuditFixView{
			Category: f.Category, Check: f.Check, Weight: f.Max,
			Detail: f.Detail, Suggestion: f.Suggestion,
		})
	}
	return &AuditReportView{
		Kind: model.GeoAuditKindSite, Scope: model.GeoAuditScopePage,
		URL: rep.URL, Brand: brand, RubricVersion: rep.RubricVersion,
		Score: rep.Score, MaxScore: rep.MaxScore, Grade: rep.Grade, Summary: rep.Summary,
		Categories: rep.Categories, Fixes: fixes,
	}
}

func contentReportView(rep *GeoAuditReport, brand string) *AuditReportView {
	checks := make([]SiteAuditCheck, 0, len(rep.Factors))
	earned, total := 0, 0
	for _, f := range rep.Factors {
		score := 0
		if f.Pass {
			score = f.Weight
			earned += f.Weight
		}
		total += f.Weight
		checks = append(checks, SiteAuditCheck{
			Check: f.Factor, Score: score, Max: f.Weight, Pass: f.Pass,
			Detail: f.Detail, Fix: fixSuggestion(f.Factor, f.Detail),
		})
	}
	cats := []SiteAuditCategory{{
		Category: "content", Label: "内容可引用性", Score: earned, Max: total, Checks: checks,
	}}
	fixes := make([]AuditFixView, 0, len(rep.Fixes))
	for _, f := range rep.Fixes {
		fixes = append(fixes, AuditFixView{
			Category: "内容可引用性", Check: f.Factor, Weight: f.Weight,
			Detail: f.Detail, Suggestion: f.Suggestion,
		})
	}
	return &AuditReportView{
		Kind: model.GeoAuditKindContent, Scope: model.GeoAuditScopePage,
		URL: rep.URL, Brand: brand, RubricVersion: rep.RubricVersion,
		Score: earned, MaxScore: total, Grade: rep.Grade, Summary: rep.Summary,
		Categories: cats, Fixes: fixes,
	}
}

func (s *AuditService) persist(ctx context.Context, view *AuditReportView, pageCount int, source, createdBy string) (*model.GeoAuditRun, error) {
	source = normalizeAuditSource(source)
	cats, err := json.Marshal(view.Categories)
	if err != nil {
		return nil, fmt.Errorf("序列化审计分类失败: %w", err)
	}
	fixes, err := json.Marshal(view.Fixes)
	if err != nil {
		return nil, fmt.Errorf("序列化修复清单失败: %w", err)
	}
	var pages []byte
	if len(view.Pages) > 0 {
		pages, err = json.Marshal(view.Pages)
		if err != nil {
			return nil, fmt.Errorf("序列化逐页结果失败: %w", err)
		}
		if pageCount < len(view.Pages) {
			pageCount = len(view.Pages)
		}
	}
	if pageCount <= 0 {
		pageCount = 1
	}
	run := &model.GeoAuditRun{
		Kind: view.Kind, Scope: view.Scope, URL: view.URL, Brand: view.Brand,
		RubricVersion: view.RubricVersion, Score: view.Score, MaxScore: view.MaxScore,
		Grade: view.Grade, Categories: cats, Fixes: fixes, PageResults: pages, Summary: view.Summary,
		Source: source, PageCount: pageCount, CreatedBy: createdBy,
	}
	items := make([]*model.GeoAuditItem, 0)
	order := 0
	for _, cat := range view.Categories {
		for _, ck := range cat.Checks {
			items = append(items, &model.GeoAuditItem{
				Category: cat.Category, CategoryLabel: cat.Label, Check: ck.Check,
				Score: ck.Score, Max: ck.Max, Pass: ck.Pass,
				Detail: ck.Detail, Fix: ck.Fix, Source: ck.Source, SortOrder: order,
			})
			order++
		}
	}
	if err := s.repo.Create(ctx, run, items); err != nil {
		return nil, err
	}
	return run, nil
}

func normalizeAuditSource(source string) string {
	switch source {
	case model.GeoAuditSourceManual, model.GeoAuditSourceSchedule, model.GeoAuditSourceAutopilot, model.GeoAuditSourceAPI:
		return source
	default:
		return model.GeoAuditSourceManual
	}
}

func normalizeAuditScope(scope string) string {
	if scope == model.GeoAuditScopeSite {
		return model.GeoAuditScopeSite
	}
	return model.GeoAuditScopePage
}

// Get 取一次体检及逐项结果。
func (s *AuditService) Get(ctx context.Context, id string) (*model.GeoAuditRun, []*model.GeoAuditItem, error) {
	return s.repo.GetByID(ctx, id)
}

// List 历史列表。
func (s *AuditService) List(ctx context.Context, q repository.AuditRunQuery) ([]*model.GeoAuditRun, int64, error) {
	return s.repo.List(ctx, q)
}

// Compare 两次体检的同口径对比。URL / kind / rubric_version 任一不同即不可比：
// 不同分母的分数相减会造出“提升了 12 分”的假结论。
func (s *AuditService) Compare(ctx context.Context, fromID, toID string) (*AuditCompareResult, error) {
	from, fromItems, err := s.repo.GetByID(ctx, fromID)
	if err != nil {
		return nil, err
	}
	to, toItems, err := s.repo.GetByID(ctx, toID)
	if err != nil {
		return nil, err
	}
	res := &AuditCompareResult{From: from, To: to, Categories: []AuditCompareCategory{}, Items: []AuditCompareItem{}}
	if from.Kind != to.Kind {
		res.Reason = "两次审计类型不同（site/content），不可比"
		return res, nil
	}
	if from.URL != to.URL {
		res.Reason = "两次审计的 URL 不同，不可比"
		return res, nil
	}
	if from.RubricVersion != to.RubricVersion {
		res.Reason = fmt.Sprintf("rubric 版本不同（%s vs %s），分母不同，不可直接比较", from.RubricVersion, to.RubricVersion)
		return res, nil
	}
	if from.Scope != to.Scope {
		res.Reason = fmt.Sprintf("审计范围不同（%s vs %s），不可比", from.Scope, to.Scope)
		return res, nil
	}
	if from.PageCount != to.PageCount {
		res.Reason = fmt.Sprintf("全站页面数不同（%d vs %d），总分分母不同，不可比", from.PageCount, to.PageCount)
		return res, nil
	}
	res.Comparable = true
	res.ScoreDelta = to.Score - from.Score

	type agg struct{ score, max int }
	fromCats := map[string]agg{}
	toCats := map[string]agg{}
	labels := map[string]string{}
	addAgg := func(m map[string]agg, it *model.GeoAuditItem) {
		a := m[it.Category]
		a.score += it.Score
		a.max += it.Max
		m[it.Category] = a
		if it.CategoryLabel != "" {
			labels[it.Category] = it.CategoryLabel
		}
	}
	for _, it := range fromItems {
		addAgg(fromCats, it)
	}
	for _, it := range toItems {
		addAgg(toCats, it)
	}
	catSeen := map[string]bool{}
	for _, it := range fromItems {
		catSeen[it.Category] = true
	}
	for _, it := range toItems {
		catSeen[it.Category] = true
	}
	for cat := range catSeen {
		f, t := fromCats[cat], toCats[cat]
		label := labels[cat]
		if label == "" {
			label = cat
		}
		res.Categories = append(res.Categories, AuditCompareCategory{
			Category: cat, Label: label,
			FromScore: f.score, ToScore: t.score, FromMax: f.max, ToMax: t.max,
			Delta: t.score - f.score,
		})
	}
	sortCompareCategories(res.Categories)

	type itemAgg struct {
		pass       bool
		score, max int
		detail     string
	}
	fromMap := map[string]itemAgg{}
	toMap := map[string]itemAgg{}
	for _, it := range fromItems {
		fromMap[it.Category+"\x00"+it.Check] = itemAgg{it.Pass, it.Score, it.Max, it.Detail}
	}
	for _, it := range toItems {
		toMap[it.Category+"\x00"+it.Check] = itemAgg{it.Pass, it.Score, it.Max, it.Detail}
	}
	keys := map[string]bool{}
	for k := range fromMap {
		keys[k] = true
	}
	for k := range toMap {
		keys[k] = true
	}
	for key := range keys {
		f, hasF := fromMap[key]
		t, hasT := toMap[key]
		parts := strings.SplitN(key, "\x00", 2)
		item := AuditCompareItem{Category: parts[0], Check: parts[1], FromScore: f.score, ToScore: t.score, Max: t.max}
		if item.Max == 0 {
			item.Max = f.max
		}
		switch {
		case !hasF:
			item.Status = "added"
			res.Added++
		case !hasT:
			item.Status = "removed"
			res.Removed++
		case f.pass && t.pass:
			item.Status = "same_pass"
		case !f.pass && !t.pass:
			item.Status = "still_failing"
			res.StillFailed++
		case !f.pass && t.pass:
			item.Status = "fixed"
			res.Fixed++
		default:
			item.Status = "regressed"
			res.Regressed++
		}
		if item.Detail == "" {
			item.Detail = t.detail
		}
		res.Items = append(res.Items, item)
	}
	sortCompareItems(res.Items)
	return res, nil
}

func sortCompareCategories(list []AuditCompareCategory) {
	sort.Slice(list, func(i, j int) bool { return list[i].Category < list[j].Category })
}

func sortCompareItems(list []AuditCompareItem) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Category != list[j].Category {
			return list[i].Category < list[j].Category
		}
		return list[i].Check < list[j].Check
	})
}

func categoriesFromItems(items []*model.GeoAuditItem) []SiteAuditCategory {
	out := make([]SiteAuditCategory, 0)
	index := map[string]int{}
	for _, it := range items {
		i, ok := index[it.Category]
		if !ok {
			i = len(out)
			index[it.Category] = i
			out = append(out, SiteAuditCategory{
				Category: it.Category, Label: it.CategoryLabel,
			})
		}
		out[i].Score += it.Score
		out[i].Max += it.Max
		out[i].Checks = append(out[i].Checks, SiteAuditCheck{
			Check: it.Check, Score: it.Score, Max: it.Max, Pass: it.Pass,
			Detail: it.Detail, Fix: it.Fix, Source: it.Source,
		})
	}
	return out
}

func fixesFromItems(items []*model.GeoAuditItem) []AuditFixView {
	out := make([]AuditFixView, 0)
	for _, it := range items {
		if it.Pass {
			continue
		}
		out = append(out, AuditFixView{
			Category: it.CategoryLabel, Check: it.Check, Weight: it.Max,
			Detail: it.Detail, Suggestion: it.Fix,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Check < out[j].Check
	})
	return out
}

// BuildReportHTML 生成自包含的可打印报告（无外部 CSS/JS）。
func (s *AuditService) BuildReportHTML(ctx context.Context, id string) (string, error) {
	run, items, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	var cats []SiteAuditCategory
	_ = json.Unmarshal(run.Categories, &cats)
	var fixes []AuditFixView
	_ = json.Unmarshal(run.Fixes, &fixes)
	var pages []SiteAuditPageResult
	_ = json.Unmarshal(run.PageResults, &pages)
	// 兼容两类历史/种子行：快照 JSON 为空时从 item 行重建，
	// 否则报告会出现“有失败项但没有修复清单”的裂缝。
	if len(cats) == 0 {
		cats = categoriesFromItems(items)
	}
	if len(fixes) == 0 {
		fixes = fixesFromItems(items)
	}
	pct := 0
	if run.MaxScore > 0 {
		pct = run.Score * 100 / run.MaxScore
	}
	data := auditReportTemplateData{
		Run: run, Categories: cats, Fixes: fixes, Items: items, Pages: pages,
		ScorePct: pct, GeneratedAt: time.Now().Format("2006-01-02 15:04"),
	}
	var buf bytes.Buffer
	if err := auditReportTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildReportCSV 逐检查项导出（带 UTF-8 BOM，便于 Excel 直接打开）。
func (s *AuditService) BuildReportCSV(ctx context.Context, id string) (string, error) {
	_, items, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"category", "category_label", "check", "score", "max", "pass", "detail", "fix"})
	for _, it := range items {
		_ = w.Write([]string{
			it.Category, it.CategoryLabel, it.Check,
			strconv.Itoa(it.Score), strconv.Itoa(it.Max), strconv.FormatBool(it.Pass),
			it.Detail, it.Fix,
		})
	}
	w.Flush()
	return buf.String(), nil
}

type auditReportTemplateData struct {
	Run         *model.GeoAuditRun
	Items       []*model.GeoAuditItem
	Categories  []SiteAuditCategory
	Fixes       []AuditFixView
	Pages       []SiteAuditPageResult
	ScorePct    int
	GeneratedAt string
}

var auditReportTmpl = template.Must(template.New("geo-audit-report").Funcs(template.FuncMap{
	"passText": func(b bool) string {
		if b {
			return "通过"
		}
		return "未通过"
	},
	"add": func(a, b int) int { return a + b },
}).Parse(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<title>GEO 体检报告 - {{.Run.URL}}</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;max-width:960px;margin:32px auto;padding:0 24px;color:#1f2937;line-height:1.6}
h1{font-size:24px;margin:0 0 4px}h2{font-size:18px;margin:32px 0 12px;border-left:4px solid #2563eb;padding-left:10px}
.meta{color:#6b7280;font-size:13px}.score{display:flex;align-items:center;gap:20px;margin:20px 0;padding:18px;background:#f8fafc;border:1px solid #e5e7eb;border-radius:10px}
.big{font-size:40px;font-weight:700;color:#2563eb}.bar{height:10px;background:#e5e7eb;border-radius:999px;overflow:hidden;flex:1}.bar>i{display:block;height:100%;background:#2563eb}
table{width:100%;border-collapse:collapse;font-size:13px}th,td{border:1px solid #e5e7eb;padding:7px 9px;text-align:left;vertical-align:top}
th{background:#f3f4f6}.pass{color:#059669;font-weight:600}.fail{color:#dc2626;font-weight:600}
.cat{margin:14px 0;padding:12px;border:1px solid #e5e7eb;border-radius:8px}.cat h3{margin:0 0 6px;font-size:15px}
.muted{color:#6b7280}.fix{background:#fff7ed;border:1px solid #fed7aa;padding:12px;border-radius:8px;margin:8px 0}
@media print{body{margin:0}.no-print{display:none}}
</style></head><body>
<h1>GEO 体检报告</h1>
<div class="meta">{{.Run.URL}} · {{.Run.Kind}} / {{.Run.Scope}} · rubric {{.Run.RubricVersion}} · 生成于 {{.GeneratedAt}}</div>
<div class="score"><div><div class="big">{{.Run.Score}}<span style="font-size:16px;color:#6b7280">/{{.Run.MaxScore}}</span></div><div class="muted">{{.Run.Grade}}</div></div><div class="bar"><i style="width:{{.ScorePct}}%"></i></div><div class="muted">得分率 {{.ScorePct}}%</div></div>
<p>{{.Run.Summary}}</p>
<h2>分类得分</h2>
<table><thead><tr><th>分类</th><th>得分</th><th>通过项</th></tr></thead><tbody>
{{range .Categories}}<tr><td>{{.Label}}</td><td>{{.Score}} / {{.Max}}</td><td>{{$p := 0}}{{range .Checks}}{{if .Pass}}{{$p = add $p 1}}{{end}}{{end}}{{$p}} / {{len .Checks}}</td></tr>{{end}}
</tbody></table>
<h2>修复清单（按分值降序）</h2>
{{if .Fixes}}{{range .Fixes}}<div class="fix"><strong>[{{.Category}}] {{.Check}}</strong>（{{.Weight}} 分）<div>{{.Detail}}</div><div class="muted">建议：{{.Suggestion}}</div></div>{{end}}{{else}}<p class="muted">没有未通过项。</p>{{end}}
{{if .Pages}}<h2>页面明细</h2>
<table><thead><tr><th>页面</th><th>得分</th><th>等级</th><th>结果</th></tr></thead><tbody>
{{range .Pages}}<tr><td>{{.URL}}</td><td>{{.Score}} / {{.MaxScore}}</td><td>{{.Grade}}</td><td>{{if .Error}}<span class="fail">{{.Error}}</span>{{else}}<span class="pass">已完成</span>{{end}}</td></tr>{{end}}
</tbody></table>{{end}}
<h2>逐项明细</h2>
<table><thead><tr><th>分类</th><th>检查项</th><th>得分</th><th>结果</th><th>说明</th></tr></thead><tbody>
{{range .Items}}<tr><td>{{.CategoryLabel}}</td><td>{{.Check}}</td><td>{{.Score}} / {{.Max}}</td><td class="{{if .Pass}}pass{{else}}fail{{end}}">{{passText .Pass}}</td><td>{{.Detail}}</td></tr>{{end}}
</tbody></table>
<p class="meta">本报告由 GEO 审计中心生成。GEO 没有“保证被 AI 引用”的通行证；本报告只描述体检读数与可执行修复项。</p>
</body></html>`))

var auditInlineSpaceRe = regexp.MustCompile(`[ \t]+`)
var auditBlankLineRe = regexp.MustCompile(`\n{3,}`)

// htmlToAuditMarkdown 把页面 HTML 抽成可引用性审计需要的 Markdown 形状：
// 标题层级、列表、表格与正文链接都保留，导航/页脚/脚本丢弃。
func htmlToAuditMarkdown(body string) string {
	doc, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "noscript", "svg", "nav", "footer", "aside":
				return
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := int(n.Data[1] - '0')
				b.WriteString("\n\n" + strings.Repeat("#", level) + " " + strings.TrimSpace(textContentOf(n)) + "\n\n")
				return
			case "p", "div", "section", "article", "main", "blockquote":
				b.WriteString("\n\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				b.WriteString("\n\n")
				return
			case "li":
				b.WriteString("\n- ")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				b.WriteString("\n")
				return
			case "tr":
				b.WriteString("\n|")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == xhtml.ElementNode && (c.Data == "td" || c.Data == "th") {
						b.WriteString(" " + strings.TrimSpace(textContentOf(c)) + " |")
					}
				}
				b.WriteString("\n")
				return
			case "br":
				b.WriteString("\n")
				return
			case "a":
				href := auditAttr(n, "href")
				text := strings.TrimSpace(textContentOf(n))
				if href != "" && text != "" && (strings.HasPrefix(href, "http") || strings.HasPrefix(href, "/")) {
					b.WriteString("[" + text + "](" + href + ")")
					return
				}
			}
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	out := auditInlineSpaceRe.ReplaceAllString(b.String(), " ")
	out = auditBlankLineRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

func auditAttr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
