package ragretrieval

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"
)

// VectorRetriever pgvector HNSW 向量召回器
type VectorRetriever struct {
	db              *gorm.DB
	embeddingClient llm.EmbeddingServiceInterface
	efSearch        int
}

// NewVectorRetriever 创建向量召回器
//
// efSearch <= 0 时使用默认值 80（设计文档推荐）
// embeddingClient 通常应传入 *CachedEmbeddingClient 装饰后的实例；
// 若直接传入 *llm.EmbeddingService 也能工作，只是无缓存。
func NewVectorRetriever(db *gorm.DB, embeddingClient llm.EmbeddingServiceInterface, efSearch int) *VectorRetriever {
	if efSearch <= 0 {
		efSearch = 80
	}
	return &VectorRetriever{
		db:              db,
		embeddingClient: embeddingClient,
		efSearch:        efSearch,
	}
}

type chunkScanRow struct {
	ID         uint64  `gorm:"column:id"`
	DocumentID uint64  `gorm:"column:document_id"`
	Content    string  `gorm:"column:content"`
	Score      float64 `gorm:"column:score"`
}

// SearchVector 实现 VectorSearcher 接口（从向量检索）
func (r *VectorRetriever) SearchVector(ctx context.Context, kbID string, queryVec []float32, topK int) ([]Chunk, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("vector retriever 未初始化")
	}
	if topK <= 0 {
		topK = 50
	}
	vecLiteral := vecToPGString(queryVec)

	var rows []chunkScanRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", r.efSearch)).Error; err != nil {
			return fmt.Errorf("set hnsw.ef_search: %w", err)
		}
		sql := `
			SELECT id, document_id, content,
			       (1 - (embedding <=> ?::vector))::float8 AS score
			FROM knowledge_chunks
			WHERE embedding IS NOT NULL AND embedding_source = 'tei'
		`
		args := []any{vecLiteral}
		if kbID != "" {
			sql += " AND product_id = ?" + kbrelease.AndVisible() + " ORDER BY embedding <=> ?::vector LIMIT ?"
			args = append(args, kbID, vecLiteral, topK)
		} else {
			sql += kbrelease.AndVisible() + " ORDER BY embedding <=> ?::vector LIMIT ?"
			args = append(args, vecLiteral, topK)
		}
		return tx.Raw(sql, args...).Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return rowsToChunks(rows), nil
}

// Retrieve 向量召回
//
// 参数:
//   - productID != "" 时按产品过滤；= 0 时全产品检索
//   - query 原始查询文本（将被 embeddingClient 编码为向量）
//   - topK 返回结果数（<= 0 时使用默认值 50）
//
// 返回:
//   - []Chunk 已按相似度降序排序
//   - 维度非法 / embedding 失败 / DB 错误均返回 error
func (r *VectorRetriever) Retrieve(ctx context.Context, productID string, query string, topK int) ([]Chunk, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("vector retriever 未初始化")
	}
	if r.embeddingClient == nil {
		return nil, fmt.Errorf("embedding client 未初始化")
	}
	if topK <= 0 {
		topK = 50
	}
	cfg := r.embeddingClient.DefaultConfig()

	queryVec, err := r.embeddingClient.EmbedOne(ctx, cfg, query)
	if err != nil {
		return nil, fmt.Errorf("embedding 失败: %w", err)
	}
	expectDim := 1024
	if cfg != nil && cfg.Dimension > 0 {
		expectDim = cfg.Dimension
	}
	if len(queryVec) != expectDim {
		return nil, fmt.Errorf("embedding 维度非法: expect %d, got %d", expectDim, len(queryVec))
	}
	vecLiteral := vecToPGString(queryVec)

	var rows []chunkScanRow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", r.efSearch)).Error; err != nil {
			return fmt.Errorf("set hnsw.ef_search: %w", err)
		}
		sql := `
			SELECT id, document_id, content,
			       (1 - (embedding <=> ?::vector))::float8 AS score
			FROM knowledge_chunks
			WHERE embedding IS NOT NULL AND embedding_source = 'tei'
		`
		args := []any{vecLiteral}
		if productID != "" {
			sql += " AND product_id = ?" + kbrelease.AndVisible() + " ORDER BY embedding <=> ?::vector LIMIT ?"
			args = append(args, productID, vecLiteral, topK)
		} else {
			sql += kbrelease.AndVisible() + " ORDER BY embedding <=> ?::vector LIMIT ?"
			args = append(args, vecLiteral, topK)
		}
		return tx.Raw(sql, args...).Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		if n := r.countUnembeddedChunks(ctx, productID); n > 0 {
			// 读数带上闸门档位：召回为 0 有两种成因（没向量化 / 内容还没发布），
			// 而运维只看到"空召回"时会按第一种去重跑导入。
			logger.Warnf("[VectorRetriever] 向量召回为空（版本闸门 %s），但存在 %d 个未向量化 chunk (embed_status='pending' 或 embedding IS NULL)，疑似回填缺失；query=%q product_id=%q",
				kbrelease.ModeForLog(), n, query, productID)
		}
	}
	return rowsToChunks(rows), nil
}

func (r *VectorRetriever) countUnembeddedChunks(ctx context.Context, productID string) int64 {
	sql := `
		SELECT COUNT(*) FROM knowledge_chunks
		WHERE (embed_status = 'pending' OR embedding IS NULL)
	`
	args := []any{}
	if productID != "" {
		sql += " AND product_id = ?"
		args = append(args, productID)
	}
	var n int64
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&n).Error; err != nil {
		return 0
	}
	return n
}

func rowsToChunks(rows []chunkScanRow) []Chunk {
	out := make([]Chunk, 0, len(rows))
	for _, r := range rows {
		out = append(out, Chunk{
			ID:         strconv.FormatUint(r.ID, 10),
			DocumentID: strconv.FormatUint(r.DocumentID, 10),
			Content:    truncateContent(r.Content, 500),
			Score:      r.Score,
		})
	}
	return out
}
