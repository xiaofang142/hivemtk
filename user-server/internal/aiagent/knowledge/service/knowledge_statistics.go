package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

// KnowledgeStatisticsService 知识库统计服务
type KnowledgeStatisticsService struct {
	docRepo       *repository.KnowledgeDocumentRepository
	chunkRepo     *repository.KnowledgeChunkRepository
	importLogRepo *repository.KnowledgeImportLogRepository
	searchLogRepo *repository.KnowledgeSearchLogRepository
	openapiRepo   *repository.KnowledgeOpenAPIRepository
}

// NewKnowledgeStatisticsService 创建统计服务
func NewKnowledgeStatisticsService() *KnowledgeStatisticsService {
	return newKnowledgeStatisticsServiceWithDB(db.GetDB())
}

// NewKnowledgeStatisticsServiceWithDB 带 DB 的统计服务(用于测试)
func NewKnowledgeStatisticsServiceWithDB(gdb *gorm.DB) *KnowledgeStatisticsService {
	return newKnowledgeStatisticsServiceWithDB(gdb)
}

func newKnowledgeStatisticsServiceWithDB(gdb *gorm.DB) *KnowledgeStatisticsService {
	return &KnowledgeStatisticsService{
		docRepo:       repository.NewKnowledgeDocumentRepository(gdb),
		chunkRepo:     repository.NewKnowledgeChunkRepository(gdb),
		importLogRepo: repository.NewKnowledgeImportLogRepository(gdb),
		searchLogRepo: repository.NewKnowledgeSearchLogRepository(gdb),
		openapiRepo:   repository.NewKnowledgeOpenAPIRepository(gdb),
	}
}

// OverviewData 总览数据
type OverviewData struct {
	TotalDocuments       int64            `json:"total_documents"`
	TotalChunks          int64            `json:"total_chunks"`
	TotalTokens          int64            `json:"total_tokens"`
	TotalSearches        int64            `json:"total_searches"`
	TodayImports         int64            `json:"today_imports"`
	TodaySearches        int64            `json:"today_searches"`
	HitRate              float64          `json:"hit_rate"`
	AvgSearchLatency     float64          `json:"avg_search_latency_ms"`
	EmbedStatusBreakdown map[string]int64 `json:"embed_status_breakdown"`
	SourceTypeBreakdown  map[string]int64 `json:"source_type_breakdown"`
	IndexHealth          IndexHealth      `json:"index_health"`
}

// IndexHealth 索引健康度
type IndexHealth struct {
	IndexedDocs    int64   `json:"indexed_docs"`
	ProcessingDocs int64   `json:"processing_docs"`
	PendingDocs    int64   `json:"pending_docs"`
	FailedDocs     int64   `json:"failed_docs"`
	IndexRate      float64 `json:"index_rate"`
}

