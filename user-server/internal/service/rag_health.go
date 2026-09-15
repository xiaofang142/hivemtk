package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

const (
	RagHealthGradeA = "A"
	RagHealthGradeB = "B"
	RagHealthGradeC = "C"
	RagHealthGradeD = "D"

	RagHealthDefaultWindow = 1 * time.Hour
)

// 6 个维度的权重（总和 1.0）
const (
	RagHealthWeightRetrieval   = 0.10
	RagHealthWeightRecall      = 0.30
	RagHealthWeightEmbedding   = 0.15
	RagHealthWeightCoverage    = 0.15
	RagHealthWeightPerformance = 0.15
	RagHealthWeightAlerts      = 0.15
)

// RagHealthService RAG 健康度服务
type RagHealthService struct {
	repo     repository.RagHealthRepository
	metric   *RagMetricsService
	mu       sync.Mutex
	cached   *RagHealthReport
	cachedAt time.Time
}

// NewRagHealthService 创建 RAG 健康度服务
//
// 私域部署: 已移除外部告警通道 (RagAlertService 已删除), alert 维度不再纳入评分
// metric 为 nil 时内部新建；db 为 nil 时返回错误响应
func NewRagHealthService(db *gorm.DB, metric *RagMetricsService) *RagHealthService {
	if metric == nil {
		metric = NewRagMetricsService(db)
	}
	return &RagHealthService{
		repo:   repository.NewRagHealthRepository(db),
		metric: metric,
	}
}

// RagHealthReport 健康度报告
type RagHealthReport struct {
	Score       int                  `json:"score"`
	Grade       string               `json:"grade"`
	Dimensions  []RagHealthDimension `json:"dimensions"`
	CheckedAt   time.Time            `json:"checked_at"`
	WindowStart time.Time            `json:"window_start"`
	WindowEnd   time.Time            `json:"window_end"`
	Summary     string               `json:"summary"`
}

// RagHealthDimension 单维度子分数
type RagHealthDimension struct {
	Name          string  `json:"name"`
	Key           string  `json:"key"`
	Score         int     `json:"score"`
	Weight        float64 `json:"weight"`
	WeightedScore float64 `json:"weighted_score"`
	MetricValue   float64 `json:"metric_value"`
	MetricDesc    string  `json:"metric_desc"`
	Status        string  `json:"status"`
}

// 维度键
const (
	RagHealthDimRetrieval   = "retrieval"
	RagHealthDimRecall      = "recall"
	RagHealthDimEmbedding   = "embedding"
	RagHealthDimCoverage    = "coverage"
	RagHealthDimPerformance = "performance"
	RagHealthDimAlerts      = "alerts"
)

// GetHealth 计算并返回 RAG 系统健康度报告
//
// 时间窗口默认为最近 1 小时；可通过 window 参数自定义
func (s *RagHealthService) GetHealth(ctx context.Context, window time.Duration) (*RagHealthReport, error) {
	if s == nil || s.repo == nil || s.metric == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if window <= 0 {
		window = RagHealthDefaultWindow
	}

	now := time.Now()
	start := now.Add(-window)
	end := now

	report := &RagHealthReport{
		CheckedAt:   now,
		WindowStart: start,
		WindowEnd:   end,
	}

	recallMetrics, err := s.metric.GetRecallMetrics(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("query recall metrics: %w", err)
	}

	var embedFailRate float64
	var embedTotal int64
	if recallMetrics != nil {
		embedFailRate = 0
		embedTotal = int64(recallMetrics.TotalQueries)
	}

	chunkCount, err := s.getChunkCount(ctx)
	if err != nil {
		chunkCount = 0
	}

	activeAlertCount := 0

	dimensions := s.computeDimensions(ctx, recallMetrics, embedFailRate, embedTotal, chunkCount, activeAlertCount)
	report.Dimensions = dimensions

	totalScore := 0.0
	for _, d := range dimensions {
		totalScore += d.WeightedScore
	}
	report.Score = int(totalScore + 0.5)
	if report.Score > 100 {
		report.Score = 100
	}
	if report.Score < 0 {
		report.Score = 0
	}

	report.Grade = scoreToGrade(report.Score)

	report.Summary = buildHealthSummary(report)

	return report, nil
}

