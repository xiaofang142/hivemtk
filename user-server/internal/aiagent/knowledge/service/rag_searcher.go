package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/kbsearch"
	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
)

type cacheRedisAdapter struct {
	c cache.Cache
}

func (a *cacheRedisAdapter) Get(ctx context.Context, key string) (string, error) {
	if a == nil || a.c == nil {
		return "", fmt.Errorf("redis adapter 未初始化")
	}
	return a.c.Get(ctx, key)
}

func (a *cacheRedisAdapter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if a == nil || a.c == nil {
		return nil
	}
	return a.c.Set(ctx, key, value, ttl)
}

func getGlobalRedisClient() ragretrieval.RedisClient {
	if !cache.GlobalIsRedis() {
		return nil
	}
	return &cacheRedisAdapter{c: cache.GetGlobalCache()}
}

type RagSearcher struct {
	chunkRepo        *knowledgeRepo.KnowledgeChunkRepository
	embeddingService llm.EmbeddingServiceInterface
	hybridSearcher   *ragretrieval.HybridSearcher
}

// NewRagSearcher 创建全局 RAG 检索器（自动初始化 TEI 客户端 + HybridSearcher）
//
// 自动启用 HybridSearcher，无需外部手动调用 EnableHybridSearcher。
// 便捷入口同样走 CachedEmbeddingClient 装饰（L1 Redis + L2 PG）与默认 rerank 注入：
//   - embedding 走 NewCachedEmbeddingClient（全局 Redis 可达时启用 L1，否则仅 L2 PG）
//   - 依据 DefaultRerankConfig().Enabled 注入 NewLocalReranker（默认启用重排）
//   - llmChatClient 为 nil：HyDE / Multi-Query 默认关闭；redisClient 仅用于查询改写 L1 缓存
//
// 需要完整能力（HyDE / Multi-Query / 显式覆盖）的生产环境可用 EnableHybridSearcher 注入。
// AV-04c 批次7 刀4：连接经分段仓储（Default 构造自取全局句柄），句柄不在 service 过手；
// ragretrieval 构造接缝经 kbsearch（llm×ragretrieval×gorm 交汇点）。
func NewRagSearcher() *RagSearcher {
	return newRagSearcherWithRepo(knowledgeRepo.NewKnowledgeChunkRepositoryDefault())
}

// NewRagSearcherWithRepo 仓储注入构造（装配方/测试显式注入分段仓储）。
func NewRagSearcherWithRepo(chunkRepo *knowledgeRepo.KnowledgeChunkRepository) *RagSearcher {
	return newRagSearcherWithRepo(chunkRepo)
}

func newRagSearcherWithRepo(chunkRepo *knowledgeRepo.KnowledgeChunkRepository) *RagSearcher {
	s := &RagSearcher{
		chunkRepo:        chunkRepo,
		embeddingService: llm.NewEmbeddingService(),
	}
	dispatcher := llm.NewDispatcher(llm.NewLLMService())
	_ = dispatcher

	embClient := kbsearch.NewCachedEmbeddingClientFromRepo(chunkRepo, s.embeddingService, getGlobalRedisClient())

	var reranker ragretrieval.RerankerInterface
	if rc := ragretrieval.DefaultRerankConfig(); rc != nil && rc.Enabled {
		reranker = ragretrieval.NewLocalReranker()
	}

	s.EnableHybridSearcher(reranker, nil, getGlobalRedisClient(), embClient, ragretrieval.DefaultHybridSearcherConfig())
	return s
}

// SetHybridSearcher 注入混合检索器（DI，用于运行时切换检索策略）
//
// 传入 nil 等价于禁用混合检索（回退到 legacy vector + BM25-lite）
func (s *RagSearcher) SetHybridSearcher(hs *ragretrieval.HybridSearcher) {
	s.hybridSearcher = hs
}

// HybridSearcher 返回当前委托的混合检索器（可能为 nil）
func (s *RagSearcher) HybridSearcher() *ragretrieval.HybridSearcher {
	return s.hybridSearcher
}

