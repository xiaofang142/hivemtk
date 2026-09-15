package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// KeywordService 关键词服务
type KeywordService struct {
	keywordGroupRepo repository.GeoKeywordGroupRepository
	keywordRepo      repository.GeoKeywordRepository
	apiCallRepo      repository.GeoAPICallRepository
	llm              *LLMAdapter
}

// NewKeywordService 创建关键词服务
func NewKeywordService(kgRepo repository.GeoKeywordGroupRepository, repo repository.GeoKeywordRepository, acr repository.GeoAPICallRepository, adapter *LLMAdapter) *KeywordService {
	return &KeywordService{
		keywordGroupRepo: kgRepo,
		keywordRepo:      repo,
		apiCallRepo:      acr,
		llm:              adapter,
	}
}

type minedKeyword struct {
	Keyword        string `json:"keyword"`
	Category       string `json:"category"`
	Intent         string `json:"intent"`
	EstimatedValue int    `json:"estimated_value"`
}

// MineKeywords 挖掘关键词
func (s *KeywordService) MineKeywords(ctx context.Context, seedWords []string, mode string, brandName string, advantages []string) ([]*model.GeoKeyword, error) {
	advantagesStr := AdvantagesToString(advantages)
	seedWordsJSON := KeywordsToJSON(seedWords)

	baseKeywords := generateKeywordCombinations(seedWords)

	var prompt string
	if mode == "longtail" {
		prompt = KeywordPolishPrompt(brandName, KeywordsToJSON(baseKeywords))
	} else {
		prompt = KeywordMiningPrompt(brandName, advantagesStr, seedWordsJSON)
	}

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("关键词挖掘失败: %w", err)
	}

	s.recordAPICall(ctx, resp, "keyword_mining")

	keywords := s.parseMinedKeywords(resp.Content, baseKeywords)

	result := make([]*model.GeoKeyword, 0, len(keywords))
	failed := 0
	for _, kw := range keywords {
		geoKW := &model.GeoKeyword{
			Keyword:      kw.Keyword,
			Category:     kw.Category,
			Intent:       kw.Intent,
			Source:       mode,
			SearchVolume: kw.EstimatedValue,
			Layer:        "related",
		}
		for _, seed := range seedWords {
			if seed != "" && strings.Contains(geoKW.Keyword, seed) {
				geoKW.ParentKeyword = seed
				break
			}
		}
		if err := s.keywordRepo.Create(geoKW); err != nil {
			failed++
			continue
		}
		result = append(result, geoKW)
	}
	if failed > 0 {
		logger.Errorf("关键词挖掘入库部分失败: %d/%d 条写入失败", failed, len(keywords))
	}
	return result, nil
}

func (s *KeywordService) parseMinedKeywords(content string, baseKeywords []string) []*minedKeyword {
	result := make([]*minedKeyword, 0)
	tryParse := func(jsonStr string) []*minedKeyword {
		if jsonStr == "" {
			return nil
		}

		var mined []minedKeyword
		if err := json.Unmarshal([]byte(jsonStr), &mined); err == nil && len(mined) > 0 {
			out := make([]*minedKeyword, 0, len(mined))
			for i := range mined {
				if mined[i].Keyword != "" {
					out = append(out, &mined[i])
				}
			}
			if len(out) > 0 {
				return out
			}
		}

		var strs []string
		if err := json.Unmarshal([]byte(jsonStr), &strs); err == nil && len(strs) > 0 {
			out := make([]*minedKeyword, 0, len(strs))
			for _, kw := range strs {
				kw = strings.TrimSpace(kw)
				if kw == "" {
					continue
				}
				out = append(out, &minedKeyword{
					Keyword: kw, Category: "AI生成",
					Intent:         "未知",
					EstimatedValue: 5,
				})
			}
			if len(out) > 0 {
				return out
			}
		}
		return nil
	}

	if out := tryParse(extractJSONArray(content)); out != nil {
		return out
	}

	if objStr := extractJSONObject(content); objStr != "" {
		var wrap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(objStr), &wrap); err == nil {
			for _, v := range wrap {
				if out := tryParse(string(v)); out != nil {
					return out
				}
			}
		}
	}

	cleaned := strings.ReplaceAll(content, "```json", "")
	cleaned = strings.ReplaceAll(cleaned, "```", "")
	if out := tryParse(extractJSONArray(cleaned)); out != nil {
		return out
	}

	logger.Errorf("[GEO] 关键词挖掘 JSON 解析失败，回退组合词。原始返回前300字: %s", truncateForLog(content, 300))
	for _, kw := range baseKeywords {
		result = append(result, &minedKeyword{
			Keyword: kw, Category: "其他", Intent: "未知", EstimatedValue: 5,
		})
	}
	return result
}