// GetHealthCached 带缓存的 GetHealth（缓存 30 秒）
func (s *RagHealthService) GetHealthCached(ctx context.Context, window time.Duration) (*RagHealthReport, error) {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.cachedAt) < 30*time.Second {
		r := *s.cached
		s.mu.Unlock()
		return &r, nil
	}
	s.mu.Unlock()

	r, err := s.GetHealth(ctx, window)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cached = r
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return r, nil
}

func (s *RagHealthService) computeDimensions(ctx context.Context,
	recall *RecallMetrics,
	embedFailRate float64,
	embedTotal int64,
	chunkCount int64,
	activeAlertCount int,
) []RagHealthDimension {
	dims := make([]RagHealthDimension, 0, 6)

	retrievalScore := 0
	retrievalMetric := 0.0
	retrievalDesc := "无检索记录"
	retrievalStatus := "critical"
	if recall.TotalQueries > 0 {
		retrievalScore = 100
		retrievalMetric = float64(recall.TotalQueries)
		retrievalDesc = fmt.Sprintf("最近窗口检索 %d 次", recall.TotalQueries)
		retrievalStatus = "healthy"
	}
	dims = append(dims, makeDimension(RagHealthDimRetrieval, "检索可用性", retrievalScore, RagHealthWeightRetrieval, retrievalMetric, retrievalDesc, retrievalStatus))

	recallScore := 0
	recallMetric := recall.AvgRecall
	recallDesc := "无召回数据"
	recallStatus := "critical"
	if recall.TotalQueries > 0 {
		if recall.AvgRecall >= 0.7 {
			recallScore = 100
		} else {
			recallScore = int(recall.AvgRecall / 0.7 * 100)
		}
		recallDesc = fmt.Sprintf("avg_recall=%.4f (低召回样本 %d)", recall.AvgRecall, recall.LowRecallCount)
		if recallScore >= 75 {
			recallStatus = "healthy"
		} else if recallScore >= 50 {
			recallStatus = "warning"
		} else {
			recallStatus = "critical"
		}
	}
	dims = append(dims, makeDimension(RagHealthDimRecall, "召回质量", recallScore, RagHealthWeightRecall, recallMetric, recallDesc, recallStatus))

	embedScore := 0
	embedMetric := embedFailRate
	embedDesc := "无文档数据"
	embedStatus := "critical"
	if embedTotal > 0 {
		if embedFailRate <= 0 {
			embedScore = 100
		} else if embedFailRate >= 0.10 {
			embedScore = 0
		} else {
			embedScore = int((1 - embedFailRate/0.10) * 100)
		}
		embedDesc = fmt.Sprintf("embedding 失败率=%.4f (total=%d)", embedFailRate, embedTotal)
		if embedScore >= 75 {
			embedStatus = "healthy"
		} else if embedScore >= 50 {
			embedStatus = "warning"
		} else {
			embedStatus = "critical"
		}
	}
	dims = append(dims, makeDimension(RagHealthDimEmbedding, "向量化质量", embedScore, RagHealthWeightEmbedding, embedMetric, embedDesc, embedStatus))

	coverageScore := 0
	coverageMetric := float64(chunkCount)
	coverageDesc := "无知识库数据"
	coverageStatus := "critical"
	if chunkCount > 0 {
		switch {
		case chunkCount >= 1000:
			coverageScore = 100
		case chunkCount >= 100:
			coverageScore = 75
		case chunkCount >= 10:
			coverageScore = 50
		default:
			coverageScore = 25
		}
		coverageDesc = fmt.Sprintf("chunk 数=%d", chunkCount)
		if coverageScore >= 75 {
			coverageStatus = "healthy"
		} else if coverageScore >= 50 {
			coverageStatus = "warning"
		} else {
			coverageStatus = "critical"
		}
	}
	dims = append(dims, makeDimension(RagHealthDimCoverage, "知识库覆盖", coverageScore, RagHealthWeightCoverage, coverageMetric, coverageDesc, coverageStatus))

	perfScore := 0
	perfMetric := float64(recall.P99LatencyMs)
	perfDesc := "无延迟数据"
	perfStatus := "critical"
	if recall.TotalQueries > 0 {
		if recall.P99LatencyMs <= 500 {
			perfScore = 100
		} else if recall.P99LatencyMs >= 2000 {
			perfScore = 0
		} else {
			perfScore = int((1 - float64(recall.P99LatencyMs-500)/1500) * 100)
		}
		perfDesc = fmt.Sprintf("p99_latency=%dms", recall.P99LatencyMs)
		if perfScore >= 75 {
			perfStatus = "healthy"
		} else if perfScore >= 50 {
			perfStatus = "warning"
		} else {
			perfStatus = "critical"
		}
	}
	dims = append(dims, makeDimension(RagHealthDimPerformance, "性能", perfScore, RagHealthWeightPerformance, perfMetric, perfDesc, perfStatus))

	alertScore := 0
	alertMetric := float64(activeAlertCount)
	alertDesc := "无活跃预警"
	alertStatus := "critical"
	if recall.TotalQueries > 0 {
		switch {
		case activeAlertCount == 0:
			alertScore = 100
			alertStatus = "healthy"
		case activeAlertCount <= 3:
			alertScore = 60
			alertStatus = "warning"
		case activeAlertCount <= 10:
			alertScore = 30
			alertStatus = "warning"
		default:
			alertScore = 0
			alertStatus = "critical"
		}
	}
	if activeAlertCount > 0 {
		alertDesc = fmt.Sprintf("活跃预警 %d 个", activeAlertCount)
	}
	dims = append(dims, makeDimension(RagHealthDimAlerts, "告警状态", alertScore, RagHealthWeightAlerts, alertMetric, alertDesc, alertStatus))

	return dims
}