// EnableHybridSearcher 构造依赖并启用混合检索（一站式便捷方法）
//
// 参数：
//   - reranker: 重排器（bge-reranker-v2-m3）；nil 时禁用重排
//   - llmChatClient: LLM 对话客户端（用于 HyDE / Multi-Query）；nil 时禁用查询改写
//   - redisClient: Redis 客户端（用于查询改写缓存）；nil 时跳过 L1 缓存
//   - embeddingClient: 已被 CachedEmbeddingClient 装饰的 Embedding 服务；nil 时使用内部默认
//   - cfg: 检索参数；nil 用默认配置
//
// 调用此方法后，Search / SearchIndex 将优先委托给 HybridSearcher
func (s *RagSearcher) EnableHybridSearcher(
	reranker ragretrieval.RerankerInterface,
	llmChatClient ragretrieval.LLMChatClient,
	redisClient ragretrieval.RedisClient,
	embeddingClient llm.EmbeddingServiceInterface,
	cfg *ragretrieval.HybridSearcherConfig,
) {
	if embeddingClient == nil {
		embeddingClient = s.embeddingService
	}
	s.hybridSearcher = kbsearch.NewHybridSearcherFromRepo(
		s.chunkRepo, embeddingClient, reranker, llmChatClient, redisClient, cfg,
	)
}

