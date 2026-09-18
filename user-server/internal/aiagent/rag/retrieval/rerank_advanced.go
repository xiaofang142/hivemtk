package ragretrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// RetrievedDoc 检索得到的候选文档（含来源/原始分数，便于多路融合）
type RetrievedDoc struct {
	ID       string
	Content  string
	Score    float64
	Source   string
	Metadata map[string]any
}

// RankedDoc 重排后的文档（带最终分数与排名）
type RankedDoc struct {
	ID         string
	Content    string
	Score      float64
	Rank       int
	Original   float64
	Strategy   string
	Recomputed bool
}

// Reranker 高级别重排接口（C 域）
//
// 与 RerankerInterface 区别：
//   - RerankerInterface（rerank.go）只做单路打分（LocalReranker 调 TEI /rerank）
//   - Reranker 在其上封装多策略组合（RRF / Cross-Encoder / Hybrid）+ 缓存 + 限制
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []RetrievedDoc, topK int) ([]RankedDoc, error)

	Strategy() string
}

type rerankScoreCache struct {
	mu       sync.RWMutex
	items    map[string]rerankCacheEntry
	capacity int
	ttl      time.Duration
}

type rerankCacheEntry struct {
	score   float64
	expires time.Time
}

func newRerankScoreCache(capacity int, ttl time.Duration) *rerankScoreCache {
	if capacity <= 0 {
		capacity = 2048
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &rerankScoreCache{
		items:    make(map[string]rerankCacheEntry, capacity),
		capacity: capacity,
		ttl:      ttl,
	}
}

func (c *rerankScoreCache) Get(key string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return 0, false
	}
	if time.Now().After(e.expires) {
		return 0, false
	}
	return e.score, true
}

func (c *rerankScoreCache) Set(key string, score float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		c.items[key] = rerankCacheEntry{score: score, expires: time.Now().Add(c.ttl)}
		return
	}
	if len(c.items) >= c.capacity {
		var oldestKey string
		var oldestExp time.Time
		first := true
		for k, v := range c.items {
			if first {
				oldestKey, oldestExp, first = k, v.expires, false
				continue
			}
			if v.expires.Before(oldestExp) {
				oldestKey, oldestExp = k, v.expires
			}
		}
		if oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}
	c.items[key] = rerankCacheEntry{score: score, expires: time.Now().Add(c.ttl)}
}

func (c *rerankScoreCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]rerankCacheEntry, c.capacity)
}

func (c *rerankScoreCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func hashQuery(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])[:16]
}

func cacheKey(query, docID string) string {
	return hashQuery(query) + ":" + docID
}

// RerankStrategy 重排策略
type RerankStrategy string

const (
	StrategyCrossEncoder RerankStrategy = "cross_encoder"
	StrategyRRF          RerankStrategy = "rrf"
	StrategyHybrid       RerankStrategy = "hybrid"
)

// CrossEncoderScorer 通过 RerankerInterface（LocalReranker）给 query-doc 对打分
//
// LocalReranker.Rerank(ctx, query, []RerankDoc) 返回 []RerankResult{ID, Score}
// 内部走本地 TEI /rerank 端点（bge-reranker-v2-m3 跨编码器）
type CrossEncoderScorer struct {
	delegate RerankerInterface
	cache    *rerankScoreCache
}

// NewCrossEncoderScorer 构造 Cross-Encoder 评分器
//
// delegate 为 nil 时返回 nil scorer（HybridReranker 会降级为纯 RRF）
func NewCrossEncoderScorer(delegate RerankerInterface, cache *rerankScoreCache) *CrossEncoderScorer {
	if delegate == nil {
		return nil
	}
	if cache == nil {
		cache = newRerankScoreCache(2048, time.Hour)
	}
	return &CrossEncoderScorer{delegate: delegate, cache: cache}
}

