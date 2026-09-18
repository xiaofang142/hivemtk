package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ContentService 内容服务
type ContentService struct {
	articleRepo      repository.GeoArticleRepository
	optimizationRepo repository.GeoOptimizationRepository
	apiCallRepo      repository.GeoAPICallRepository
	kbRepo           repository.GeoKnowledgeDocumentRepository
	llm              *LLMAdapter
}

// NewContentService 创建内容服务
func NewContentService(ar repository.GeoArticleRepository, or repository.GeoOptimizationRepository, acr repository.GeoAPICallRepository, kr repository.GeoKnowledgeDocumentRepository, adapter *LLMAdapter) *ContentService {
	return &ContentService{
		articleRepo:      ar,
		optimizationRepo: or,
		apiCallRepo:      acr,
		kbRepo:           kr,
		llm:              adapter,
	}
}

func extractTitleAndContent(raw string) (title, content string) {
	content = strings.TrimSpace(raw)

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for h := 6; h >= 1; h-- {
			prefix := strings.Repeat("#", h) + " "
			if strings.HasPrefix(trimmed, prefix) {
				title = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))

				content = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				return
			}
		}
	}
	if title == "" {
		title = truncateRunes(content, 40)
	}
	return
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *ContentService) GenerateContent(ctx context.Context, lang, keyword, brandName string, advantages []string, wordCount int, style string) (*model.GeoArticle, error) {
	if lang == "" {
		lang = "zh"
	}
	advantagesStr := AdvantagesToString(advantages)

	if wordCount > 20000 {
		wordCount = 20000
	}
	wordCountStr := "800"
	if wordCount > 0 {
		wordCountStr = fmt.Sprintf("%d", wordCount)
	}
	if style == "" {
		style = "专业"
	}
	prompt := ContentGenerationPrompt(brandName, advantagesStr, keyword, wordCountStr, style, lang)

	resp, err := s.llm.Generate(ctx, "", prompt, 0.7, 4000)
	if err != nil {
		return nil, fmt.Errorf("内容生成失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "content_generate")

	title, content := extractTitleAndContent(resp.Content)
	article := &model.GeoArticle{
		Keyword:   keyword,
		Title:     title,
		BrandName: brandName,
		Content:   content,
		Model:     resp.Model,
		WordCount: wordCount,
	}
	if err := s.articleRepo.Create(article); err != nil {
		return nil, fmt.Errorf("保存文章失败: %w", err)
	}

	if s.kbRepo != nil {
		if err := s.kbRepo.Create(&model.GeoKnowledgeDocument{
			Title:       article.Title,
			Content:     article.Content,
			DocType:     "generated",
			SourceLevel: "D",
			SourceURL:   "",
		}); err != nil {

			logger.Error(err, "[GEO Content] 知识库联动写入失败")
		} else {
			logger.Info(fmt.Sprintf("[GEO Content] 已写入知识库 title=%s", article.Title))
		}
	}

	return article, nil
}

// OptimizeContent 优化内容
func (s *ContentService) OptimizeContent(ctx context.Context, articleID, content, brandName string, advantages []string) (*model.GeoOptimization, error) {
	advantagesStr := AdvantagesToString(advantages)
	prompt := ContentOptimizePrompt(brandName, advantagesStr, content)

	resp, err := s.llm.Generate(ctx, "", prompt, 0.6, 4000)
	if err != nil {
		return nil, fmt.Errorf("内容优化失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "content_optimize")

	optimization := &model.GeoOptimization{
		ArticleID:        articleID,
		OriginalContent:  content,
		OptimizedContent: extractOptimizedContent(resp.Content),
		Model:            resp.Model,
	}
	if err := s.optimizationRepo.Create(optimization); err != nil {
		return nil, fmt.Errorf("保存优化记录失败: %w", err)
	}
	return optimization, nil
}

func extractOptimizedContent(response string) string {
	if idx := strings.Index(response, "【来源占位清单】"); idx != -1 {
		return strings.TrimSpace(response[:idx])
	}
	return strings.TrimSpace(response)
}

// ScoreContent 内容质量评分
// articleID 可选：非空时将评分结果持久化到 geo_articles.score / score_detail
func (s *ContentService) ScoreContent(ctx context.Context, articleID, content, brandName, keyword string) (map[string]any, error) {
	prompt := ContentScorePrompt(brandName, keyword, content)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("内容评分失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "content_score")

	result := parseScoreResult(resp.Content)

	if articleID != "" && s.articleRepo != nil {
		article, gErr := s.articleRepo.GetByID(articleID)
		if gErr == nil && article != nil {
			if scores, ok := result["scores"].(map[string]any); ok {
				if total, ok2 := scores["total"].(float64); ok2 {
					article.Score = total
				}
			}
			if detail, jErr := json.Marshal(result); jErr == nil {
				article.ScoreDetail = string(detail)
			}
			if uErr := s.articleRepo.Update(article); uErr != nil {
				logger.Error(uErr, "[GEO Content] 评分持久化失败")
			}
		}
	}

	return result, nil
}

type scoreResult struct {
	Scores struct {
		Structure    float64 `json:"structure"`
		BrandMention float64 `json:"brand_mention"`
		Authority    float64 `json:"authority"`
		Citations    float64 `json:"citations"`
		Total        float64 `json:"total"`
	} `json:"scores"`
	Details      map[string]string `json:"details"`
	Improvements []string          `json:"improvements"`
	Strengths    []string          `json:"strengths"`
}

func parseScoreResult(content string) map[string]any {
	fallback := map[string]any{
		"scores":       map[string]float64{"total": 0},
		"improvements": []string{},
		"strengths":    []string{},
		"summary":      content,
	}
	jsonStr := extractJSONObject(content)
	if jsonStr == "" {
		return fallback
	}
	var parsed scoreResult
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return fallback
	}
	return map[string]any{
		"scores":       parsed.Scores,
		"details":      parsed.Details,
		"improvements": parsed.Improvements,
		"strengths":    parsed.Strengths,
		"summary":      content,
	}
}

// EnhanceEEAT E-E-A-T 增强
// articleID 可选：非空时将增强后内容持久化到 geo_articles.content
func (s *ContentService) EnhanceEEAT(ctx context.Context, articleID, content, brandName string, advantages []string) (map[string]any, error) {
	advantagesStr := AdvantagesToString(advantages)
	prompt := EEATEnhancePrompt(brandName, advantagesStr, content)

	resp, err := s.llm.Generate(ctx, "", prompt, 0.5, 4000)
	if err != nil {
		return nil, fmt.Errorf("E-E-A-T 增强失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "eeat_enhance")

	result := map[string]any{
		"original": content,
		"enhanced": resp.Content,
		"provider": resp.Provider,
		"model":    resp.Model,
	}

	if articleID != "" && s.articleRepo != nil {
		article, gErr := s.articleRepo.GetByID(articleID)
		if gErr == nil && article != nil {
			article.Content = resp.Content
			article.WordCount = len([]rune(resp.Content))
			if uErr := s.articleRepo.Update(article); uErr != nil {
				logger.Error(uErr, "[GEO Content] EEAT 增强持久化失败")
			}
		}
	}

	return result, nil
}

// GenerateSchema 生成 JSON-LD Schema（第三参为站点域名）
// articleID 可选：非空时将 Schema 持久化到 geo_articles.json_ld
func (s *ContentService) GenerateSchema(ctx context.Context, articleID, brandName, description, domain string) (map[string]any, error) {
	prompt := SchemaGeneratePrompt(brandName, description, domain)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("schema 生成失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "schema_generate")

	var schema map[string]any
	jsonStr := extractJSONObject(resp.Content)
	if jsonStr != "" {
		_ = json.Unmarshal([]byte(jsonStr), &schema)
	}
	if schema == nil {
		schema = map[string]any{
			"@context":    "https://schema.org",
			"@type":       "Organization",
			"name":        brandName,
			"description": description,
			"url":         domain,
		}
	}
	schema["provider"] = resp.Provider
	schema["model"] = resp.Model

	if articleID != "" && s.articleRepo != nil {
		article, gErr := s.articleRepo.GetByID(articleID)
		if gErr == nil && article != nil {
			if ld, jErr := json.Marshal(schema); jErr == nil {
				article.JSONLD = string(ld)
				if uErr := s.articleRepo.Update(article); uErr != nil {
					logger.Error(uErr, "[GEO Content] Schema 持久化失败")
				}
			}
		}
	}

	return schema, nil
}

// CheckUniqueness LLM 驱动的内容原创性与查重检测
// 分析内容是否为陈词滥调、模板化、或与常见 GEO 内容高度雷同
// LLM 不可达时 fallback 到启发式套话检测（正则匹配常见 GEO 行业模板）
func (s *ContentService) CheckUniqueness(ctx context.Context, content string) (map[string]any, error) {

	prompt := fmt.Sprintf(`你是 GEO 内容原创性评审专家。请对以下内容进行原创性检测，输出 JSON：
{
  "originality_score": 0-100（分数越高越原创）,
  "plagiarism_risk": "low/medium/high"（抄袭风险）,
  "is_unique": true/false,
  "duplicate_patterns": ["检测到的模板化/陈词滥调句子列表"],
  "suggestions": ["提升原创性的具体建议"]
}

检测维度：
1. 是否使用了 GEO 行业常见套话模板（如"在当今数字化时代""随着互联网的发展"等）
2. 是否存在明显可以被其他品牌直接套用的通用段落
3. 核心观点是否有原创性，还是泛泛而谈
4. 品牌特色、差异化表达是否充分

内容：
"""%s"""`, content)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 1500)
	if err == nil {
		s.recordAPICall(ctx, resp, "uniqueness_check")

		jsonStr := extractJSONObject(resp.Content)
		result := map[string]any{
			"originality_score":  0,
			"plagiarism_risk":    "unknown",
			"is_unique":          false,
			"duplicate_patterns": []string{},
			"suggestions":        []string{},
			"provider":           resp.Provider,
			"model":              resp.Model,
		}
		if jsonStr != "" {
			var parsed map[string]any
			if json.Unmarshal([]byte(jsonStr), &parsed) == nil {
				for k := range result {
					if v, ok := parsed[k]; ok {
						result[k] = v
					}
				}
			}
		}
		return result, nil
	}

	return heuristicUniquenessCheck(content), nil
}

func heuristicUniquenessCheck(content string) map[string]any {
	cliches := []string{
		"在当今数字化时代", "随着互联网的发展", "在这个信息爆炸的时代",
		"毋庸置疑", "众所周知", "不言而喻",
		"随着科技的进步", "随着人工智能的崛起", "大数据时代",
		"引领行业发展", "助力企业腾飞", "赋能千行百业",
		"一站式解决方案", "端到端服务", "全链路覆盖",
		"重新定义", "颠覆传统", "革命性创新",
		"降本增效", "提质增效", "转型升级",
		"痛点与难点", "机遇与挑战", "任重而道远",
		"让我们一起", "携手共进", "共创辉煌",
	}

	patterns := []string{}
	for _, cliche := range cliches {
		if strings.Contains(content, cliche) {
			patterns = append(patterns, cliche)
		}
	}

	score := 100 - len(patterns)*15
	if score < 0 {
		score = 0
	}

	risk := "low"
	if score < 40 {
		risk = "high"
	} else if score < 70 {
		risk = "medium"
	}

	suggestions := []string{}
	if len(patterns) > 0 {
		suggestions = append(suggestions, "替换行业套话为品牌特有的差异化表达")
		suggestions = append(suggestions, "补充具体数据、案例或用户反馈来支撑观点")
	}
	suggestions = append(suggestions, "确保品牌核心优势在内容中有充分体现")

	return map[string]any{
		"originality_score":  score,
		"plagiarism_risk":    risk,
		"is_unique":          score >= 70,
		"duplicate_patterns": patterns,
		"suggestions":        suggestions,
		"provider":           "heuristic_fallback",
		"model":              "regex_v1",
	}
}

// GetArticleList 获取文章列表
func (s *ContentService) GetArticleList(ctx context.Context, page, limit int) ([]*model.GeoArticle, int64, error) {
	return s.articleRepo.GetList("", "", page, limit)
}

// GetArticleByID 根据 ID 获取文章
func (s *ContentService) GetArticleByID(ctx context.Context, id string) (*model.GeoArticle, error) {
	return s.articleRepo.GetByID(id)
}

func (s *ContentService) recordAPICall(ctx context.Context, resp *LLMResult, operation string) {
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
		Purpose:      operation,
		Status:       "success",
	}
	_ = s.apiCallRepo.Create(call)
}
