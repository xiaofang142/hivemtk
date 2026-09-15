package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/repository"
)

// ReportService GEO 报表服务（ROI 与成本统计）
type ReportService struct {
	articleRepo      repository.GeoArticleRepository
	keywordRepo      repository.GeoKeywordRepository
	optimizationRepo repository.GeoOptimizationRepository
	verifyRepo       repository.GeoVerifyResultRepository
	apiCallRepo      repository.GeoAPICallRepository
}

// NewReportService 创建 GEO 报表服务
func NewReportService(
	ar repository.GeoArticleRepository,
	kr repository.GeoKeywordRepository,
	or repository.GeoOptimizationRepository,
	vr repository.GeoVerifyResultRepository,
	acr repository.GeoAPICallRepository,
) *ReportService {
	return &ReportService{
		articleRepo:      ar,
		keywordRepo:      kr,
		optimizationRepo: or,
		verifyRepo:       vr,
		apiCallRepo:      acr,
	}
}

// GetReport 获取 GEO 汇总报表
func (s *ReportService) GetReport(ctx context.Context, startDate, endDate string) (*dto.ReportResponse, error) {
	report := &dto.ReportResponse{}

	_, totalArticles, err := s.articleRepo.GetList("", "", 1, 1)
	if err != nil {
		return nil, fmt.Errorf("获取文章总数失败: %w", err)
	}
	report.TotalArticles = totalArticles

	_, totalKeywords, err := s.keywordRepo.GetList("", "", "", "", "", "", 1, 1)
	if err != nil {
		return nil, fmt.Errorf("获取关键词总数失败: %w", err)
	}
	report.TotalKeywords = totalKeywords

	_, totalOptimizations, err := s.optimizationRepo.GetList("", 1, 1)
	if err != nil {
		return nil, fmt.Errorf("获取优化记录总数失败: %w", err)
	}
	report.TotalOptimizations = totalOptimizations

	costStats, err := s.apiCallRepo.GetCostStatistics(startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("获取 API 成本统计失败: %w", err)
	}

	var totalCostUSD, totalCostCNY float64
	var totalAPICalls int64
	for _, stat := range costStats {
		totalAPICalls += mapToInt64(stat, "call_count")
		totalCostUSD += mapToFloat64(stat, "cost_usd")
		totalCostCNY += mapToFloat64(stat, "cost_cny")
	}
	report.TotalCostUSD = totalCostUSD
	report.TotalCostCNY = totalCostCNY
	report.TotalAPICalls = totalAPICalls

	verifyStats, err := s.verifyRepo.GetStatistics()
	if err == nil {
		var totalVerifications int64
		for _, stat := range verifyStats {
			totalVerifications += mapToInt64(stat, "total")
		}
		report.TotalVerifications = totalVerifications
	}

	return report, nil
}

// GetROI 获取 ROI 分析（按 provider/model 过滤）
func (s *ReportService) GetROI(ctx context.Context, provider, modelName, startDate, endDate string) (map[string]any, error) {
	costStats, err := s.apiCallRepo.GetCostStatistics(startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("获取成本统计失败: %w", err)
	}

	items := make([]map[string]any, 0)
	var totalCostUSD, totalCostCNY float64
	var totalCalls int64
	var totalInputTokens, totalOutputTokens int64

	for _, stat := range costStats {
		p := mapToString(stat, "provider")
		m := mapToString(stat, "model")
		if provider != "" && p != provider {
			continue
		}
		if modelName != "" && m != modelName {
			continue
		}

		callCount := mapToInt64(stat, "call_count")
		inputTokens := mapToInt64(stat, "input_tokens")
		outputTokens := mapToInt64(stat, "output_tokens")
		costUSD := mapToFloat64(stat, "cost_usd")
		costCNY := mapToFloat64(stat, "cost_cny")

		items = append(items, map[string]any{
			"provider":      p,
			"model":         m,
			"call_count":    callCount,
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"cost_usd":      costUSD,
			"cost_cny":      costCNY,
		})

		totalCalls += callCount
		totalInputTokens += inputTokens
		totalOutputTokens += outputTokens
		totalCostUSD += costUSD
		totalCostCNY += costCNY
	}

	return map[string]any{
		"filter": map[string]any{
			"provider":   provider,
			"model":      modelName,
			"start_date": startDate,
			"end_date":   endDate,
		},
		"items":                 items,
		"total_calls":           totalCalls,
		"total_input_tokens":    totalInputTokens,
		"total_output_tokens":   totalOutputTokens,
		"total_cost_usd":        totalCostUSD,
		"total_cost_cny":        totalCostCNY,
		"avg_cost_per_call_usd": safeDivFloat(totalCostUSD, float64(totalCalls)),
	}, nil
}

// GetAPICosts 获取 API 调用成本明细
func (s *ReportService) GetAPICosts(ctx context.Context) ([]dto.APICostItem, error) {
	stats, err := s.apiCallRepo.GetCostStatistics("", "")
	if err != nil {
		return nil, fmt.Errorf("获取 API 成本失败: %w", err)
	}

	items := make([]dto.APICostItem, 0, len(stats))
	for _, stat := range stats {
		items = append(items, dto.APICostItem{
			Provider:     mapToString(stat, "provider"),
			Model:        mapToString(stat, "model"),
			CallCount:    mapToInt64(stat, "call_count"),
			InputTokens:  mapToInt64(stat, "input_tokens"),
			OutputTokens: mapToInt64(stat, "output_tokens"),
			CostUSD:      mapToFloat64(stat, "cost_usd"),
			CostCNY:      mapToFloat64(stat, "cost_cny"),
		})
	}
	return items, nil
}

func mapToString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func mapToInt64(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int32:
			return int64(n)
		case int:
			return int64(n)
		case int16:
			return int64(n)
		case uint64:
			return int64(n)
		case uint32:
			return int64(n)
		case uint:
			return int64(n)
		case float64:
			return int64(n)
		case float32:
			return int64(n)
		}
	}
	return 0
}

func mapToFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int64:
			return float64(n)
		case int32:
			return float64(n)
		case int:
			return float64(n)
		}
	}
	return 0
}

func safeDivFloat(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
