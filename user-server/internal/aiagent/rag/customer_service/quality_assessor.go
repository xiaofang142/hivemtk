package ragcustomerservice

import (
	"context"
	"math"
	"strings"
)

// QualityAssessorImpl 质量评估器实现
type QualityAssessorImpl struct {
	config *QualityAssessmentConfig
}

// QualityAssessmentConfig 质量评估配置
type QualityAssessmentConfig struct {
	RelevanceWeight   float64 `json:"relevance_weight"`
	AccuracyWeight    float64 `json:"accuracy_weight"`
	CoherenceWeight   float64 `json:"coherence_weight"`
	HelpfulnessWeight float64 `json:"helpfulness_weight"`
	MinimumScore      float64 `json:"minimum_score"`
}

// NewQualityAssessorImpl 创建新的质量评估器
func NewQualityAssessorImpl(config *QualityAssessmentConfig) *QualityAssessorImpl {
	if config == nil {
		config = &QualityAssessmentConfig{
			RelevanceWeight:   0.3,
			AccuracyWeight:    0.3,
			CoherenceWeight:   0.2,
			HelpfulnessWeight: 0.2,
			MinimumScore:      0.5,
		}
	}

	return &QualityAssessorImpl{
		config: config,
	}
}

// EvaluateResponse 评估回复质量
func (qa *QualityAssessorImpl) EvaluateResponse(ctx context.Context, responseContent, query string, searchResults []any) (float64, error) {
	if responseContent == "" {
		return 0.0, nil
	}

	relevance, err := qa.EvaluateRelevance(ctx, responseContent, query)
	if err != nil {
		return 0.0, err
	}

	accuracy, err := qa.EvaluateAccuracy(ctx, responseContent, extractReferenceSources(searchResults))
	if err != nil {
		return 0.0, err
	}

	coherence, err := qa.EvaluateCoherence(ctx, responseContent)
	if err != nil {
		return 0.0, err
	}

	helpfulness := qa.evaluateHelpfulness(responseContent, query)

	compositeScore := (relevance * qa.config.RelevanceWeight) +
		(accuracy * qa.config.AccuracyWeight) +
		(coherence * qa.config.CoherenceWeight) +
		(helpfulness * qa.config.HelpfulnessWeight)

	return compositeScore, nil
}

// EvaluateRelevance 评估相关性
func (qa *QualityAssessorImpl) EvaluateRelevance(ctx context.Context, response, query string) (float64, error) {
	if response == "" || query == "" {
		return 0.0, nil
	}

	similarity := calculateSemanticSimilarity(query, response)

	queryKeywords := extractKeywords(query)
	responseLower := strings.ToLower(response)

	keywordMatches := 0
	for _, keyword := range queryKeywords {
		if strings.Contains(responseLower, strings.ToLower(keyword)) {
			keywordMatches++
		}
	}

	keywordScore := 0.0
	if len(queryKeywords) > 0 {
		keywordScore = float64(keywordMatches) / float64(len(queryKeywords))
	}

	finalScore := (similarity*0.7 + keywordScore*0.3)

	if finalScore < 0 {
		finalScore = 0
	}
	if finalScore > 1 {
		finalScore = 1
	}

	return finalScore, nil
}

// EvaluateAccuracy 评估准确性
func (qa *QualityAssessorImpl) EvaluateAccuracy(ctx context.Context, response string, referenceSources []string) (float64, error) {
	if response == "" {
		return 0.0, nil
	}

	if len(referenceSources) == 0 {
		uncertaintyIndicators := []string{"可能", "也许", "大概", "不确定", "据我所知"}
		uncertaintyCount := 0
		responseLower := strings.ToLower(response)

		for _, indicator := range uncertaintyIndicators {
			if strings.Contains(responseLower, strings.ToLower(indicator)) {
				uncertaintyCount++
			}
		}

		if uncertaintyCount > 0 {
			return 0.3, nil
		}

		return 0.4, nil
	}

	consistencyScore := calculateConsistencyScore(response, referenceSources)

	return consistencyScore, nil
}

// EvaluateCoherence 评估连贯性
func (qa *QualityAssessorImpl) EvaluateCoherence(ctx context.Context, response string) (float64, error) {
	if response == "" {
		return 0.0, nil
	}

	coherenceScore := calculateCoherenceScore(response)

	return coherenceScore, nil
}

