// Package kbsearch RAG 检索基础设施装配点（AV-04c 批次7 刀4）。
//
// ragretrieval 的三个构造接缝（CachedEmbeddingClient 的 L2 PG 缓存句柄、
// HybridSearcher、ContextualRetrievalEnhancer）同时需要 *gorm.DB 与 llm 类型：
// 两类引用无法同进 knowledge/repository（llm→platform→repository→kbrepo 构成
// import 环）。本包是 llm×ragretrieval×gorm 的交汇点——连接经仓储的 WithConn
// 接缝取用，句柄不在 service 层过手；本包自身不持有任何句柄字段。
package kbsearch

import (
	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
)

// NewCachedEmbeddingClientFromRepo 以分段仓储的连接构造 L1+L2 缓存 embedding 客户端
// （L2 PG 缓存用仓储连接；与原 newRagSearcherWithDB 内联构造同连接同配置）。
func NewCachedEmbeddingClientFromRepo(repo *repository.KnowledgeChunkRepository, inner llm.EmbeddingServiceInterface, redis ragretrieval.RedisClient) *ragretrieval.CachedEmbeddingClient {
	return repository.WithConn(repo, func(db *gorm.DB) *ragretrieval.CachedEmbeddingClient {
		return ragretrieval.NewCachedEmbeddingClient(inner, redis, db, nil)
	})
}

// NewHybridSearcherFromRepo 以分段仓储的连接构造混合检索器
// （与原 ragretrieval.NewHybridSearcher(s.db, ...) 同连接同参数序）。
func NewHybridSearcherFromRepo(
	repo *repository.KnowledgeChunkRepository,
	embeddingClient llm.EmbeddingServiceInterface,
	reranker ragretrieval.RerankerInterface,
	llmChatClient ragretrieval.LLMChatClient,
	redisClient ragretrieval.RedisClient,
	cfg *ragretrieval.HybridSearcherConfig,
) *ragretrieval.HybridSearcher {
	return repository.WithConn(repo, func(db *gorm.DB) *ragretrieval.HybridSearcher {
		return ragretrieval.NewHybridSearcher(db, embeddingClient, reranker, llmChatClient, redisClient, cfg)
	})
}

// NewContextualEnhancerFromRepo 以分段仓储的连接构造上下文检索增强器（R-3）。
func NewContextualEnhancerFromRepo(
	repo *repository.KnowledgeChunkRepository,
	chatClient ragretrieval.LLMChatClient,
	embeddingSvc llm.EmbeddingServiceInterface,
	cfg *ragretrieval.ContextualEnhancerConfig,
) *ragretrieval.ContextualRetrievalEnhancer {
	return repository.WithConn(repo, func(db *gorm.DB) *ragretrieval.ContextualRetrievalEnhancer {
		return ragretrieval.NewContextualRetrievalEnhancer(db, chatClient, embeddingSvc, cfg)
	})
}