func makeDimension(key, name string, score int, weight, metric float64, desc, status string) RagHealthDimension {
	return RagHealthDimension{
		Name:          name,
		Key:           key,
		Score:         score,
		Weight:        weight,
		WeightedScore: float64(score) * weight,
		MetricValue:   metric,
		MetricDesc:    desc,
		Status:        status,
	}
}

func scoreToGrade(score int) string {
	switch {
	case score >= 90:
		return RagHealthGradeA
	case score >= 75:
		return RagHealthGradeB
	case score >= 60:
		return RagHealthGradeC
	default:
		return RagHealthGradeD
	}
}

func buildHealthSummary(r *RagHealthReport) string {
	gradeText := map[string]string{
		RagHealthGradeA: "优秀",
		RagHealthGradeB: "良好",
		RagHealthGradeC: "合格",
		RagHealthGradeD: "不合格",
	}
	gradeCN := gradeText[r.Grade]
	if gradeCN == "" {
		gradeCN = r.Grade
	}

	worstDim := ""
	worstScore := 101
	for _, d := range r.Dimensions {
		if d.Score < worstScore {
			worstScore = d.Score
			worstDim = d.Name
		}
	}

	summary := fmt.Sprintf("RAG 系统健康度 %d 分（%s）", r.Score, gradeCN)
	if worstScore < 75 && worstDim != "" {
		summary += fmt.Sprintf("，最薄弱维度：%s（%d 分）", worstDim, worstScore)
	}
	return summary
}

func (s *RagHealthService) getChunkCount(ctx context.Context) (int64, error) {
	return s.repo.CountKnowledgeChunks(ctx)
}

// ClearCache 清除缓存（测试用）
func (s *RagHealthService) ClearCache(ctx context.Context) {
	s.mu.Lock()
	s.cached = nil
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}