// GetOverview 获取知识库总览
//
// KnowledgeDocument.ProductID 是 string（与 RagProduct.ID 同为 UUID），前端直接传入。
// productID="" 表示不按 product 过滤。
func (s *KnowledgeStatisticsService) GetOverview(ctx context.Context, productID string) (*OverviewData, error) {
	overview := &OverviewData{
		EmbedStatusBreakdown: make(map[string]int64),
		SourceTypeBreakdown:  make(map[string]int64),
	}

	// 产品筛选必须同时作用于文档数/分段数：否则按产品筛选后「来源分布/索引健康度」
	// 只统计该产品文档，而总量与就绪率分母仍是全库（曾出现 5 篇文档却算 5/36 就绪率）
	totalDocs, err := s.docRepo.CountByMerchant(ctx, productID)
	if err != nil {
		return nil, errors.New("统计文档数失败: " + err.Error())
	}
	overview.TotalDocuments = totalDocs

	totalChunks, err := s.chunkRepo.CountByMerchant(ctx, productID)
	if err != nil {
		return nil, errors.New("统计分段数失败: " + err.Error())
	}
	overview.TotalChunks = totalChunks

	totalTokens, err := s.docRepo.SumTotalTokens(ctx, productID)
	if err != nil {
		return nil, errors.New("累计 token 数失败: " + err.Error())
	}
	overview.TotalTokens = totalTokens

	// 累计检索次数用 TotalCount（原先用 TodayCount，导致 KPI 当天为 0 时
	// 与下方「检索质量」的窗口总数矛盾）
	overview.TotalSearches, _ = s.searchLogRepo.TotalCount(ctx, productID)

	overview.TodayImports, _ = s.docRepo.CountTodayImports(ctx)
	overview.TodaySearches, _ = s.searchLogRepo.TodayCount(ctx)

	start := time.Now().AddDate(0, 0, -30)
	quality, _ := s.searchLogRepo.GetQualityStats(ctx, productID, start, time.Now())
	if quality != nil {
		overview.HitRate = quality.HitRate
		overview.AvgSearchLatency = quality.AvgLatencyMs
	}

	for _, status := range []model.EmbedStatus{
		model.EmbedStatusPending, model.EmbedStatusProcessing,
		model.EmbedStatusIndexed, model.EmbedStatusFailed,
	} {
		filter := repository.ListFilter{

			ProductID:   productID,
			EmbedStatus: string(status),
			PageSize:    1,
		}
		_, total, _ := s.docRepo.List(ctx, filter)
		overview.EmbedStatusBreakdown[string(status)] = total
	}

	// 必须覆盖库里真实存在的全部来源类型：漏掉 batch / system_seed / manual 会让
	// 「来源类型分布」加起来小于文档总数（曾出现 36 篇只统计到 11 篇）
	for _, st := range []model.SourceType{
		model.SourceTypeUpload, model.SourceTypeText,
		model.SourceTypeURL, model.SourceTypeOpenAPI,
		model.SourceTypeBatch, model.SourceTypeSystemSeed, model.SourceTypeManual,
	} {
		filter := repository.ListFilter{

			ProductID:  productID,
			SourceType: string(st),
			PageSize:   1,
		}
		_, total, _ := s.docRepo.List(ctx, filter)
		overview.SourceTypeBreakdown[string(st)] = total
	}

	overview.IndexHealth.IndexedDocs = overview.EmbedStatusBreakdown[string(model.EmbedStatusIndexed)]
	overview.IndexHealth.ProcessingDocs = overview.EmbedStatusBreakdown[string(model.EmbedStatusProcessing)]
	overview.IndexHealth.PendingDocs = overview.EmbedStatusBreakdown[string(model.EmbedStatusPending)]
	overview.IndexHealth.FailedDocs = overview.EmbedStatusBreakdown[string(model.EmbedStatusFailed)]
	if totalDocs > 0 {
		overview.IndexHealth.IndexRate = float64(overview.IndexHealth.IndexedDocs) / float64(totalDocs)
	}

	return overview, nil
}

// DocumentStatsData 文档维度统计
type DocumentStatsData struct {
	Overview      *OverviewData               `json:"overview"`
	ImportTrend   []repository.DailyTrendItem `json:"import_trend"`
	SourceTypePie []SourceTypeStat            `json:"source_type_pie"`
	CategoryPie   []CategoryStat              `json:"category_pie"`
	TopDocuments  []DocumentHit               `json:"top_documents"`
}

