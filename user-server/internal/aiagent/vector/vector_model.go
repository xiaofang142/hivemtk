package vector

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/llm"
	"math"
	"sync"
	"time"
)

// Vectorizer 向量化接口
type Vectorizer interface {
	EmbedText(text string) ([]float32, error)
	EmbedBatch(texts []string) ([][]float32, error)
	GetDimension() int
}

// VectorStore 向量存储接口
type VectorStore interface {
	AddVectors(vectors [][]float32, metadatas []map[string]any) error
	Search(queryVector []float32, topK int) ([]SearchResult, error)
	Delete(ids []string) error
	Update(id string, vector []float32, metadata map[string]any) error
	GetDimension() int
}

// SearchResult 搜索结果
type SearchResult struct {
	ID       string         `json:"id"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata"`
	Vector   []float32      `json:"-"`
}

// InMemoryVectorStore 内存向量存储实现
type InMemoryVectorStore struct {
	vectors   [][]float32
	metadatas []map[string]any
	ids       []string
	lock      sync.RWMutex
	dimension int
}

// NewInMemoryVectorStore 创建内存向量存储
func NewInMemoryVectorStore(dimension int) *InMemoryVectorStore {
	return &InMemoryVectorStore{
		vectors:   make([][]float32, 0),
		metadatas: make([]map[string]any, 0),
		ids:       make([]string, 0),
		dimension: dimension,
	}
}

// AddVectors 添加向量
func (v *InMemoryVectorStore) AddVectors(vectors [][]float32, metadatas []map[string]any) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if len(vectors) != len(metadatas) {
		return errors.New("vectors and metadatas length mismatch")
	}

	for i, vector := range vectors {
		if len(vector) != v.dimension {
			return fmt.Errorf("vector dimension mismatch: expected %d, got %d", v.dimension, len(vector))
		}
		v.vectors = append(v.vectors, vector)
		v.metadatas = append(v.metadatas, metadatas[i])
		v.ids = append(v.ids, fmt.Sprintf("vec_%d_%d", time.Now().UnixNano(), i))
	}

	return nil
}

// Search 搜索相似向量
func (v *InMemoryVectorStore) Search(queryVector []float32, topK int) ([]SearchResult, error) {
	v.lock.RLock()
	defer v.lock.RUnlock()

	if len(queryVector) != v.dimension {
		return nil, fmt.Errorf("query vector dimension mismatch: expected %d, got %d", v.dimension, len(queryVector))
	}

	if topK <= 0 {
		topK = len(v.vectors)
	}
	if topK > len(v.vectors) {
		topK = len(v.vectors)
	}

	scores := make([]struct {
		index int
		score float64
	}, len(v.vectors))

	for i, vector := range v.vectors {
		score := cosineSimilarity(queryVector, vector)
		scores[i] = struct {
			index int
			score float64
		}{index: i, score: score}
	}

	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score < scores[j].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	results := make([]SearchResult, 0, topK)
	for i := 0; i < topK && i < len(scores); i++ {
		idx := scores[i].index
		results = append(results, SearchResult{
			ID:       v.ids[idx],
			Score:    scores[i].score,
			Metadata: v.metadatas[idx],
		})
	}

	return results, nil
}

// Delete 根据 ID 精确删除向量
func (v *InMemoryVectorStore) Delete(ids []string) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	newVecs := make([][]float32, 0, len(v.vectors))
	newMetas := make([]map[string]any, 0, len(v.metadatas))
	newIDs := make([]string, 0, len(v.ids))
	for i, id := range v.ids {
		if _, drop := idSet[id]; drop {
			continue
		}
		newVecs = append(newVecs, v.vectors[i])
		newMetas = append(newMetas, v.metadatas[i])
		newIDs = append(newIDs, id)
	}
	v.vectors = newVecs
	v.metadatas = newMetas
	v.ids = newIDs
	return nil
}

// Update 根据 ID 精确更新向量
func (v *InMemoryVectorStore) Update(id string, vector []float32, metadata map[string]any) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if len(vector) != v.dimension {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", v.dimension, len(vector))
	}
	for i, existID := range v.ids {
		if existID == id {
			v.vectors[i] = vector
			v.metadatas[i] = metadata
			return nil
		}
	}
	return fmt.Errorf("vector id %s not found", id)
}