// Search 全产品检索（实现 SalesEngine 的 RAGSearcher 接口）
//
// 流程：
//  1. 若 hybridSearcher 已注入 → 委托给 HybridSearcher（混合检索 + RRF + 重排）
//  2. 否则走 legacy：TEI 编码 query → 1024 维向量 → pgvector <=> HNSW → 失败回退 BM25-lite
func (s *RagSearcher) Search(ctx context.Context, query string, topK int) ([]RAGChunk, error) {
	if !s.chunkRepo.Connected() {
		return nil, nil
	}
	if topK <= 0 {
		topK = DefaultTopK()
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	if s.hybridSearcher != nil {
		chunks, err := s.hybridSearcher.Search(ctx, query, topK)
		if err == nil {
			return s.rankRAGChunks(ctx, chunksToRAGChunks(chunks)), nil
		}
		logger.Errorf("[RagSearcher] hybrid search failed, fallback to legacy: %v", err)
	} else {
		logger.Debugf("[RagSearcher] hybrid searcher not enabled, use legacy vector+BM25-lite")
	}

	rows, vecErr := s.vectorSearch(ctx, "", query, topK)
	if vecErr == nil && len(rows) > 0 {
		return s.rankRAGChunks(ctx, s.toRAGChunks(rows)), nil
	}
	if vecErr != nil {
		logger.Errorf("[RagSearcher] vector search failed, fallback to BM25-lite: %v", vecErr)
	}

	bm25, _ := s.bm25SearchAll(ctx, query, topK)
	if len(bm25) == 0 && vecErr != nil {
		logger.Warnf("[RagSearcher] 向量检索失败且 BM25 无命中（query=%q）：可能 knowledge_chunks.embedding 缺失或 embedding 服务不可达，导致召回为 0；请检查 chunk 向量化状态与 EMBEDDING_BASE_URL", query)
	}
	return s.rankRAGChunks(ctx, bm25), nil
}

// SearchIndex 在指定产品下检索 query
//
// 流程与 Search 相同，但带 product_id 过滤。
// metadata 为可选附加字段过滤条件（如 {"customer_id":"123","order_id":"A01"}），
// 用于把检索范围收敛到特定业务上下文（如某客户的订单知识）。
func (s *RagSearcher) SearchIndex(ctx context.Context, productID string, query string, topK int, metadata map[string]string) ([]MerchantRAGChunk, error) {
	if !s.chunkRepo.Connected() {
		return nil, nil
	}
	if topK <= 0 {
		topK = DefaultTopK()
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	if s.hybridSearcher != nil {
		chunks, err := s.hybridSearcher.SearchIndex(ctx, productID, query, topK)
		if err == nil {
			return s.rankMerchantChunks(ctx, filterMerchantChunksByMetadata(chunksToMerchantChunks(chunks), metadata)), nil
		}
		logger.Errorf("[RagSearcher] hybrid search (product=%s) failed, fallback to legacy: %v", productID, err)
	}

	rows, vecErr := s.vectorSearch(ctx, productID, query, topK)
	if vecErr == nil && len(rows) > 0 {
		return s.rankMerchantChunks(ctx, filterMerchantChunksByMetadata(s.toMerchantChunks(rows), metadata)), nil
	}
	if vecErr != nil {
		logger.Errorf("[RagSearcher] vector search failed (product=%s), fallback to BM25-lite: %v", productID, vecErr)
	}

	bm25, _ := s.bm25SearchIndex(ctx, productID, query, topK)
	filtered := filterMerchantChunksByMetadata(bm25, metadata)
	if len(filtered) == 0 && vecErr != nil {
		logger.Warnf("[RagSearcher] 向量检索失败且 BM25 无命中（product=%s, query=%q）：可能 knowledge_chunks.embedding 缺失或 embedding 服务不可达，导致召回为 0；请检查 chunk 向量化状态", productID, query)
	}
	return s.rankMerchantChunks(ctx, filtered), nil
}

// SearchIndexWithConfig 使用指定的 embedding/rerank 配置做 per 知识库检索（覆盖全局默认）
//
// 实现：每次请求用传入的 embedding client + reranker 构造临时 HybridSearcher，
// 不修改共享的 s.hybridSearcher 单例（避免并发竞态）。结果经 chunksToMerchantChunks 转换。
func (s *RagSearcher) SearchIndexWithConfig(ctx context.Context, productID string, query string, topK int, embClient llm.EmbeddingServiceInterface, reranker ragretrieval.RerankerInterface) ([]MerchantRAGChunk, error) {
	if !s.chunkRepo.Connected() {
		return nil, fmt.Errorf("db 未初始化")
	}
	cfg := ragretrieval.DefaultHybridSearcherConfig()
	hs := kbsearch.NewHybridSearcherFromRepo(s.chunkRepo, embClient, reranker, nil, nil, cfg)
	chunks, err := hs.SearchIndex(ctx, productID, query, topK)
	if err != nil {
		return nil, err
	}
	return s.rankMerchantChunks(ctx, chunksToMerchantChunks(chunks)), nil
}

func clampWeight(w float64) float64 {
	if w <= 0 {
		return 1.0
	}
	if w < 0.1 {
		return 0.1
	}
	if w > 3.0 {
		return 3.0
	}
	return w
}

func (s *RagSearcher) loadChunkWeights(ctx context.Context, ids []uint64) map[uint64]float64 {
	out := make(map[uint64]float64, len(ids))
	if !s.chunkRepo.Connected() || len(ids) == 0 {
		return out
	}
	rows, err := s.chunkRepo.ChunkWeights(ctx, ids)
	if err != nil {
		logger.Warnf("[RagSearcher] loadChunkWeights failed: %v", err)
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Weight
	}
	return out
}

func (s *RagSearcher) rankRAGChunks(ctx context.Context, chunks []RAGChunk) []RAGChunk {
	if len(chunks) == 0 {
		return chunks
	}
	ids := make([]uint64, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if c.ChunkID != "" {
			chunkIDs = append(chunkIDs, c.ChunkID)
		}
		if id, err := strconv.ParseUint(c.ChunkID, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	tracing.RecordRecalledChunks(ctx, chunkIDs)
	// 记录这批召回里有多少条会被版本闸门挡掉，是"要不要开闸"这个决定的数据来源；
	// off/on 档零开销（LogWouldHide 自己判档）。
	s.chunkRepo.LogWouldHide(ctx, ids, "", "rankRAGChunks")
	weights := s.loadChunkWeights(ctx, ids)
	type ranked struct {
		c   RAGChunk
		eff float64
		w   float64
	}
	items := make([]ranked, len(chunks))
	for i, c := range chunks {
		w := 1.0
		if id, err := strconv.ParseUint(c.ChunkID, 10, 64); err == nil {
			if wt, ok := weights[id]; ok && wt > 0 {
				w = wt
			}
		}
		wf := clampWeight(w)
		items[i] = ranked{c, c.Score * wf, w}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].eff > items[j].eff })
	out := make([]RAGChunk, len(items))
	for i, it := range items {
		it.c.Weight = it.w
		it.c.Score = it.eff
		out[i] = it.c
	}
	return out
}

func (s *RagSearcher) rankMerchantChunks(ctx context.Context, chunks []MerchantRAGChunk) []MerchantRAGChunk {
	if len(chunks) == 0 {
		return chunks
	}
	ids := make([]uint64, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))
	for _, c := range chunks {
		chunkIDs = append(chunkIDs, strconv.FormatUint(c.ID, 10))
		ids = append(ids, c.ID)
	}
	tracing.RecordRecalledChunks(ctx, chunkIDs)
	s.chunkRepo.LogWouldHide(ctx, ids, "", "rankMerchantChunks")
	weights := s.loadChunkWeights(ctx, ids)
	type ranked struct {
		c   MerchantRAGChunk
		eff float64
		w   float64
	}
	items := make([]ranked, len(chunks))
	for i, c := range chunks {
		w := 1.0
		if wt, ok := weights[c.ID]; ok && wt > 0 {
			w = wt
		}
		wf := clampWeight(w)
		items[i] = ranked{c, c.Score * wf, w}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].eff > items[j].eff })
	out := make([]MerchantRAGChunk, len(items))
	for i, it := range items {
		it.c.Weight = it.w
		it.c.Score = it.eff
		out[i] = it.c
	}
	return out
}
