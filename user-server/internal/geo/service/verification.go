package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

type VerificationService struct {
	verifyRepo  repository.GeoVerifyResultRepository
	apiCallRepo repository.GeoAPICallRepository
	chainRepo   repository.GeoQueryChainRepository
	taskRepo    repository.GeoContentTaskRepository
	llm         *LLMAdapter
	probe       SearchProbe
}

// NewVerificationService 创建 AI 搜索验证服务
func NewVerificationService(
	vr repository.GeoVerifyResultRepository,
	acr repository.GeoAPICallRepository,
	chainRepo repository.GeoQueryChainRepository,
	taskRepo repository.GeoContentTaskRepository,
	adapter *LLMAdapter,
	probe SearchProbe,
) *VerificationService {
	if probe == nil {
		probe = NewDefaultSearchProbe()
	}
	return &VerificationService{
		verifyRepo:  vr,
		apiCallRepo: acr,
		chainRepo:   chainRepo,
		taskRepo:    taskRepo,
		llm:         adapter,
		probe:       probe,
	}
}

type verifyLLMResponse struct {
	Response             string   `json:"response"`
	BrandMentioned       bool     `json:"brand_mentioned"`
	MentionCount         int      `json:"mention_count"`
	MentionPositions     []string `json:"mention_positions"`
	Sentiment            string   `json:"sentiment"`
	CompetitorsMentioned []string `json:"competitors_mentioned"`
}

