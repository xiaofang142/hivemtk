package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
)

// IndexTrackerService 收录追踪 + AI 引用验证
//
// 职责：
// 1. VerifyArticleFull — 单篇文章全链路验证（搜索收录 + AI 引用）
// 2. FunnelStats — 全链路漏斗统计
// 3. AutoVerifyCron — 每日定时入口（02:00）
// 4. UpsertDailyStats — 聚合到 geo_daily_stats
type IndexTrackerService struct {
	trackingRepo repository.GeoIndexTrackingRepository
	articleRepo  repository.GeoArticleRepository
	keywordRepo  repository.GeoKeywordRepository
	dailyRepo    repository.GeoDailyStatRepository
	probes       []SearchProbe
	resolver     ProbeResolver
}

// NewIndexTrackerService 创建收录追踪服务（probes 为 AI 搜索探针，可为空）
func NewIndexTrackerService(
	trackingRepo repository.GeoIndexTrackingRepository,
	articleRepo repository.GeoArticleRepository,
	keywordRepo repository.GeoKeywordRepository,
	dailyRepo repository.GeoDailyStatRepository,
	probes ...SearchProbe,
) *IndexTrackerService {
	return &IndexTrackerService{
		trackingRepo: trackingRepo,
		articleRepo:  articleRepo,
		keywordRepo:  keywordRepo,
		dailyRepo:    dailyRepo,
		probes:       probes,
	}
}

// SetProbeResolver 装配期注入按租户解析器；未注入时使用静态清单（测试/离线）。
func (s *IndexTrackerService) SetProbeResolver(r ProbeResolver) { s.resolver = r }

// ArticleStanding 单篇文章站位评分
type ArticleStanding struct {
	ArticleID      string                    `json:"article_id"`
	Title          string                    `json:"title"`
	TotalEngines   int                       `json:"total_engines"`
	AICitedEngines int                       `json:"ai_cited_engines"`
	StandingScore  float64                   `json:"standing_score"` // 0-100
	Trackings      []*model.GeoIndexTracking `json:"trackings"`
}

// FunnelStats 全链路漏斗
type FunnelStats struct {
	SeedCount       int                   `json:"seed_count"`
	LongtailCount   int                   `json:"longtail_count"`
	ArticleCount    int                   `json:"article_count"`
	DeployedCount   int                   `json:"deployed_count"`
	IndexedByEngine map[string]int        `json:"indexed_by_engine"`
	AICitedByEngine map[string]int        `json:"ai_cited_by_engine"`
	ConversionRates map[string]float64    `json:"conversion_rates"`
	Trackings       []repository.TrackRow `json:"trackings"`
}

// VerifyArticleFull 单篇全链路验证：真实调用 AI 搜索探针，检测文章 URL 是否被引用
func (s *IndexTrackerService) VerifyArticleFull(ctx context.Context, articleID string) (*ArticleStanding, error) {
	article, err := s.articleRepo.GetByID(ctx, articleID)
	if err != nil {
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

	probes := s.activeProbes(ctx)
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
			tracking := &model.GeoIndexTracking{
				ArticleID:   article.ID,
				Engine:      oc.engine,
				URL:         article.SiteURL,
				Keyword:     query,
				AICited:     oc.cited > 0,
				AICiteCount: oc.cited,
				LastChecked: &now,
			}
			if err := s.trackingRepo.UpsertCitation(ctx, tracking); err != nil {
				logger.Warnf("VerifyArticleFull: 更新 tracking %s 失败: %v", oc.engine, err)
			}
		}
	}

	// 站位评分以 DB 最终状态为准，避免内存零值误判
	tracked, err := s.trackingRepo.ListByArticle(ctx, article.ID)
	if err != nil {
		return nil, err
	}
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

// activeProbes 按调用租户解析探针。不能在装配期固化成进程级清单：
// llm_providers 与 geo_config 都是租户表，固化后 A 的品牌提示词和
// provider 凭据会被 B 的收录验证使用。
func (s *IndexTrackerService) activeProbes(ctx context.Context) []SearchProbe {
	if s.resolver != nil {
		return s.resolver(ctx)
	}
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
	seedCnt, err := s.keywordRepo.CountByLayerStatus(ctx, "seed", "active")
	if err != nil {
		return nil, err
	}
	longtailCnt, err := s.keywordRepo.CountByLayerStatus(ctx, "longtail", "active")
	if err != nil {
		return nil, err
	}
	fs.SeedCount = int(seedCnt)
	fs.LongtailCount = int(longtailCnt)

	// 文章数
	artCnt, err := s.articleRepo.Count(ctx, false)
	if err != nil {
		return nil, err
	}
	depCnt, err := s.articleRepo.Count(ctx, true)
	if err != nil {
		return nil, err
	}
	fs.ArticleCount = int(artCnt)
	fs.DeployedCount = int(depCnt)

	// 按引擎统计收录 / AI 引用
	indexedRows, err := s.trackingRepo.CountIndexedByEngine(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range indexedRows {
		fs.IndexedByEngine[r.Engine] = int(r.Count)
	}
	citedRows, err := s.trackingRepo.CountAICitedByEngine(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range citedRows {
		fs.AICitedByEngine[r.Engine] = int(r.Count)
	}

	// 转化率
	if longtailCnt > 0 {
		fs.ConversionRates["longtail_to_article"] = float64(artCnt) / float64(longtailCnt) * 100
	}
	if artCnt > 0 {
		fs.ConversionRates["article_to_deployed"] = float64(depCnt) / float64(artCnt) * 100
	}

	// 追踪明细（最近 100 条，供前端列表展示）
	trackings, err := s.trackingRepo.Recent(ctx, 100)
	if err != nil {
		return nil, err
	}
	fs.Trackings = trackings

	return fs, nil
}

// AutoVerifyCron 每日 02:00 定时入口
// 扫描已部署且超过 24h 未验证的文章 → 全链路验证 → Upsert geo_daily_stats
func (s *IndexTrackerService) AutoVerifyCron(ctx context.Context) error {
	articles, err := s.articleRepo.ListDeployedNeedingVerify(ctx, time.Now().Add(-24*time.Hour), 200)
	if err != nil {
		return fmt.Errorf("查询待验证文章失败: %w", err)
	}

	logger.Warnf("AutoVerifyCron: %d 篇已部署文章待验证", len(articles))

	for _, a := range articles {
		if _, err := s.VerifyArticleFull(ctx, a.ID); err != nil {
			logger.Warnf("AutoVerifyCron: 验证文章 %s 失败: %v", a.ID, err)
		}
	}

	// Upsert geo_daily_stats（聚合写入）
	s.upsertDailyStats(ctx)

	return nil
}

// upsertDailyStats 把 index_tracking 聚合到 daily_stats
func (s *IndexTrackerService) upsertDailyStats(ctx context.Context) {
	rows, err := s.trackingRepo.CountAICitedByEngine(ctx)
	if err != nil {
		logger.Warnf("upsertDailyStats: 聚合查询失败: %v", err)
		return
	}

	// stat_date 是 DB 的日期唯一键组成部分：宿主机是 UTC 时，16:00–23:59 之间
	// 这一轮会把业务当日的引用数累加到**前一天**的行上。
	statDate := timeutil.BusinessToday()
	for _, r := range rows {
		if err := s.dailyRepo.SetCitationByEngine(ctx, statDate, r.Engine, int(r.Count)); err != nil {
			logger.Warnf("upsertDailyStats: 引擎 %s 聚合失败: %v", r.Engine, err)
		}
	}
	logger.Warnf("upsertDailyStats: %d 引擎已聚合", len(rows))
}
