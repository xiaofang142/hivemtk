package service

import (
	"context"
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
	db *gorm.DB
}

// NewIndexTrackerService 创建收录追踪服务
func NewIndexTrackerService(db *gorm.DB) *IndexTrackerService {
	return &IndexTrackerService{db: db}
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
}

// VerifyArticleFull 单篇全链路验证
func (s *IndexTrackerService) VerifyArticleFull(ctx context.Context, articleID string) (*ArticleStanding, error) {
	var article model.GeoArticle
	if err := s.db.First(&article, "id = ?", articleID).Error; err != nil {
		return nil, err
	}

	as := &ArticleStanding{
		ArticleID: article.ID,
		Title:     article.Title,
	}

	// 搜索引擎 + AI 引擎
	engines := []string{"baidu", "google", "bing", "toutiao", "shenma", "doubao", "wenxin", "kimi", "deepseek"}

	for _, eng := range engines {
		now := time.Now()
		tracking := model.GeoIndexTracking{
			ArticleID:   article.ID,
			Engine:      eng,
			Keyword:     article.Keyword,
			LastChecked: &now,
		}

		// 简化版：已有 probe/verify 数据时从 geo_probe_runs / geo_verify_results 读
		// 这里做标记占位，真实实现需要调用 ProbeService + VerificationService
		var count int64
		s.db.Model(&model.GeoIndexTracking{}).
			Where("article_id = ? AND engine = ?", article.ID, eng).
			Count(&count)
		if count > 0 {
			s.db.Model(&model.GeoIndexTracking{}).
				Where("article_id = ? AND engine = ?", article.ID, eng).
				Update("last_checked", now)
		} else {
			_ = s.db.Create(&tracking).Error
		}

		// 统计站位
		if tracking.AICited {
			as.AICitedEngines++
		}
		as.TotalEngines++
	}

	if as.TotalEngines > 0 {
		as.StandingScore = float64(as.AICitedEngines) / float64(as.TotalEngines) * 100
	}

	s.db.Where("article_id = ?", article.ID).Find(&as.Trackings)
	return as, nil
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

	return fs, nil
}

// AutoVerifyCron 每日 02:00 定时入口
// 扫描前一天发布的文章 → 全链路验证 → Upsert geo_daily_stats
func (s *IndexTrackerService) AutoVerifyCron(ctx context.Context) error {
	// 读前一天发布的文章
	yesterday := time.Now().AddDate(0, 0, -1)
	var articles []model.GeoArticle
	s.db.Where("DATE(created_at) = DATE(?)", yesterday).Find(&articles)

	logger.Warnf("AutoVerifyCron: 昨日发布 %d 篇待验证", len(articles))

	for _, a := range articles {
		_, _ = s.VerifyArticleFull(ctx, a.ID)
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
