package ragretrieval

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
)

// EmbeddingProviderOpenAI 默认 provider：基于 llm.EmbeddingService（OpenAI 兼容）
const EmbeddingProviderOpenAI = "openai"

// EmbeddingClient 真实向量化客户端(由 LLM 包的 EmbeddingService 提供)
type EmbeddingClient = llm.EmbeddingServiceInterface

// Vectorizer 真实向量化器实现
// 底层走 OpenAI 兼容协议的 /v1/embeddings,无 API Key 时显式降级为本地哈希实现并在日志中标记,
// 严禁在生产环境静默返回伪向量。
type Vectorizer struct {
	dimension int
	embedder  EmbeddingClient
	config    *llm.EmbeddingConfig
}

// NewVectorizer 创建真实向量化器。dimension 为期望向量维度（<=0 时默认 1024，
// 即 bge-m3 维度，与本地 TEI 容器真实输出一致）。
func NewVectorizer(dimension int, embedder EmbeddingClient) *Vectorizer {
	if embedder == nil {
		embedder = llm.NewEmbeddingService()
	}
	if dimension <= 0 {
		dimension = 1024
	}
	v := &Vectorizer{
		dimension: dimension,
		embedder:  embedder,
		config:    embedder.DefaultConfig(),
	}
	v.config.Dimension = dimension
	return v
}

// NewVectorizerFromConfig 根据 provider 配置选择向量化器实现：
// "bge-m3" → *BGEM3Vectorizer；"openai"（默认）或其他值 → *Vectorizer。
func NewVectorizerFromConfig(provider string, bgeM3Cfg BGEM3Config) VectorizerInterface {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = EmbeddingProviderOpenAI
	}
	switch provider {
	case BGEM3ProviderName:
		return NewBGEM3Vectorizer(bgeM3Cfg)
	case EmbeddingProviderOpenAI:
		fallthrough
	default:
		return NewVectorizer(bgeM3Cfg.Dimension, nil)
	}
}

func (v *Vectorizer) EmbedText(text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("text cannot be empty")
	}
	vec, err := v.embedder.EmbedOne(context.Background(), v.config, text)
	if err != nil {
		return nil, fmt.Errorf("embedding 调用失败: %w", err)
	}
	return vec, nil
}

func (v *Vectorizer) EmbedBatch(texts []string) ([][]float32, error) {
	return v.EmbedBatchContext(context.Background(), texts)
}

// EmbedBatchContext 保留批次/导入任务的租户、trace、取消与截止时间。
func (v *Vectorizer) EmbedBatchContext(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("text cannot be empty")
		}
	}
	vectors, err := v.embedder.Embed(ctx, v.config, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding 批量调用失败: %w", err)
	}
	return vectors, nil
}

func (v *Vectorizer) GetDimension() int { return v.dimension }

// ValidateEmbedding 验证向量有效性
func (v *Vectorizer) ValidateEmbedding(embedding []float32) bool {
	if len(embedding) != v.dimension {
		return false
	}
	for _, val := range embedding {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return false
		}
	}
	return true
}

// CosineSimilarity 计算余弦相似度
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// JaccardSimilarity 计算Jaccard相似度（基于词汇重叠）
func JaccardSimilarity(text1, text2 string) float64 {
	words1 := getUniqueWords(text1)
	words2 := getUniqueWords(text2)

	intersection := 0
	union := len(words2)

	for word := range words1 {
		if _, exists := words2[word]; exists {
			intersection++
		} else {
			union++
		}
	}

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func getUniqueWords(text string) map[string]bool {
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
