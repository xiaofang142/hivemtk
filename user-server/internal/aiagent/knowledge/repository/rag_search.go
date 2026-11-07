// rag_search.go RAG 检索读口仓储（AV-04c 批次7 刀4 自 service/rag_searcher*.go 下沉）。
//
// RagSearcher 的三段直查（chunk 权重、BM25-lite 扫描、向量召回）与两处句柄传递
// （kbrelease.LogWouldHide、ragretrieval.NewHybridSearcher）收进本包：SQL/谓词/
// 排序/可见性闸逐字保留，service 只留打分、排序、映射逻辑。
package repository

import (
	"context"

	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/pgvector"

	"gorm.io/gorm"
)

// RagSearchChunkRow 检索行（id/document_id/content[/score]）。
type RagSearchChunkRow struct {
	ID         uint64
	DocumentID uint64
	Content    string
	Score      float64
}

// RagSearchWeightRow 权重行（id → weight）。
type RagSearchWeightRow struct {
	ID     uint64
	Weight float64
}

// ChunkWeights 批量读分段权重（id, weight；逐字保留原 SQL）。
func (r *KnowledgeChunkRepository) ChunkWeights(ctx context.Context, ids []uint64) ([]RagSearchWeightRow, error) {
	var rows []RagSearchWeightRow
	err := r.db.WithContext(ctx).Table("knowledge_chunks").Select("id, weight").Where("id IN ?", ids).Scan(&rows).Error
	return rows, err
}

// BM25ScanRows BM25-lite 全库扫描（可见性闸内取正文列；逐字保留原 SQL，
// 含恒真谓词 "embedding IS NULL OR embedding IS NOT NULL"）。
func (r *KnowledgeChunkRepository) BM25ScanRows(ctx context.Context, limit int) ([]RagSearchChunkRow, error) {
	var rows []RagSearchChunkRow
	q := kbrelease.WhereVisible(r.db.WithContext(ctx).
		Table("knowledge_chunks").
		Select("id, document_id, content").
		Where("embedding IS NULL OR embedding IS NOT NULL"))
	if err := q.Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// BM25ScanRowsByProduct BM25-lite 产品内扫描（可见性闸 + product_id；逐字保留原 SQL）。
func (r *KnowledgeChunkRepository) BM25ScanRowsByProduct(ctx context.Context, productID string, limit int) ([]RagSearchChunkRow, error) {
	var rows []RagSearchChunkRow
	q := kbrelease.WhereVisible(r.db.WithContext(ctx).
		Table("knowledge_chunks").
		Select("id, document_id, content").
		Where("product_id = ?", productID))
	if err := q.Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// VectorSearchRows 向量召回（pgvector <=> 距离排序 + 可见性闸；逐字保留原 SQL）。
// vecLiteral 由调用方（service 层 TEI 编码后）传入，经 ?::vector 参数绑定。
func (r *KnowledgeChunkRepository) VectorSearchRows(ctx context.Context, productID, vecLiteral string, topK int) ([]RagSearchChunkRow, error) {
	var rows []RagSearchChunkRow
	q := r.db.WithContext(ctx).Table("knowledge_chunks").
		Select("id, document_id, content, (1 - (embedding <=> ?::vector))::float8 AS score", vecLiteral).
		Where("embedding IS NOT NULL AND embedding_source = 'tei'")
	if productID != "" {
		q = q.Where("product_id = ?"+kbrelease.AndVisible(), productID)
	} else if vis := kbrelease.AndVisible(); vis != "" {
		q = q.Where(trimPrefixAND(vis))
	}
	q = q.Order(pgvector.OrderByEmbeddingDistance(vecLiteral)).Limit(topK)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func trimPrefixAND(s string) string {
	const prefix = " AND "
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// LogWouldHide 版本闸 would-hide 观测（句柄不出仓储；场景标记由调用方传）。
func (r *KnowledgeChunkRepository) LogWouldHide(ctx context.Context, ids []uint64, productID, where string) {
	kbrelease.LogWouldHide(ctx, r.db, ids, productID, where)
}

// Connected 句柄就绪探针（纯内存访问器，无 DB 操作；替代原 service 侧 s.db == nil 判断）。
func (r *KnowledgeChunkRepository) Connected() bool { return r != nil && r.db != nil }

// WithConn 把仓储连接交给 build 构造检索基础设施（ragretrieval/llm 类型无法进
// 本包——llm→platform→repository→kbrepo 构成 import 环；build 由 kbsearch 等
// 基础设施装配包实现，句柄不在 service 层过手）。仓储为 nil 时传 nil 连接，
// 与原 service 直持 nil 句柄的构造语义一致。
func WithConn[T any](r *KnowledgeChunkRepository, build func(db *gorm.DB) T) T {
	if r == nil {
		var zero *gorm.DB
		return build(zero)
	}
	return build(r.db)
}

// HybridSearcher / ContextualRetrievalEnhancer 构造已随批次7 改走
// kbsearch.NewHybridSearcherFromRepo / kbsearch.NewContextualEnhancerFromRepo
//（llm×ragretrieval×gorm 交汇点；llm→platform→repository→kbrepo 环使本包无法
// 直引 llm 类型），本包不再直构。