// Score 对每个 doc 调用 Cross-Encoder 评估分数
//
// 返回 docID → score 映射；命中缓存的不重复调用
// 失败的 doc 不计入结果（由调用方决定降级策略）
func (s *CrossEncoderScorer) Score(ctx context.Context, query string, docs []RetrievedDoc) (map[string]float64, error) {
	if s == nil || s.delegate == nil {
		return nil, errors.New("cross-encoder scorer 未初始化")
	}
	if len(docs) == 0 {
		return map[string]float64{}, nil
	}

	scores := make(map[string]float64, len(docs))
	var pending []RetrievedDoc
	for _, d := range docs {
		if v, ok := s.cache.Get(cacheKey(query, d.ID)); ok {
			scores[d.ID] = v
			continue
		}
		pending = append(pending, d)
	}

	if len(pending) == 0 {
		return scores, nil
	}

	rerankDocs := make([]RerankDoc, 0, len(pending))
	for _, d := range pending {
		rerankDocs = append(rerankDocs, RerankDoc{ID: d.ID, Content: d.Content})
	}
	results, err := s.delegate.Rerank(ctx, query, rerankDocs)
	if err != nil {
		return scores, fmt.Errorf("cross-encoder 调用失败: %w", err)
	}

	for _, r := range results {
		scores[r.ID] = r.Score
		s.cache.Set(cacheKey(query, r.ID), r.Score)
	}
	return scores, nil
}

// CrossEncoderReranker 纯 Cross-Encoder 重排
type CrossEncoderReranker struct {
	scorer     *CrossEncoderScorer
	cache      *rerankScoreCache
	scoreFloor float64
}

// NewCrossEncoderReranker 构造纯 Cross-Encoder 重排器
func NewCrossEncoderReranker(scorer *CrossEncoderScorer) *CrossEncoderReranker {
	if scorer == nil {
		return nil
	}
	return &CrossEncoderReranker{scorer: scorer, cache: scorer.cache}
}

// SetScoreFloor 显式开启分数地板截断（floor<=0 关闭）。
// 建议值 rerankScoreFloorDefault=0.3，须按实际 cross-encoder 分域校准后开启。
func (r *CrossEncoderReranker) SetScoreFloor(floor float64) {
	r.scoreFloor = floor
}

// Strategy 策略名
func (r *CrossEncoderReranker) Strategy() string { return string(StrategyCrossEncoder) }