func truncateForLog(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func generateKeywordCombinations(seedWords []string) []string {
	wordbanks := map[string][]string{
		"A前缀1": {"行业上", "市场上", "市面上", "目前", "国内", "市场"},
		"B前缀2": {"口碑好的", "比较好的", "靠谱的", "有实力的", "可靠的", "诚信的", "正规的", "专业的", "热门的", "知名的"},
		"C主词":  {"软件", "管理系统", "工具"},
		"D通义词": {"品牌", "公司", "工厂", "厂商", "生产厂家", "供应商"},
		"E推荐词": {"推荐", "排行", "推荐榜", "排行榜", "推荐榜单", "推荐排行", "推荐排行榜", "口碑排行"},
		"F疑问词": {"哪家好", "哪家强", "哪家靠谱", "哪家权威", "哪个好", "有哪些", "找哪家", "选哪家", "为什么"},
	}
	if len(seedWords) > 0 {
		wordbanks["C主词"] = append(wordbanks["C主词"], seedWords...)
	}
	patterns := [][]string{
		{"C", "D"}, {"A", "C", "D"}, {"B", "C", "D"}, {"A", "B", "C", "D"},
		{"C", "D", "E"}, {"C", "D", "F"}, {"A", "C", "D", "E"}, {"B", "C", "D", "E"},
		{"A", "B", "C", "D", "E"}, {"A", "B", "C", "D", "F"},
	}
	patternToBank := make(map[string]string)
	for bankKey := range wordbanks {
		if len(bankKey) > 0 {
			patternToBank[string(bankKey[0])] = bankKey
		}
	}
	seen := make(map[string]bool)
	allKeywords := make([]string, 0)
	for _, pattern := range patterns {
		requiredBanks := make([]string, 0)
		for _, letter := range pattern {
			if bankKey, ok := patternToBank[letter]; ok {
				if _, exists := wordbanks[bankKey]; exists {
					requiredBanks = append(requiredBanks, bankKey)
				}
			}
		}
		if len(requiredBanks) == 0 {
			continue
		}
		wordLists := make([][]string, 0, len(requiredBanks))
		for _, bank := range requiredBanks {
			wordLists = append(wordLists, wordbanks[bank])
		}
		for _, combo := range cartesianProduct(wordLists) {
			keyword := strings.Join(combo, "")
			kwLower := strings.ToLower(keyword)
			if seen[kwLower] {
				continue
			}
			seen[kwLower] = true
			allKeywords = append(allKeywords, keyword)
			if len(allKeywords) >= 100 {
				return allKeywords
			}
		}
	}
	return allKeywords
}

func cartesianProduct(lists [][]string) [][]string {
	if len(lists) == 0 {
		return nil
	}
	if len(lists) == 1 {
		result := make([][]string, 0, len(lists[0]))
		for _, item := range lists[0] {
			result = append(result, []string{item})
		}
		return result
	}
	sub := cartesianProduct(lists[1:])
	result := make([][]string, 0, len(lists[0])*len(sub))
	for _, item := range lists[0] {
		for _, s := range sub {
			combined := make([]string, 0, len(s)+1)
			combined = append(combined, item)
			combined = append(combined, s...)
			result = append(result, combined)
		}
	}
	return result
}

type expandedKeywordResult struct {
	ExpandedKeywords []string `json:"expanded_keywords"`
}

// SemanticExpand 语义足迹扩展
func (s *KeywordService) SemanticExpand(ctx context.Context, keywords []string, brandName string) ([]*model.GeoKeyword, error) {
	if len(keywords) == 0 {
		return nil, nil
	}
	keywordsToExpand := keywords
	if len(keywordsToExpand) > 50 {
		keywordsToExpand = keywordsToExpand[:50]
	}
	keywordsJSON := KeywordsToJSON(keywordsToExpand)
	prompt := SemanticExpandPrompt(brandName, keywordsJSON)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("语义扩展失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "semantic_expand")

	expanded := s.parseExpandedKeywords(resp.Content, keywordsToExpand)

	result := make([]*model.GeoKeyword, 0, len(expanded))
	originalSet := make(map[string]bool)
	for _, kw := range keywordsToExpand {
		originalSet[strings.ToLower(kw)] = true
	}
	failed := 0
	for _, kw := range expanded {
		if originalSet[strings.ToLower(kw)] {
			continue
		}
		geoKW := &model.GeoKeyword{
			Keyword: kw, Category: "扩展", Intent: "语义扩展", Source: "semantic_expand", Layer: "related",
		}
		for _, seed := range keywordsToExpand {
			if seed != "" && strings.Contains(kw, seed) {
				geoKW.ParentKeyword = seed
				break
			}
		}
		if err := s.keywordRepo.Create(geoKW); err != nil {
			failed++
			continue
		}
		result = append(result, geoKW)
	}
	if failed > 0 {
		logger.Errorf("语义扩展关键词入库部分失败: %d 条写入失败", failed)
	}
	return result, nil
}

func (s *KeywordService) parseExpandedKeywords(content string, originalKeywords []string) []string {
	jsonStr := extractJSONObject(content)
	if jsonStr != "" {
		var result expandedKeywordResult
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil && len(result.ExpandedKeywords) > 0 {
			return deduplicateKeywords(result.ExpandedKeywords, originalKeywords)
		}
	}
	jsonArrStr := extractJSONArray(content)
	if jsonArrStr != "" {
		var keywords []string
		if err := json.Unmarshal([]byte(jsonArrStr), &keywords); err == nil && len(keywords) > 0 {
			return deduplicateKeywords(keywords, originalKeywords)
		}
	}
	return nil
}

func deduplicateKeywords(expanded []string, original []string) []string {
	seen := make(map[string]bool)
	for _, kw := range original {
		seen[strings.ToLower(kw)] = true
	}
	result := make([]string, 0, len(expanded))
	for _, kw := range expanded {
		kw = strings.TrimSpace(kw)
		if len(kw) < 3 {
			continue
		}
		lower := strings.ToLower(kw)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		result = append(result, kw)
	}
	return result
}

type clusterResult struct {
	Clusters []struct {
		ID          int      `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Keywords    []string `json:"keywords"`
		Priority    string   `json:"priority"`
	} `json:"clusters"`
}

// TopicCluster 话题聚类 + 结果持久化到 geo_keyword_groups
func (s *KeywordService) TopicCluster(ctx context.Context, keywords []string, brandName string) (map[string][]string, error) {
	if len(keywords) == 0 {
		return map[string][]string{}, nil
	}
	keywordsToCluster := keywords
	if len(keywordsToCluster) > 100 {
		keywordsToCluster = keywordsToCluster[:100]
	}
	keywordsJSON := KeywordsToJSON(keywordsToCluster)
	prompt := TopicClusterPrompt(brandName, keywordsJSON)

	resp, err := s.llm.GenerateJSON(ctx, "", prompt, 8000)
	if err != nil {
		return nil, fmt.Errorf("话题聚类失败: %w", err)
	}
	s.recordAPICall(ctx, resp, "topic_cluster")

	clusters := s.parseClusterResult(resp.Content, keywordsToCluster)

	for name, kws := range clusters {
		if name == "" || len(kws) == 0 {
			continue
		}
		if _, uErr := s.keywordGroupRepo.UpsertByName(name, kws); uErr != nil {
			logger.Errorf("[GEO Keyword] 聚类分组持久化失败 name=%s err=%v", name, uErr)
		}
	}

	return clusters, nil
}

func (s *KeywordService) parseClusterResult(content string, originalKeywords []string) map[string][]string {
	clusters := make(map[string][]string)
	jsonStr := extractJSONObject(content)
	if jsonStr == "" {
		return ruleBasedCluster(originalKeywords)
	}
	var result clusterResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil || len(result.Clusters) == 0 {
		return ruleBasedCluster(originalKeywords)
	}
	for _, c := range result.Clusters {
		if c.Name != "" && len(c.Keywords) > 0 {
			clusters[c.Name] = c.Keywords
		}
	}
	if len(clusters) == 0 {
		return ruleBasedCluster(originalKeywords)
	}
	return clusters
}

func ruleBasedCluster(keywords []string) map[string][]string {
	clusters := make(map[string][]string)
	for _, kw := range keywords {
		category := "其他"
		if strings.Contains(kw, "对比") || strings.Contains(kw, "比较") {
			category = "对比类"
		} else if strings.Contains(kw, "推荐") || strings.Contains(kw, "排行") {
			category = "推荐类"
		} else if strings.Contains(kw, "哪家") || strings.Contains(kw, "哪个") {
			category = "疑问类"
		} else if strings.Contains(kw, "教程") || strings.Contains(kw, "如何") {
			category = "教程类"
		}
		clusters[category] = append(clusters[category], kw)
	}
	return clusters
}

// GetKeywordList 获取关键词列表（分页），layer 为漏斗层级过滤（seed/related/suggest/longtail）
func (s *KeywordService) GetKeywordList(ctx context.Context, page, limit int, search, source, layer string) ([]*model.GeoKeyword, int64, error) {
	return s.keywordRepo.GetList(search, "", source, "", "", layer, page, limit)
}

// DeleteKeyword 删除关键词
func (s *KeywordService) DeleteKeyword(ctx context.Context, id string) error {
	return s.keywordRepo.Delete(id)
}

func (s *KeywordService) recordAPICall(ctx context.Context, resp *LLMResult, purpose string) {
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

var jsonArrayRegex = regexp.MustCompile(`\[[\s\S]*\]`)
var jsonObjectRegex = regexp.MustCompile(`\{[\s\S]*\}`)

func extractJSONArray(s string) string {
	return jsonArrayRegex.FindString(s)
}

func extractJSONObject(s string) string {
	return jsonObjectRegex.FindString(s)
}