func (s *VerificationService) VerifyArticle(ctx context.Context, req dto.VerifyRequest) (*model.GeoVerifyResult, error) {

	probeResult, probeErr := s.probe.Probe(ctx, req.Query)
	probeResponse := ""
	probeEngine := ""
	if probeErr != nil {

		probeResponse = fmt.Sprintf("[探针错误：%v]", probeErr)
	} else {
		probeResponse = probeResult.Response
		probeEngine = probeResult.Engine
	}

	prompt := VerifySearchPrompt(req.BrandName, req.Query, probeResponse)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("AI 搜索验证失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "verify_search")

	parsed := parseVerifyResponse(resp.Content)

	engineTag := probeEngine
	if engineTag == "" {
		engineTag = "probe-failed"
	}

	for _, extraModel := range req.Models {
		if strings.TrimSpace(extraModel) == "" || extraModel == resp.Model {
			continue
		}
		extraResp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
		if err != nil {
			continue
		}
		extraParsed := parseVerifyResponse(extraResp.Content)
		s.recordAPICall(ctx, extraResp, "verify_search_multi")
		_ = s.verifyRepo.Create(&model.GeoVerifyResult{
			ArticleID:      req.ArticleID,
			BrandName:      req.BrandName,
			Model:          extraModel,
			Engine:         engineTag,
			Query:          req.Query,
			Response:       extraParsed.Response,
			BrandMentioned: extraParsed.BrandMentioned,
			MentionCount:   extraParsed.MentionCount,
			Sentiment:      extraParsed.Sentiment,
			Position:       strings.Join(extraParsed.MentionPositions, "; "),
		})
		if extraParsed.BrandMentioned && !parsed.BrandMentioned {
			parsed.BrandMentioned = true
			parsed.MentionCount += extraParsed.MentionCount
		}
	}

	result := &model.GeoVerifyResult{
		ArticleID:      req.ArticleID,
		BrandName:      req.BrandName,
		Model:          resp.Model,
		Engine:         engineTag,
		Query:          req.Query,
		Response:       parsed.Response,
		BrandMentioned: parsed.BrandMentioned,
		MentionCount:   parsed.MentionCount,
		Sentiment:      parsed.Sentiment,
		Position:       strings.Join(parsed.MentionPositions, "; "),
	}
	if err := s.verifyRepo.Create(result); err != nil {
		return nil, fmt.Errorf("保存验证结果失败: %w", err)
	}

	if s.chainRepo != nil {
		position := "absent"
		if parsed.BrandMentioned {
			position = "candidate"
			if len(parsed.MentionPositions) > 0 && strings.Contains(strings.ToLower(parsed.MentionPositions[0]), "first") {
				position = "first"
			}
		}
		if parsed.Sentiment == "negative" {
			position = "negative"
		}
		_ = s.chainRepo.Append(ctx, &model.GeoQueryChain{
			ChainID:       fmt.Sprintf("verify:%s:%s", req.BrandName, req.ArticleID),
			Seq:           1,
			Query:         req.Query,
			Intent:        classifyIntent(req.Query),
			BrandName:     req.BrandName,
			BrandPosition: position,
			CitedURLs:     strings.Join(parsed.MentionPositions, ","),
			Source:        "probe",
		})
	}

	return result, nil
}

func parseVerifyResponse(content string) *verifyLLMResponse {
	result := &verifyLLMResponse{}
	jsonStr := extractJSONObject(content)
	if jsonStr == "" {
		return result
	}
	_ = json.Unmarshal([]byte(jsonStr), result)
	return result
}

type negativeMonitorResult struct {
	Queries []struct {
		Query            string   `json:"query"`
		Response         string   `json:"response"`
		BrandMentioned   bool     `json:"brand_mentioned"`
		MentionCount     int      `json:"mention_count"`
		IsNegative       bool     `json:"is_negative"`
		NegativeScore    float64  `json:"negative_score"`
		NegativeKeywords []string `json:"negative_keywords"`
		RiskLevel        string   `json:"risk_level"`
		RiskDescription  string   `json:"risk_description"`
	} `json:"queries"`
	Summary struct {
		TotalQueries        int      `json:"total_queries"`
		HighRiskCount       int      `json:"high_risk_count"`
		MediumRiskCount     int      `json:"medium_risk_count"`
		LowRiskCount        int      `json:"low_risk_count"`
		AverageMentionCount float64  `json:"average_mention_count"`
		Alerts              []string `json:"alerts"`
		Recommendations     []string `json:"recommendations"`
	} `json:"summary"`
}

func (s *VerificationService) MonitorNegative(ctx context.Context, brandName string) (map[string]any, error) {

	negativeQueries, err := s.generateNegativeQueries(ctx, brandName)
	if err != nil || len(negativeQueries) == 0 {

		negativeQueries = []string{
			brandName + " 缺点",
			brandName + " 问题",
			brandName + " 投诉",
			brandName + " 差评",
			brandName + " 骗局",
		}
	}

	var probeResults strings.Builder
	engineTag := ""
	for i, q := range negativeQueries {
		pr, probeErr := s.probe.Probe(ctx, q)
		if probeErr != nil {
			fmt.Fprintf(&probeResults, "--- 查询%d: %s ---\n[探针错误: %v]\n\n", i+1, q, probeErr)
			continue
		}
		if engineTag == "" {
			engineTag = pr.Engine
		}
		fmt.Fprintf(&probeResults, "--- 查询%d [%s]: %s ---\n%s\n\n", i+1, pr.Engine, q, pr.Response)
	}
	if engineTag == "" {
		engineTag = "probe-failed"
	}

	prompt := NegativeMonitorPrompt(brandName, probeResults.String())

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		logger.Warn(fmt.Sprintf("[GEO Verify] LLM 不可用，降级负面监控: brand=%s err=%v", brandName, err))
		return map[string]any{
			"model":   "heuristic_fallback",
			"queries": []map[string]any{},
			"summary": map[string]any{
				"total_queries":         0,
				"high_risk_count":       0,
				"medium_risk_count":     0,
				"low_risk_count":        0,
				"average_mention_count": 0,
				"alerts":                []string{"LLM 不可用，本次负面监控为降级结果，请稍后重试"},
				"recommendations":       []string{},
			},
		}, nil
	}
	s.recordAPICall(ctx, resp, "negative_monitor")

	parsed := parseNegativeMonitorResult(resp.Content)
	for _, q := range parsed.Queries {
		result := &model.GeoVerifyResult{
			BrandName:      brandName,
			Model:          resp.Model,
			Engine:         engineTag,
			Query:          q.Query,
			Response:       q.Response,
			BrandMentioned: q.BrandMentioned,
			MentionCount:   q.MentionCount,
			Sentiment:      classifyNegativeSentiment(q.IsNegative, q.NegativeScore),
			Position:       q.RiskLevel,
		}
		_ = s.verifyRepo.Create(result)

		if s.taskRepo != nil && result.Sentiment == "negative" {
			_ = s.taskRepo.Create(ctx, &model.GeoContentTask{
				Keyword: q.Query,
				Intent:  classifyIntent(q.Query),
				GapType: "negative_counter",
				Detail: fmt.Sprintf("负面监控命中(risk=%s)：需生成澄清/对冲内容覆盖查询 %q 的 AI 答案",
					q.RiskLevel, q.Query),
				Status: "pending",
			})
		}
	}
	return negativeResultToMap(parsed, resp.Model), nil
}

