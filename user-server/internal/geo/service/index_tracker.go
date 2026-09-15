package service

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// IndexTrackerService 收录追踪 + AI 引用验证
//
// 职责：
// 1. VerifyArticleFull — 单篇文章全链路验证（搜索收录 + AI 引用）
// 2. FunnelStats — 全链路漏斗统计
// 3. AutoVerifyCron — 每日定时入口（02:00）
// 4. UpsertDailyStats — 聚合到 geo_daily_stats
type IndexTrackerService struct {
	db     *gorm.DB
	probes []SearchProbe
}

// NewIndexTrackerService 创建收录追踪服务（probes 为 AI 搜索探针，可为空）
func NewIndexTrackerService(db *gorm.DB, probes ...SearchProbe) *IndexTrackerService {
	return &IndexTrackerService{db: db, probes: probes}
}

// ArticleStanding 单篇文章站位评分
type ArticleStanding struct {
	ArticleID      string                   `json:"article_id"`
	Title          string                   `json:"title"`
	TotalEngines   int                      `json:"total_engines"`
	AICitedEngines int                      `json:"ai_cited_engines"`
	StandingScore  float64                  `json:"standing_score"` // 0-100
	Trackings      []model.GeoIndexTracking `json:"trackings"`
}

// FunnelStats 全链路漏斗
type FunnelStats struct {
	SeedCount       int                `json:"seed_count"`
	LongtailCount   int                `json:"longtail_count"`
	ArticleCount    int                `json:"article_count"`
	DeployedCount   int                `json:"deployed_count"`
	IndexedByEngine map[string]int     `json:"indexed_by_engine"`
	AICitedByEngine map[string]int     `json:"ai_cited_by_engine"`
	ConversionRates map[string]float64 `json:"conversion_rates"`
	Trackings       []TrackRow         `json:"trackings"`
}

// TrackRow 收录追踪明细行（JOIN 文章标题）
type TrackRow struct {
	ArticleID    string     `gorm:"column:article_id" json:"article_id"`
	URL          string     `gorm:"column:url" json:"url"`
	Engine       string     `gorm:"column:engine" json:"engine"`
	Keyword      string     `gorm:"column:keyword" json:"keyword"`
	Indexed      bool       `gorm:"column:indexed" json:"indexed"`
	RankPosition int        `gorm:"column:rank_position" json:"rank_position"`
	AICited      bool       `gorm:"column:ai_cited" json:"ai_cited"`
	AICiteCount  int        `gorm:"column:ai_cite_count" json:"ai_cite_count"`
	LastChecked  *time.Time `gorm:"column:last_checked" json:"last_checked"`
}

