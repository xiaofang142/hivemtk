package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// KeywordEnhanceService 关键词数据增强服务
// 从历史验证数据中提取高价值关键词，反哺关键词生成
type KeywordEnhanceService struct {
	keywordRepo repository.GeoKeywordRepository
	verifyRepo  repository.GeoVerifyResultRepository
	llm         *LLMAdapter
}

// NewKeywordEnhanceService 创建关键词数据增强服务
func NewKeywordEnhanceService(kr repository.GeoKeywordRepository, vr repository.GeoVerifyResultRepository, adapter *LLMAdapter) *KeywordEnhanceService {
	return &KeywordEnhanceService{
		keywordRepo: kr,
		verifyRepo:  vr,
		llm:         adapter,
	}
}

// AnalyzeHistoricalPerformance 分析历史验证数据中的关键词表现
func (s *KeywordEnhanceService) AnalyzeHistoricalPerformance(ctx context.Context, brandName string) (*dto.KeywordEnhanceResponse, error) {

	results, err := s.verifyRepo.GetByBrandName(brandName)
	if err != nil || len(results) == 0 {
		return &dto.KeywordEnhanceResponse{
			HasData: false,
			Message: "暂无历史验证数据",
		}, nil
	}

	perfMap := map[string]*dto.KeywordPerformance{}
	for _, r := range results {
		query := r.Query
		if query == "" {
			continue
		}
		if _, ok := perfMap[query]; !ok {
			perfMap[query] = &dto.KeywordPerformance{Keyword: query}
		}
		p := perfMap[query]
		p.QueryCount++
		if r.BrandMentioned {
			p.MentionCount++
		}
	}

	perfs := make([]*dto.KeywordPerformance, 0, len(perfMap))
	for _, p := range perfMap {
		if p.QueryCount > 0 {
			p.MentionRate = float64(p.MentionCount) / float64(p.QueryCount)
		}

		p.HighValueScore = p.MentionRate * float64(p.QueryCount) * 10
		perfs = append(perfs, p)
	}

	// perfs 来自 perfMap 迭代，只比 HighValueScore 时同分词的先后每轮随机；
	// 下面还要截前 20 个 ⇒ 「高价值词」名单本身会漂，必须用关键词升序把顺序钉死。
	sort.Slice(perfs, func(i, j int) bool {
		if perfs[i].HighValueScore != perfs[j].HighValueScore {
			return perfs[i].HighValueScore > perfs[j].HighValueScore
		}
		return perfs[i].Keyword < perfs[j].Keyword
	})

	highValue := make([]*dto.KeywordPerformance, 0, 20)
	for i, p := range perfs {
		if i >= 20 {
			break
		}
		if p.MentionRate > 0.3 {
			highValue = append(highValue, p)
		}
	}

	suggestions, err := s.generateEnhancementSuggestions(ctx, brandName, highValue)
	if err != nil {
		return nil, fmt.Errorf("生成增强建议失败: %w", err)
	}

	return &dto.KeywordEnhanceResponse{
		HasData:            true,
		TotalKeywords:      len(perfMap),
		HighValueKeywords:  highValue,
		Suggestions:        suggestions,
		IntentDistribution: s.calculateIntentDistribution(perfs),
	}, nil
}

func (s *KeywordEnhanceService) generateEnhancementSuggestions(ctx context.Context, brandName string, highValue []*dto.KeywordPerformance) ([]string, error) {
	if len(highValue) == 0 || s.llm == nil {
		return []string{"暂无足够数据生成建议"}, nil
	}

	kwSummary := make([]string, 0, len(highValue))
	for _, p := range highValue {
		kwSummary = append(kwSummary, fmt.Sprintf("%s (提及率:%.0f%%)", p.Keyword, p.MentionRate*100))
	}

	prompt := fmt.Sprintf(`基于以下高价值关键词表现数据，为品牌"%s"生成 5 条关键词策略建议。

高价值关键词（按价值排序）：
%s

要求：
1. 每条建议具体可行，针对关键词策略优化
2. 结合提及率数据给出针对性建议
3. 输出 JSON 数组格式：["建议1", "建议2", ...]`, brandName, strings.Join(kwSummary, "\n"))

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 1000)
	if err != nil {
		return nil, fmt.Errorf("LLM 生成建议失败: %w", err)
	}

	var suggestions []string
	jsonStr := extractJSONArray(resp.Content)
	if jsonStr != "" {
		_ = json.Unmarshal([]byte(jsonStr), &suggestions)
	}
	if len(suggestions) == 0 {
		suggestions = []string{"基于高价值关键词持续优化内容覆盖"}
	}
	return suggestions, nil
}

func (s *KeywordEnhanceService) calculateIntentDistribution(perfs []*dto.KeywordPerformance) map[string]int {
	distribution := map[string]int{}
	for _, p := range perfs {
		intent := classifyIntent(p.Keyword)
		distribution[intent]++
	}
	return distribution
}

func classifyIntent(keyword string) string {
	switch {
	case strings.Contains(keyword, "哪家") || strings.Contains(keyword, "哪个") || strings.Contains(keyword, "哪个好") ||
		strings.Contains(keyword, "是什么") || strings.Contains(keyword, "什么是") || strings.Contains(keyword, "怎么样") ||
		strings.Contains(keyword, "靠谱吗") || strings.Contains(keyword, "好用吗"):
		return "疑问"
	case strings.Contains(keyword, "区别") || strings.Contains(keyword, "差异") || strings.Contains(keyword, "优缺点"):
		return "对比"
	case strings.Contains(keyword, "对比") || strings.Contains(keyword, "比较") || strings.Contains(keyword, "vs"):
		return "对比"
	case strings.Contains(keyword, "推荐") || strings.Contains(keyword, "排行"):
		return "推荐"
	case strings.Contains(keyword, "教程") || strings.Contains(keyword, "如何") || strings.Contains(keyword, "怎么"):
		return "教程"
	case strings.Contains(keyword, "评测") || strings.Contains(keyword, "体验"):
		return "评测"
	default:
		return "信息"
	}
}

// EnhanceKeywordWithData 用历史数据增强现有关键词
func (s *KeywordEnhanceService) EnhanceKeywordWithData(ctx context.Context, keywords []string, brandName string) ([]*model.GeoKeyword, error) {
	results, err := s.verifyRepo.GetByBrandName(brandName)
	if err != nil || len(results) == 0 {
		return nil, nil
	}

	mentionMap := map[string]float64{}
	queryCount := map[string]int{}
	for _, r := range results {
		if r.Query == "" {
			continue
		}
		queryCount[r.Query]++
		if r.BrandMentioned {
			mentionMap[r.Query]++
		}
	}
	for q := range mentionMap {
		mentionMap[q] = mentionMap[q] / float64(queryCount[q])
	}

	enhanced := make([]*model.GeoKeyword, 0, len(keywords))
	failed := 0
	for _, kw := range keywords {
		geoKW := &model.GeoKeyword{
			Keyword:  kw,
			Source:   "data_enhanced",
			Category: "数据增强",
		}
		if rate, ok := mentionMap[kw]; ok {
			geoKW.SearchVolume = int(rate * 100)
		}
		if err := s.keywordRepo.Create(geoKW); err != nil {
			failed++
			continue
		}
		enhanced = append(enhanced, geoKW)
	}
	if failed > 0 {
		logger.Errorf("数据增强关键词入库部分失败: %d 条写入失败", failed)
	}
	return enhanced, nil
}