func (s *VerificationService) generateNegativeQueries(ctx context.Context, brandName string) ([]string, error) {
	prompt := fmt.Sprintf(`请为品牌 "%s" 生成 5 个用于负面监控的搜索查询。
要求：中文，每条查询格式为"品牌名 + 负面关键词/问题"，覆盖不同类型的负面风险。
仅返回 JSON 数组，格式：["查询1", "查询2", ...]`, brandName)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 800)
	if err != nil {
		return nil, err
	}
	var queries []string
	jsonStr := extractJSONObject(resp.Content)
	if jsonStr == "" {

		if idx := strings.Index(resp.Content, "["); idx >= 0 {
			if end := strings.LastIndex(resp.Content, "]"); end > idx {
				jsonStr = resp.Content[idx : end+1]
			}
		}
	}
	if jsonStr != "" {
		_ = json.Unmarshal([]byte(jsonStr), &queries)
	}

	seen := make(map[string]bool)
	var clean []string
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q != "" && !seen[q] {
			seen[q] = true
			clean = append(clean, q)
		}
	}
	return clean, nil
}

func parseNegativeMonitorResult(content string) *negativeMonitorResult {
	result := &negativeMonitorResult{}
	jsonStr := extractJSONObject(content)
	if jsonStr == "" {
		return result
	}
	_ = json.Unmarshal([]byte(jsonStr), result)
	return result
}

func classifyNegativeSentiment(isNegative bool, score float64) string {
	if isNegative || score > 0.5 {
		return "negative"
	}
	return "neutral"
}

func negativeResultToMap(r *negativeMonitorResult, modelName string) map[string]any {
	queries := make([]map[string]any, 0, len(r.Queries))
	for _, q := range r.Queries {
		queries = append(queries, map[string]any{
			"query":             q.Query,
			"response":          q.Response,
			"brand_mentioned":   q.BrandMentioned,
			"mention_count":     q.MentionCount,
			"is_negative":       q.IsNegative,
			"negative_score":    q.NegativeScore,
			"negative_keywords": q.NegativeKeywords,
			"risk_level":        q.RiskLevel,
			"risk_description":  q.RiskDescription,
		})
	}
	return map[string]any{
		"model":   modelName,
		"queries": queries,
		"summary": map[string]any{
			"total_queries":         r.Summary.TotalQueries,
			"high_risk_count":       r.Summary.HighRiskCount,
			"medium_risk_count":     r.Summary.MediumRiskCount,
			"low_risk_count":        r.Summary.LowRiskCount,
			"average_mention_count": r.Summary.AverageMentionCount,
			"alerts":                r.Summary.Alerts,
			"recommendations":       r.Summary.Recommendations,
		},
	}
}

// GetVerifyResults 获取文章的验证结果列表
func (s *VerificationService) GetVerifyResults(ctx context.Context, articleID string) ([]*model.GeoVerifyResult, error) {
	return s.verifyRepo.GetByArticleID(articleID)
}

func (s *VerificationService) recordAPICall(ctx context.Context, resp *LLMResult, purpose string) {
	if s.apiCallRepo == nil || resp == nil {
		return
	}
	costUSD, costCNY := EstimateCostUSD(resp.Model, resp.InputTokens, resp.OutputTokens)
	call := &model.GeoAPICall{
		Provider:     resp.Provider,
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      costUSD,
		CostCNY:      costCNY,
		Purpose:      purpose,
		Status:       "success",
	}
	_ = s.apiCallRepo.Create(call)
}

// SetChainRepo 显式注入思维链仓储
func (s *VerificationService) SetChainRepo(r repository.GeoQueryChainRepository) {
	s.chainRepo = r
}