// Rerank 实现 Reranker 接口
func (r *CrossEncoderReranker) Rerank(ctx context.Context, query string, docs []RetrievedDoc, topK int) ([]RankedDoc, error) {
	if r == nil || r.scorer == nil {
		return nil, errors.New("cross-encoder reranker 未初始化")
	}
	docs = clampDocs(docs)
	topK = clampTopK(topK)
	if len(docs) == 0 {
		return nil, nil
	}

	scores, err := r.scorer.Score(ctx, query, docs)
	if err != nil {
		return fallbackByOriginal(docs, topK, string(StrategyCrossEncoder)), nil
	}

	ranked := make([]RankedDoc, 0, len(docs))
	for _, d := range docs {
		score, ok := scores[d.ID]
		if !ok {
			score = d.Score
		}
		ranked = append(ranked, RankedDoc{
			ID:         d.ID,
			Content:    d.Content,
			Score:      score,
			Original:   d.Score,
			Strategy:   string(StrategyCrossEncoder),
			Recomputed: true,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
	return finalize(applyScoreFloor(r.scoreFloor, ranked), topK), nil
}

const rerankScoreFloorDefault = 0.3 //nolint:unused //// 仅被 *_test.go 引用，生产路径未用

func applyScoreFloor(floor float64, ranked []RankedDoc) []RankedDoc {
	if floor <= 0 || len(ranked) == 0 {
		return ranked
	}
	kept := make([]RankedDoc, 0, len(ranked))
	for _, d := range ranked {
		if d.Score >= floor {
			kept = append(kept, d)
		}
	}
	if len(kept) == 0 {

		return ranked[:1]
	}
	return kept
}

// RRFReranker 纯 RRF 融合重排（不需要 LLM，纯算法层）
//
// 把所有候选视为"多路召回结果"，按 RRF 公式重新打分
type RRFReranker struct {
	k     int
	cache *rerankScoreCache
}

// NewRRFReranker 构造 RRF 重排器
//
// k ≤ 0 时用默认值 60（与 rrf_fusion.go 一致）
func NewRRFReranker(k int, cache *rerankScoreCache) *RRFReranker {
	if k <= 0 {
		k = 60
	}
	if cache == nil {
		cache = newRerankScoreCache(2048, time.Hour)
	}
	return &RRFReranker{k: k, cache: cache}
}

// Strategy 策略名
func (r *RRFReranker) Strategy() string { return string(StrategyRRF) }

// Rerank 实现 Reranker 接口
//
// 算法：
//  1. 按 Source 分组（vector / bm25 / rrf / unknown 等），各自视为一路召回
//  2. 对每路按 Original 分数降序得到 rank
//  3. RRF 公式：score = Σ 1 / (k + rank)
//  4. 同一 doc 在多路出现会累加分数
func (r *RRFReranker) Rerank(ctx context.Context, query string, docs []RetrievedDoc, topK int) ([]RankedDoc, error) {
	docs = clampDocs(docs)
	topK = clampTopK(topK)
	if len(docs) == 0 {
		return nil, nil
	}

	bySource := make(map[string][]RetrievedDoc, 4)
	for _, d := range docs {
		src := d.Source
		if src == "" {
			src = "default"
		}
		bySource[src] = append(bySource[src], d)
	}

	type entry struct {
		doc   RetrievedDoc
		score float64
	}
	merged := make(map[string]*entry, len(docs))
	for src, list := range bySource {
		sorted := make([]RetrievedDoc, len(list))
		copy(sorted, list)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Score > sorted[j].Score
		})
		for i, d := range sorted {
			rrfScore := 1.0 / float64(r.k+i+1)
			if e, ok := merged[d.ID]; ok {
				e.score += rrfScore
			} else {
				merged[d.ID] = &entry{doc: d, score: rrfScore}
			}
		}
		_ = src
	}

	ranked := make([]RankedDoc, 0, len(merged))
	for _, e := range merged {
		r.cache.Set(cacheKey(query, e.doc.ID), e.score)
		ranked = append(ranked, RankedDoc{
			ID:         e.doc.ID,
			Content:    e.doc.Content,
			Score:      e.score,
			Original:   e.doc.Score,
			Strategy:   string(StrategyRRF),
			Recomputed: true,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].ID < ranked[j].ID
	})
	return finalize(ranked, topK), nil
}

// HybridReranker 混合策略：先 RRF 后 Cross-Encoder
//
// 流程：
//  1. 用 RRFReranker 对所有候选重新打分（得到 RRF 排序）
//  2. 取 RRF top N（默认 = min(topK*3, 30)）送 Cross-Encoder 精排
//  3. Cross-Encoder 失败则回退 RRF 排序
type HybridReranker struct {
	rrf        *RRFReranker
	crossEnc   *CrossEncoderReranker
	cache      *rerankScoreCache
	crossRatio int
	maxCross   int
}

// NewHybridReranker 构造混合策略重排器
//
// rrfK: RRF 平滑常数（≤0 用默认 60）
// delegate: Cross-Encoder 底层评估器（LocalReranker），nil 时降级为纯 RRF
func NewHybridReranker(rrfK int, delegate RerankerInterface, cache *rerankScoreCache) *HybridReranker {
	if cache == nil {
		cache = newRerankScoreCache(2048, time.Hour)
	}
	r := &HybridReranker{
		rrf:        NewRRFReranker(rrfK, cache),
		cache:      cache,
		crossRatio: 3,
		maxCross:   30,
	}
	if delegate != nil {
		scorer := NewCrossEncoderScorer(delegate, cache)
		r.crossEnc = NewCrossEncoderReranker(scorer)
	}
	return r
}

// Strategy 策略名
func (r *HybridReranker) Strategy() string { return string(StrategyHybrid) }

// Rerank 实现 Reranker 接口
func (r *HybridReranker) Rerank(ctx context.Context, query string, docs []RetrievedDoc, topK int) ([]RankedDoc, error) {
	if r == nil {
		return nil, errors.New("hybrid reranker 未初始化")
	}
	docs = clampDocs(docs)
	topK = clampTopK(topK)
	if len(docs) == 0 {
		return nil, nil
	}

	rrfTopN := topK * r.crossRatio
	if rrfTopN > r.maxCross {
		rrfTopN = r.maxCross
	}
	if rrfTopN < topK {
		rrfTopN = topK
	}
	rrfRanked, err := r.rrf.Rerank(ctx, query, docs, rrfTopN)
	if err != nil {
		return fallbackByOriginal(docs, topK, string(StrategyHybrid)), nil
	}
	if len(rrfRanked) == 0 {
		return nil, nil
	}

	if r.crossEnc == nil {
		for i := range rrfRanked {
			rrfRanked[i].Strategy = string(StrategyHybrid)
		}
		return finalize(rrfRanked, topK), nil
	}

	candidates := make([]RetrievedDoc, 0, len(rrfRanked))
	for _, rd := range rrfRanked {
		candidates = append(candidates, RetrievedDoc{
			ID:      rd.ID,
			Content: rd.Content,
			Score:   rd.Score,
			Source:  "rrf",
		})
	}
	ceRanked, err := r.crossEnc.Rerank(ctx, query, candidates, topK)
	if err != nil || len(ceRanked) == 0 {
		for i := range rrfRanked {
			rrfRanked[i].Strategy = string(StrategyHybrid)
		}
		return finalize(rrfRanked, topK), nil
	}

	for i := range ceRanked {
		ceRanked[i].Strategy = string(StrategyHybrid)
	}
	return finalize(ceRanked, topK), nil
}

// DefaultReranker 默认 Reranker（混合策略）
//
// 用 LocalReranker（rerank.go）作为 Cross-Encoder 底层评估器
// 当 LocalReranker 配置不可达（DefaultRerankConfig().Enabled=false）时，
// 自动降级为纯 RRF 策略（仍可用，只是不走 LLM 精排）
func DefaultReranker() Reranker {
	delegate := NewLocalReranker()
	cache := newRerankScoreCache(2048, time.Hour)
	return NewHybridReranker(60, delegate, cache)
}

// NewDefaultRerankerWithConfig 显式注入 Cross-Encoder 底层评估器
//
// delegate 为 nil 时降级为纯 RRF
func NewDefaultRerankerWithConfig(delegate RerankerInterface) Reranker {
	cache := newRerankScoreCache(2048, time.Hour)
	return NewHybridReranker(60, delegate, cache)
}

const (
	maxTopK             = 20
	maxDocs             = 100
	defaultCrossEncTopN = 20
)

func clampTopK(topK int) int {
	if topK <= 0 {
		return defaultCrossEncTopN
	}
	if topK > maxTopK {
		return maxTopK
	}
	return topK
}

func clampDocs(docs []RetrievedDoc) []RetrievedDoc {
	if len(docs) <= maxDocs {
		return docs
	}
	return docs[:maxDocs]
}

func finalize(ranked []RankedDoc, topK int) []RankedDoc {
	if topK > 0 && len(ranked) > topK {
		ranked = ranked[:topK]
	}
	for i := range ranked {
		ranked[i].Rank = i + 1
	}
	return ranked
}

func fallbackByOriginal(docs []RetrievedDoc, topK int, strategy string) []RankedDoc {
	ranked := make([]RankedDoc, 0, len(docs))
	for _, d := range docs {
		ranked = append(ranked, RankedDoc{
			ID:         d.ID,
			Content:    d.Content,
			Score:      d.Score,
			Original:   d.Score,
			Strategy:   strategy,
			Recomputed: false,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].ID < ranked[j].ID
	})
	return finalize(ranked, topK)
}

// RerankerToInterfaceAdapter 把高级 Reranker 适配为旧的 RerankerInterface
//
// 用于让 HybridSearcher（hybrid_searcher.go）透明地使用新的混合策略 Reranker
type RerankerToInterfaceAdapter struct {
	reranker Reranker
}

// NewRerankerToInterfaceAdapter 构造适配器
func NewRerankerToInterfaceAdapter(reranker Reranker) *RerankerToInterfaceAdapter {
	return &RerankerToInterfaceAdapter{reranker: reranker}
}

// Rerank 实现 RerankerInterface（接受 []RerankDoc 返回 []RerankResult）
func (a *RerankerToInterfaceAdapter) Rerank(ctx context.Context, query string, docs []RerankDoc) ([]RerankResult, error) {
	if a == nil || a.reranker == nil {
		return nil, errors.New("adapter 未初始化")
	}
	retrieved := make([]RetrievedDoc, 0, len(docs))
	for _, d := range docs {
		retrieved = append(retrieved, RetrievedDoc{ID: d.ID, Content: d.Content})
	}
	ranked, err := a.reranker.Rerank(ctx, query, retrieved, len(docs))
	if err != nil {
		return nil, err
	}
	out := make([]RerankResult, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, RerankResult{ID: r.ID, Score: r.Score})
	}
	return out, nil
}