// GetDimension 获取向量维度
func (v *InMemoryVectorStore) GetDimension() int {
	return v.dimension
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// RemoteVectorizer 真实向量化器(对接 OpenAI 兼容 /v1/embeddings)
type RemoteVectorizer struct {
	dimension int
	svc       llm.EmbeddingServiceInterface
	cfg       *llm.EmbeddingConfig
}

// NewRemoteVectorizer 构造真实向量化器
func NewRemoteVectorizer(dimension int) *RemoteVectorizer {
	svc := llm.NewEmbeddingService()
	cfg := svc.DefaultConfig()
	if dimension <= 0 {
		dimension = cfg.Dimension
	}
	cfg.Dimension = dimension
	return &RemoteVectorizer{dimension: dimension, svc: svc, cfg: cfg}
}

// EmbedText 真实 Embedding
func (r *RemoteVectorizer) EmbedText(text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("text cannot be empty")
	}
	vec, err := r.svc.EmbedOne(context.Background(), r.cfg, text)
	if err != nil {
		return nil, err
	}
	return vec, nil
}

// EmbedBatch 真实 Embedding
func (r *RemoteVectorizer) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	return r.svc.Embed(context.Background(), r.cfg, texts)
}

// GetDimension 获取维度
func (r *RemoteVectorizer) GetDimension() int { return r.dimension }

// HashVectorizer 纯内存确定性向量化器：基于文本哈希（FNV-1a + 线性同余）生成确定性向量。
// 不发起任何网络请求，用于单元测试隔离外部 embedding 服务依赖；执行的是真实计算，非 mock。
// 同一输入文本 → 同一向量，保证 ProcessAndStore/Search 的可比性。
type HashVectorizer struct {
	dimension int
}

// NewHashVectorizer 构造确定性向量化器。
func NewHashVectorizer(dimension int) *HashVectorizer {
	if dimension <= 0 {
		dimension = 128
	}
	return &HashVectorizer{dimension: dimension}
}

func hashToVector(text string, dim int) []float32 {
	vec := make([]float32, dim)

	const (
		offset uint64 = 14695981039346656037
		prime  uint64 = 1099511628211
	)
	h := offset
	for i := 0; i < len(text); i++ {
		h ^= uint64(text[i])
		h *= prime
	}
	state := h
	for i := 0; i < dim; i++ {
		state = state*6364136223846793005 + 1442695040888963407
		v := float32((state>>11)&0xFFFF)/32768.0 - 1.0
		vec[i] = v
	}
	return vec
}

// EmbedText 确定性 Embedding
func (m *HashVectorizer) EmbedText(text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("text cannot be empty")
	}
	return hashToVector(text, m.dimension), nil
}

// EmbedBatch 确定性批量 Embedding
func (m *HashVectorizer) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		if text == "" {
			return nil, errors.New("text cannot be empty")
		}
		out = append(out, hashToVector(text, m.dimension))
	}
	return out, nil
}

// GetDimension 获取维度
func (m *HashVectorizer) GetDimension() int { return m.dimension }

// VectorProcessor 向量处理器
type VectorProcessor struct {
	vectorizer Vectorizer
	store      VectorStore
}

// NewVectorProcessor 创建向量处理器
func NewVectorProcessor(vectorizer Vectorizer, store VectorStore) *VectorProcessor {
	return &VectorProcessor{
		vectorizer: vectorizer,
		store:      store,
	}
}

// ProcessAndStore 处理并存储文本
func (vp *VectorProcessor) ProcessAndStore(ctx context.Context, texts []string, metadatas []map[string]any) error {
	if len(texts) != len(metadatas) {
		return errors.New("texts and metadatas length mismatch")
	}

	vectors, err := vp.vectorizer.EmbedBatch(texts)
	if err != nil {
		return fmt.Errorf("failed to embed texts: %w", err)
	}

	err = vp.store.AddVectors(vectors, metadatas)
	if err != nil {
		return fmt.Errorf("failed to add vectors to store: %w", err)
	}

	return nil
}

// Search 搜索相似文本
func (vp *VectorProcessor) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	queryVector, err := vp.vectorizer.EmbedText(query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	results, err := vp.store.Search(queryVector, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	return results, nil
}

// BatchSearch 批量搜索
func (vp *VectorProcessor) BatchSearch(ctx context.Context, queries []string, topK int) ([][]SearchResult, error) {
	results := make([][]SearchResult, len(queries))

	for i, query := range queries {
		searchResults, err := vp.Search(ctx, query, topK)
		if err != nil {
			return nil, fmt.Errorf("failed to search for query %d: %w", i, err)
		}
		results[i] = searchResults
	}

	return results, nil
}

// RebuildIndex 重建索引
func (vp *VectorProcessor) RebuildIndex(ctx context.Context) error {
	return nil
}

// GetStats 获取统计信息
func (vp *VectorProcessor) GetStats() map[string]any {
	stats := make(map[string]any)

	stats["dimension"] = vp.store.GetDimension()
	stats["type"] = "in-memory"
	stats["timestamp"] = time.Now().Unix()

	return stats
}