// SourceTypeStat 来源类型统计
type SourceTypeStat struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// CategoryStat 分类统计
type CategoryStat struct {
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// DocumentHit 文档命中统计
type DocumentHit struct {
	DocumentID  uint64  `json:"document_id"`
	Title       string  `json:"title"`
	SearchCount int64   `json:"search_count"`
	HitCount    int64   `json:"hit_count"`
	HitRate     float64 `json:"hit_rate"`
}

// GetDocumentStats 文档维度统计
func (s *KnowledgeStatisticsService) GetDocumentStats(ctx context.Context, productID string, days int) (*DocumentStatsData, error) {
	overview, err := s.GetOverview(ctx, productID)
	if err != nil {
		return nil, err
	}

	data := &DocumentStatsData{Overview: overview}

	trend, err := s.importLogRepo.DailyImportTrend(ctx, productID, days)
	if err == nil {
		data.ImportTrend = trend
	}

	for st, count := range overview.SourceTypeBreakdown {
		data.SourceTypePie = append(data.SourceTypePie, SourceTypeStat{Type: st, Count: count})
	}

	catResults, _ := s.docRepo.CategoryStats(ctx, productID, 20)
	for _, c := range catResults {
		data.CategoryPie = append(data.CategoryPie, CategoryStat{Category: c.Category, Count: c.Count})
	}

	docHits, _ := s.docRepo.TopHitDocuments(ctx, productID, 10)
	for _, d := range docHits {
		rate := float64(0)
		if d.SearchCount > 0 {
			rate = float64(d.HitCount) / float64(d.SearchCount)
		}
		data.TopDocuments = append(data.TopDocuments, DocumentHit{
			DocumentID:  d.ID,
			Title:       d.Title,
			SearchCount: d.SearchCount,
			HitCount:    d.HitCount,
			HitRate:     rate,
		})
	}

	return data, nil
}

// SearchStatsData 检索维度统计
type SearchStatsData struct {
	Overview       *OverviewData               `json:"overview"`
	SearchTrend    []repository.DailyTrendItem `json:"search_trend"`
	HotQueries     []repository.HotQuery       `json:"hot_queries"`
	ScoreHistogram []repository.ScoreBucket    `json:"score_histogram"`
	QualityStats   *repository.QualityStats    `json:"quality_stats"`
}

// GetSearchStats 检索维度统计
func (s *KnowledgeStatisticsService) GetSearchStats(ctx context.Context, productID string, days int) (*SearchStatsData, error) {
	overview, err := s.GetOverview(ctx, productID)
	if err != nil {
		return nil, err
	}

	data := &SearchStatsData{Overview: overview}

	trend, err := s.searchLogRepo.SearchTrend(ctx, productID, days)
	if err == nil {
		data.SearchTrend = trend
	}

	hot, err := s.searchLogRepo.GetHotQueries(ctx, productID, days, 20)
	if err == nil {
		data.HotQueries = hot
	}

	hist, err := s.searchLogRepo.GetScoreHistogram(ctx, productID, days)
	if err == nil {
		data.ScoreHistogram = hist
	}

	start := time.Now().AddDate(0, 0, -days)
	quality, err := s.searchLogRepo.GetQualityStats(ctx, productID, start, time.Now())
	if err == nil {
		data.QualityStats = quality
	}

	return data, nil
}

// OpenAPIStatsData OpenAPI 同步统计
type OpenAPIStatsData struct {
	TotalSources   int64                          `json:"total_sources"`
	EnabledSources int64                          `json:"enabled_sources"`
	FailedSources  int64                          `json:"failed_sources"`
	TotalSynced    int64                          `json:"total_synced"`
	SourceList     []model.KnowledgeOpenAPISource `json:"source_list"`
}

// GetOpenAPIStats OpenAPI 同步统计
func (s *KnowledgeStatisticsService) GetOpenAPIStats(ctx context.Context, productID string) (*OpenAPIStatsData, error) {
	data := &OpenAPIStatsData{}
	sources, err := s.openapiRepo.List(ctx, productID)
	if err != nil {
		return nil, err
	}
	data.TotalSources = int64(len(sources))
	for _, src := range sources {
		if src.Enabled == 1 {
			data.EnabledSources++
		}
		if src.LastStatus == "failed" {
			data.FailedSources++
		}
		data.TotalSynced += src.TotalSynced
	}
	data.SourceList = sources
	return data, nil
}

// ImportStatsData 导入维度统计
type ImportStatsData struct {
	TotalImports   int64                       `json:"total_imports"`
	SuccessImports int64                       `json:"success_imports"`
	FailedImports  int64                       `json:"failed_imports"`
	SuccessRate    float64                     `json:"success_rate"`
	AvgDurationMs  float64                     `json:"avg_duration_ms"`
	DailyTrend     []repository.DailyTrendItem `json:"daily_trend"`
	RecentLogs     []model.KnowledgeImportLog  `json:"recent_logs"`
}

// GetImportStats 导入维度统计
func (s *KnowledgeStatisticsService) GetImportStats(ctx context.Context, productID string, days int) (*ImportStatsData, error) {
	data := &ImportStatsData{}

	trend, err := s.importLogRepo.DailyImportTrend(ctx, productID, days)
	if err == nil {
		data.DailyTrend = trend
		for _, t := range trend {
			data.TotalImports += int64(t.Count)
			data.FailedImports += int64(t.Failed)
		}
	}
	data.SuccessImports = data.TotalImports - data.FailedImports
	if data.TotalImports > 0 {
		data.SuccessRate = float64(data.SuccessImports) / float64(data.TotalImports)
	}

	avgDur, _ := s.importLogRepo.AvgImportDurationMs(ctx, productID)
	data.AvgDurationMs = avgDur

	logs, _, _ := s.importLogRepo.List(ctx, repository.ImportLogListFilter{

		ProductID: productID,
		Page:      1,
		PageSize:  DefaultPageSize,
	})
	data.RecentLogs = logs

	return data, nil
}

// LogSearch 记录一次检索行为
func (s *KnowledgeStatisticsService) LogSearch(ctx context.Context, log *model.KnowledgeSearchLog) error {
	if log.Query != "" {
		hashBytes := sha256.Sum256([]byte(log.Query))
		log.QueryHash = hex.EncodeToString(hashBytes[:])
	}
	return s.searchLogRepo.Create(ctx, log)
}