func calculateSemanticSimilarity(text1, text2 string) float64 {
	set1 := createWordSet(text1)
	set2 := createWordSet(text2)

	if len(set1) == 0 && len(set2) == 0 {
		return 1.0
	}

	if len(set1) == 0 || len(set2) == 0 {
		return 0.0
	}

	intersection := 0
	union := len(set2)

	for word := range set1 {
		if set2[word] {
			intersection++
		} else {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

func createWordSet(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	wordSet := make(map[string]bool)

	for _, word := range words {
		cleanWord := strings.Trim(word, ".,!?;:\"'()[]{}")
		if cleanWord != "" {
			wordSet[cleanWord] = true
		}
	}

	return wordSet
}

func extractKeywords(text string) []string {
	words := strings.Fields(text)
	var keywords []string

	stopWords := map[string]bool{
		"的": true, "了": true, "在": true, "是": true, "我": true,
		"有": true, "和": true, "就": true, "不": true, "人": true,
		"都": true, "一": true, "一个": true, "上": true, "也": true,
		"很": true, "到": true, "说": true, "要": true, "去": true,
		"你": true, "会": true, "着": true, "没有": true, "看": true,
		"好": true, "自己": true, "这": true,
	}

	for _, word := range words {
		cleanWord := strings.Trim(word, ".,!?;:\"'()[]{}")
		if cleanWord != "" && !stopWords[cleanWord] {
			keywords = append(keywords, cleanWord)
		}
	}

	return keywords
}

func extractReferenceSources(results []any) []string {
	var sources []string

	for _, result := range results {
		if resultMap, ok := result.(map[string]any); ok {
			if docID, exists := resultMap["document_id"]; exists {
				if docIDStr, ok := docID.(string); ok {
					sources = append(sources, docIDStr)
				}
			}
		}
	}

	return sources
}

func calculateConsistencyScore(response string, referenceSources []string) float64 {
	if len(referenceSources) == 0 {
		return 0.5
	}

	return 0.8
}

func calculateCoherenceScore(response string) float64 {
	if response == "" {
		return 0.0
	}

	sentences := splitIntoSentences(response)

	if len(sentences) == 0 {
		return 0.0
	}

	totalScore := 0.0
	for _, sentence := range sentences {
		sentenceScore := evaluateSentenceCoherence(sentence)
		totalScore += sentenceScore
	}

	averageScore := totalScore / float64(len(sentences))

	structureScore := evaluateStructureCoherence(sentences)

	finalScore := (averageScore*0.6 + structureScore*0.4)

	return finalScore
}

func splitIntoSentences(text string) []string {
	separators := []string{".", "!", "?", "。", "！", "？"}

	markedText := text
	for _, sep := range separators {
		markedText = strings.ReplaceAll(markedText, sep, "|SENTENCE_END|")
	}

	sentences := strings.Split(markedText, "|SENTENCE_END|")
	var cleanedSentences []string

	for _, sentence := range sentences {
		trimmed := strings.TrimSpace(sentence)
		if trimmed != "" {
			cleanedSentences = append(cleanedSentences, trimmed)
		}
	}

	return cleanedSentences
}

func evaluateSentenceCoherence(sentence string) float64 {
	words := strings.Fields(sentence)

	if len(words) == 0 {
		return 0.0
	}

	meaningfulWords := 0
	for _, word := range words {
		if len([]rune(word)) > 1 {
			meaningfulWords++
		}
	}

	wordRatio := float64(meaningfulWords) / float64(len(words))

	lengthScore := calculateLengthScore(len(words))

	finalScore := (wordRatio*0.6 + lengthScore*0.4)

	return finalScore
}

func calculateLengthScore(length int) float64 {
	if length < 2 {
		return 0.1
	}
	if length > 50 {
		return 0.3
	}
	if length >= 5 && length <= 20 {
		return 1.0
	}

	distance := math.Abs(float64(length) - 12.5)
	maxDistance := 37.5
	normalizedDistance := distance / maxDistance

	return 1.0 - normalizedDistance
}

func evaluateStructureCoherence(sentences []string) float64 {
	if len(sentences) == 0 {
		return 0.0
	}

	if len(sentences) == 1 {
		return 0.8
	}

	connectedSentences := 0

	for i := 1; i < len(sentences); i++ {
		prevSentence := sentences[i-1]
		currSentence := sentences[i]

		if hasConnection(prevSentence, currSentence) {
			connectedSentences++
		}
	}

	connectionRatio := float64(connectedSentences) / float64(len(sentences)-1)

	return connectionRatio
}

func hasConnection(prev, curr string) bool {
	prevWords := createWordSet(prev)
	currWords := createWordSet(curr)

	sharedCount := 0
	for word := range prevWords {
		if currWords[word] {
			sharedCount++
		}
	}

	return sharedCount > 0
}

func (qa *QualityAssessorImpl) evaluateHelpfulness(response, query string) float64 {
	if response == "" || query == "" {
		return 0.0
	}

	responseLower := strings.ToLower(response)
	queryLower := strings.ToLower(query)

	answered := qa.directlyAnswersQuestion(responseLower, queryLower)

	informative := qa.containsUsefulInformation(responseLower)

	detailed := qa.isDetailedEnough(response)

	var helpfulness float64
	if answered {
		helpfulness += 0.5
	}
	if informative {
		helpfulness += 0.3
	}
	if detailed {
		helpfulness += 0.2
	}

	return helpfulness
}

func (qa *QualityAssessorImpl) directlyAnswersQuestion(response, query string) bool {
	queryWords := strings.Fields(query)
	responseWords := createWordSet(response)

	matchedKeywords := 0
	for _, word := range queryWords {
		cleanWord := strings.Trim(word, ".,!?;:\"'()[]{}")
		if cleanWord != "" && responseWords[strings.ToLower(cleanWord)] {
			matchedKeywords++
		}
	}

	return float64(matchedKeywords)/float64(len(queryWords)) > 0.5
}

func (qa *QualityAssessorImpl) containsUsefulInformation(response string) bool {
	usefulPatterns := []string{
		"电话", "微信", "地址", "时间", "价格", "费用", "数量", "日期", "号码",
		"小时", "分钟", "秒", "年", "月", "日", "点", "分", "元", "万", "千",
	}

	for _, pattern := range usefulPatterns {
		if strings.Contains(response, pattern) {
			return true
		}
	}

	stepIndicators := []string{"第一步", "第二步", "首先", "然后", "最后", "步骤", "方法"}
	for _, indicator := range stepIndicators {
		if strings.Contains(response, indicator) {
			return true
		}
	}

	return false
}

func (qa *QualityAssessorImpl) isDetailedEnough(response string) bool {
	words := strings.Fields(response)

	minWords := 10

	return len(words) >= minWords
}
