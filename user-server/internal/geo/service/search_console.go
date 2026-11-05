package service

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// SearchConsoleRow 一行 GSC 性能数据（CSV/JSON 导入共用）。
type SearchConsoleRow struct {
	Date        string  `json:"date"`
	Query       string  `json:"query"`
	Page        string  `json:"page"`
	Clicks      int     `json:"clicks"`
	Impressions int     `json:"impressions"`
	CTR         float64 `json:"ctr"`
	Position    float64 `json:"position"`
	// Source 仅内部使用：gsc_csv / gsc_csv_undated。
	Source string `json:"-"`
}

// AIEngineStat 单引擎 AI 侧读数。
type AIEngineStat struct {
	Engine        string `json:"engine"`
	Mode          string `json:"mode"`
	Runs          int    `json:"runs"`
	BrandMentions int    `json:"brand_mentions"`
	CitationCount int    `json:"citation_count"`
}

// AISide AI 端读数。
type AISide struct {
	ProbeRuns     int            `json:"probe_runs"`
	BrandMentions int            `json:"brand_mentions"`
	CitationCount int            `json:"citation_count"`
	Modes         map[string]int `json:"modes"`
	Engines       []AIEngineStat `json:"engines"`
}

// GoogleSide 谷歌端读数（GSC CSV 导入）。
type GoogleSide struct {
	From        string                      `json:"from"`
	To          string                      `json:"to"`
	Clicks      int64                       `json:"clicks"`
	Impressions int64                       `json:"impressions"`
	CTR         float64                     `json:"ctr"`
	Position    float64                     `json:"position"`
	Rows        int64                       `json:"rows"`
	TopQueries  []repository.SearchQueryRow `json:"top_queries"`
}

// PushSide 谷歌推送/收录状态。
type PushSide struct {
	GooglePushSuccess int64 `json:"google_push_success"`
	GooglePushFail    int64 `json:"google_push_fail"`
}

// DualEngineReport 双端监测报表：口径分列，不把 AI 提及与 Google 点击混算。
type DualEngineReport struct {
	Days    int        `json:"days"`
	AI      AISide     `json:"ai"`
	Google  GoogleSide `json:"google"`
	Push    PushSide   `json:"push"`
	Fetched string     `json:"fetched_at"`
}

type SearchConsoleService struct {
	searchRepo repository.GeoSearchQueryRepository
	probeRepo  repository.GeoProbeRunRepository
	pushRepo   repository.GeoPushRecordRepository
}

func NewSearchConsoleService(searchRepo repository.GeoSearchQueryRepository, probeRepo repository.GeoProbeRunRepository, pushRepo repository.GeoPushRecordRepository) *SearchConsoleService {
	return &SearchConsoleService{searchRepo: searchRepo, probeRepo: probeRepo, pushRepo: pushRepo}
}

// ParseSearchConsoleCSV 解析 GSC 导出的 CSV。
// 兼容 “Top queries / Top pages” 两种表头；没有日期列时按导入当天记账
// 并标 gsc_csv_undated（报告会如实显示来源，不假装是精确日粒度）。
func ParseSearchConsoleCSV(text string) ([]SearchConsoleRow, []string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil, fmt.Errorf("CSV 内容为空")
	}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("解析 CSV 失败: %w", err)
	}
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("CSV 至少需要表头与一行数据")
	}
	header := map[string]int{}
	for i, raw := range records[0] {
		header[normalizeCSVHeader(raw)] = i
	}
	pick := func(names ...string) int {
		for _, n := range names {
			if i, ok := header[n]; ok {
				return i
			}
		}
		return -1
	}
	dateIdx := pick("date", "day", "日期")
	queryIdx := pick("query", "top_queries", "search_query", "queries", "查询", "热门查询")
	pageIdx := pick("page", "top_pages", "landing_page", "页面", "热门网页")
	clicksIdx := pick("clicks", "点击次数", "点击")
	imprIdx := pick("impressions", "展示次数", "展示")
	ctrIdx := pick("ctr", "点击率")
	posIdx := pick("position", "average_position", "平均排名", "排名")
	if queryIdx < 0 && pageIdx < 0 {
		return nil, nil, fmt.Errorf("CSV 缺少 query 或 page 列")
	}
	if clicksIdx < 0 && imprIdx < 0 {
		return nil, nil, fmt.Errorf("CSV 缺少 clicks/impressions 列")
	}

	rows := make([]SearchConsoleRow, 0, len(records)-1)
	errs := []string{}
	for n, rec := range records[1:] {
		get := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		row := SearchConsoleRow{
			Date:        get(dateIdx),
			Query:       get(queryIdx),
			Page:        get(pageIdx),
			Clicks:      parseIntLoose(get(clicksIdx)),
			Impressions: parseIntLoose(get(imprIdx)),
			CTR:         parsePercentLoose(get(ctrIdx)),
			Position:    parseFloatLoose(get(posIdx)),
			Source:      "gsc_csv",
		}
		if row.Date == "" {
			row.Source = "gsc_csv_undated"
		}
		if row.Query == "" && row.Page == "" {
			errs = append(errs, fmt.Sprintf("第 %d 行缺少 query/page", n+2))
			continue
		}
		rows = append(rows, row)
	}
	return rows, errs, nil
}

func normalizeCSVHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func parseIntLoose(s string) int {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func parseFloatLoose(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parsePercentLoose(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "%")), 64)
		return f / 100
	}
	f := parseFloatLoose(s)
	if f > 1 {
		return f / 100
	}
	return f
}

// Import 导入 GSC 行；日期缺失按今天，重复 row_key 覆盖更新。
func (s *SearchConsoleService) Import(ctx context.Context, rows []SearchConsoleRow) (imported, skipped int, errs []string, err error) {
	if len(rows) == 0 {
		return 0, 0, nil, fmt.Errorf("没有可导入的行")
	}
	now := time.Now()
	seen := map[string]bool{}
	recs := make([]*model.GeoSearchQuery, 0, len(rows))
	for i, row := range rows {
		query := strings.TrimSpace(row.Query)
		page := strings.TrimSpace(row.Page)
		if query == "" && page == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行缺少 query/page", i+1))
			continue
		}
		if row.Clicks < 0 || row.Impressions < 0 {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行 clicks/impressions 为负", i+1))
			continue
		}
		statDate, ok := parseGSCDate(row.Date, now)
		if !ok {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行日期格式应为 YYYY-MM-DD: %q", i+1, row.Date))
			continue
		}
		source := row.Source
		if source == "" {
			source = "gsc_csv"
		}
		key := searchQueryRowKey(statDate, query, page)
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true
		recs = append(recs, &model.GeoSearchQuery{
			RowKey: key, StatDate: statDate, Query: query, Page: page,
			Clicks: row.Clicks, Impressions: row.Impressions,
			CTR: row.CTR, Position: row.Position, Source: source,
		})
	}
	if len(recs) == 0 {
		return 0, skipped, errs, fmt.Errorf("没有合法行可导入")
	}
	n, err := s.searchRepo.UpsertBatch(ctx, recs)
	if err != nil {
		return 0, skipped, errs, err
	}
	return n, skipped, errs, nil
}

func parseGSCDate(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", "01/02/2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			y, m, d := t.Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

func searchQueryRowKey(date time.Time, query, page string) string {
	sum := sha256.Sum256([]byte(date.Format("2006-01-02") + "\x00" + query + "\x00" + page))
	return hex.EncodeToString(sum[:])
}

// DualEngine 组装双端报表：AI 侧读探针行（带 mode），Google 侧读 GSC 行，
// 推送侧读 google 推送记录。三块分开返回，前端不得把它们相加成单一分数。
func (s *SearchConsoleService) DualEngine(ctx context.Context, days int) (*DualEngineReport, error) {
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	now := time.Now()
	from := now.AddDate(0, 0, -days)

	ai := AISide{Modes: map[string]int{}, Engines: []AIEngineStat{}}
	if s.probeRepo != nil {
		runs, err := s.probeRepo.ListBetween(ctx, from, now)
		if err != nil {
			return nil, err
		}
		engineIdx := map[string]int{}
		for _, run := range runs {
			ai.ProbeRuns++
			mode := run.Mode
			if mode == "" {
				mode = ProbeModeSimulated
			}
			ai.Modes[mode]++
			if run.BrandMentioned {
				ai.BrandMentions++
			}
			cites := 0
			if len(run.Citations) > 0 {
				var list []Citation
				if json.Unmarshal(run.Citations, &list) == nil {
					cites = len(list)
				}
			}
			ai.CitationCount += cites
			i, ok := engineIdx[run.Engine]
			if !ok {
				i = len(ai.Engines)
				engineIdx[run.Engine] = i
				ai.Engines = append(ai.Engines, AIEngineStat{Engine: run.Engine, Mode: mode})
			}
			ai.Engines[i].Runs++
			if run.BrandMentioned {
				ai.Engines[i].BrandMentions++
			}
			ai.Engines[i].CitationCount += cites
		}
	}

	google := GoogleSide{From: from.Format("2006-01-02"), To: now.Format("2006-01-02"), TopQueries: []repository.SearchQueryRow{}}
	if s.searchRepo != nil {
		summary, err := s.searchRepo.Summary(ctx, from, now)
		if err != nil {
			return nil, err
		}
		google.Clicks = summary.Clicks
		google.Impressions = summary.Impressions
		google.CTR = summary.CTR
		google.Position = summary.Position
		google.Rows = summary.RowCount
		top, err := s.searchRepo.TopQueries(ctx, from, now, 20)
		if err != nil {
			return nil, err
		}
		if top != nil {
			google.TopQueries = top
		}
	}

	push := PushSide{}
	if s.pushRepo != nil {
		ok, fail, err := s.pushRepo.GoogleStats(ctx, from)
		if err == nil {
			push.GooglePushSuccess = ok
			push.GooglePushFail = fail
		}
	}

	return &DualEngineReport{
		Days: days, AI: ai, Google: google, Push: push,
		Fetched: now.Format(time.RFC3339),
	}, nil
}