// VerifyArticleFull 单篇全链路验证：真实调用 AI 搜索探针，检测文章 URL 是否被引用
func (s *IndexTrackerService) VerifyArticleFull(ctx context.Context, articleID string) (*ArticleStanding, error) {
	var article model.GeoArticle
	if err := s.db.First(&article, "id = ?", articleID).Error; err != nil {
		return nil, err
	}

	as := &ArticleStanding{
		ArticleID: article.ID,
		Title:     article.Title,
	}

	query := strings.TrimSpace(article.Keyword)
	if query == "" {
		query = article.Title
	}

	probes := s.activeProbes()
	if len(probes) == 0 {
		logger.Warnf("VerifyArticleFull: 无可用 AI 探针，仅回读历史 tracking (article=%s)", article.ID)
	} else {
		type outcome struct {
			engine  string
			cited   int
			checked time.Time
			err     error
		}
		outcomes := make([]outcome, len(probes))
		var wg sync.WaitGroup
		for i, p := range probes {
			wg.Add(1)
			go func(i int, p SearchProbe) {
				defer wg.Done()
				pctx, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				pr, err := p.Probe(pctx, query)
				oc := outcome{engine: p.Name(), checked: time.Now(), err: err}
				if err == nil {
					oc.cited = countCitationMatches(article.SiteURL, pr.Citations)
				}
				outcomes[i] = oc
			}(i, p)
		}
		wg.Wait()

		for _, oc := range outcomes {
			if oc.err != nil {
				logger.Warnf("VerifyArticleFull: 探针 %s 失败: %v", oc.engine, oc.err)
				continue
			}
			now := oc.checked
			updates := map[string]any{
				"ai_cited":      oc.cited > 0,
				"ai_cite_count": oc.cited,
				"url":           article.SiteURL,
				"last_checked":  now,
				"updated_at":    now,
			}
			res := s.db.Model(&model.GeoIndexTracking{}).
				Where("article_id = ? AND engine = ?", article.ID, oc.engine).
				Updates(updates)
			if res.Error != nil {
				logger.Warnf("VerifyArticleFull: 更新 tracking %s 失败: %v", oc.engine, res.Error)
				continue
			}
			if res.RowsAffected == 0 {
				tracking := model.GeoIndexTracking{
					ArticleID:   article.ID,
					Engine:      oc.engine,
					URL:         article.SiteURL,
					Keyword:     query,
					AICited:     oc.cited > 0,
					AICiteCount: oc.cited,
					LastChecked: &now,
				}
				_ = s.db.Create(&tracking).Error
			}
		}
	}

	// 站位评分以 DB 最终状态为准，避免内存零值误判
	var tracked []model.GeoIndexTracking
	s.db.Where("article_id = ?", article.ID).Find(&tracked)
	as.Trackings = tracked
	as.TotalEngines = len(tracked)
	for _, t := range tracked {
		if t.AICited {
			as.AICitedEngines++
		}
	}
	if as.TotalEngines > 0 {
		as.StandingScore = float64(as.AICitedEngines) / float64(as.TotalEngines) * 100
	}
	return as, nil
}

// activeProbes 返回可用探针（构造注入优先，懒加载 DB 装配兜底）
func (s *IndexTrackerService) activeProbes() []SearchProbe {
	if len(s.probes) > 0 {
		return s.probes
	}
	s.probes = NewEngineProbesFromDB(s.db)
	return s.probes
}

// countCitationMatches 统计引用信源中命中文章 URL 的条数
func countCitationMatches(siteURL string, cites []Citation) int {
	if strings.TrimSpace(siteURL) == "" {
		return 0
	}
	wantHost, wantPath, ok := splitURL(siteURL)
	if !ok {
		return 0
	}
	matched := 0
	for _, c := range cites {
		h, p, ok2 := splitURL(c.URL)
		if !ok2 {
			continue
		}
		hostHit := h == wantHost || strings.HasSuffix(h, "."+wantHost) || strings.HasSuffix(wantHost, "."+h)
		pathHit := strings.TrimRight(p, "/") == strings.TrimRight(wantPath, "/")
		if hostHit && pathHit {
			matched++
		}
	}
	return matched
}

func splitURL(raw string) (host, path string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	return strings.ToLower(u.Hostname()), u.Path, true
}