var (
	_ Reranker          = (*CrossEncoderReranker)(nil)
	_ Reranker          = (*RRFReranker)(nil)
	_ Reranker          = (*HybridReranker)(nil)
	_ RerankerInterface = (*RerankerToInterfaceAdapter)(nil)
)

// ChunksToRetrievedDocs 把 []Chunk 转为 []RetrievedDoc
//
// 用于让 HybridSearcher 现有调用点（chunks → RerankChunks）平滑接入新 Reranker
func ChunksToRetrievedDocs(chunks []Chunk, source string) []RetrievedDoc {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]RetrievedDoc, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, RetrievedDoc{
			ID:       c.ID,
			Content:  c.Content,
			Score:    c.Score,
			Source:   source,
			Metadata: c.Metadata,
		})
	}
	return out
}

// RankedDocsToChunks 把 []RankedDoc 转回 []Chunk（保留重排后顺序与分数）
func RankedDocsToChunks(ranked []RankedDoc, original map[string]Chunk) []Chunk {
	if len(ranked) == 0 {
		return nil
	}
	out := make([]Chunk, 0, len(ranked))
	for _, r := range ranked {
		c, ok := original[r.ID]
		if !ok {
			c = Chunk{ID: r.ID, Content: r.Content}
		}
		c.Score = r.Score
		out = append(out, c)
	}
	return out
}

// String 用于日志/调试
func (r RankedDoc) String() string {
	return fmt.Sprintf("RankedDoc{ID=%s, Score=%.4f, Rank=%d, Strategy=%s}", r.ID, r.Score, r.Rank, r.Strategy)
}

// DescribeReranker 返回 Reranker 的人类可读描述（测试/调试用）
func DescribeReranker(r Reranker) string {
	if r == nil {
		return "nil"
	}
	return fmt.Sprintf("Reranker(strategy=%s)", r.Strategy())
}

// TrimStrategyName 截断策略名（避免日志过长）
func TrimStrategyName(s string) string {
	if len(s) > 32 {
		return strings.TrimSpace(s[:32]) + "..."
	}
	return strings.TrimSpace(s)
}