// FunnelStats 全链路漏斗统计
func (s *IndexTrackerService) FunnelStats(ctx context.Context) (*FunnelStats, error) {
	fs := &FunnelStats{
		IndexedByEngine: map[string]int{},
		AICitedByEngine: map[string]int{},
		ConversionRates: map[string]float64{},
	}

	// 关键词数（按 layer）
	var seedCnt, longtailCnt int64
	s.db.Model(&model.GeoKeyword{}).Where("layer = 'seed' AND status = 'active'").Count(&seedCnt)
	s.db.Model(&model.GeoKeyword{}).Where("layer = 'longtail' AND status = 'active'").Count(&longtailCnt)
	fs.SeedCount = int(seedCnt)
	fs.LongtailCount = int(longtailCnt)

	// 文章数
	var artCnt, depCnt int64
	s.db.Model(&model.GeoArticle{}).Count(&artCnt)
	s.db.Model(&model.GeoArticle{}).Where("deployed_at IS NOT NULL").Count(&depCnt)
	fs.ArticleCount = int(artCnt)
	fs.DeployedCount = int(depCnt)

	// 按引擎统计收录
	type eCount struct {
		Engine string `gorm:"column:engine"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var eRows []eCount
	s.db.Model(&model.GeoIndexTracking{}).
		Select("engine, COUNT(*) as cnt").
		Where("indexed = true").
		Group("engine").
		Scan(&eRows)
	for _, r := range eRows {
		fs.IndexedByEngine[r.Engine] = int(r.Cnt)
	}

	// 按引擎统计 AI 引用
	s.db.Model(&model.GeoIndexTracking{}).
		Select("engine, COUNT(*) as cnt").
		Where("ai_cited = true").
		Group("engine").
		Scan(&eRows)
	for _, r := range eRows {
		fs.AICitedByEngine[r.Engine] = int(r.Cnt)
	}

	// 转化率
	if longtailCnt > 0 {
		fs.ConversionRates["longtail_to_article"] = float64(artCnt) / float64(longtailCnt) * 100
	}
	if artCnt > 0 {
		fs.ConversionRates["article_to_deployed"] = float64(depCnt) / float64(artCnt) * 100
	}

	// 追踪明细（最近 100 条，供前端列表展示）
	var trackings []TrackRow
	s.db.Model(&model.GeoIndexTracking{}).
		Order("updated_at DESC").
		Limit(100).
		Scan(&trackings)
	fs.Trackings = trackings

	return fs, nil
}

// AutoVerifyCron 每日 02:00 定时入口
// 扫描已部署且超过 24h 未验证的文章 → 全链路验证 → Upsert geo_daily_stats
func (s *IndexTrackerService) AutoVerifyCron(ctx context.Context) error {
	var articles []model.GeoArticle
	s.db.Where("deployed_at IS NOT NULL AND site_url != ''").
		Where(`NOT EXISTS (
			SELECT 1 FROM geo_index_trackings t
			WHERE t.article_id = geo_articles.id AND t.updated_at > ?
		)`, time.Now().Add(-24*time.Hour)).
		Order("deployed_at DESC").
		Limit(200).
		Find(&articles)

	logger.Warnf("AutoVerifyCron: %d 篇已部署文章待验证", len(articles))

	for _, a := range articles {
		if _, err := s.VerifyArticleFull(ctx, a.ID); err != nil {
			logger.Warnf("AutoVerifyCron: 验证文章 %s 失败: %v", a.ID, err)
		}
	}

	// Upsert geo_daily_stats（聚合写入）
	s.upsertDailyStats()

	return nil
}

// upsertDailyStats 把 index_tracking 聚合到 daily_stats
func (s *IndexTrackerService) upsertDailyStats() {
	// 简化版：按 engine 聚合 citation_count
	type agg struct {
		Engine        string `gorm:"column:engine"`
		Citations     int64  `gorm:"column:citations"`
		BrandMentions int64  `gorm:"column:brand_mentions"`
	}
	var rows []agg
	s.db.Model(&model.GeoIndexTracking{}).
		Select("engine, COUNT(*) as citations, 0 as brand_mentions").
		Where("ai_cited = true").
		Group("engine").
		Scan(&rows)

	statDate := time.Now().Format("2006-01-02")
	for _, r := range rows {
		// upsert：冲突则更新
		s.db.Exec(`
			INSERT INTO geo_daily_stats (stat_date, engine, intent, funnel_stage, citation_count, brand_mentioned_count, probe_count)
			VALUES (?, ?, 'mixed', 'mixed', ?, ?, 0)
			ON CONFLICT (stat_date, engine, intent, funnel_stage) DO UPDATE SET
				citation_count = geo_daily_stats.citation_count + EXCLUDED.citation_count,
				brand_mentioned_count = geo_daily_stats.brand_mentioned_count + EXCLUDED.brand_mentioned_count
		`, statDate, r.Engine, r.Citations, r.BrandMentions)
	}
	logger.Warnf("upsertDailyStats: %d 引擎已聚合", len(rows))
}
