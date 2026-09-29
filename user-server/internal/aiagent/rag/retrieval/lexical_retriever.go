package ragretrieval

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/kbrelease"
)

type LexicalRetriever struct {
	db *gorm.DB
}

// NewLexicalRetriever 创建词法召回器
func NewLexicalRetriever(db *gorm.DB) *LexicalRetriever {
	return &LexicalRetriever{db: db}
}

func (r *LexicalRetriever) Retrieve(ctx context.Context, productID string, query string, topK int) ([]Chunk, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("bm25 retriever 未初始化")
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = 50
	}

	if rows, err := r.tryTSQuery(ctx, productID, query, topK, "contextual_tsv", "zh_rag"); err == nil && len(rows) > 0 {
		return rows, nil
	}

	if rows, err := r.tryTSQuery(ctx, productID, query, topK, "content_tsv", "simple"); err == nil && len(rows) > 0 {
		return rows, nil
	}

	return r.ilikeFallback(ctx, productID, query, topK)
}

// SearchKeyword 实现 KeywordSearcher 接口
func (r *LexicalRetriever) SearchKeyword(ctx context.Context, kbID string, query string, topK int) ([]Chunk, error) {
	return r.Retrieve(ctx, kbID, query, topK)
}

var allowedTSCols = map[string]string{
	"contextual_tsv": "contextual_tsv",
	"content_tsv":    "content_tsv",
}

var allowedTSConfigs = map[string]string{
	"zh_rag":  "zh_rag",
	"simple":  "simple",
	"english": "english",
}

func (r *LexicalRetriever) tryTSQuery(ctx context.Context, productID string, query string, topK int, tsvCol, tsConfig string) ([]Chunk, error) {
	safeCol, ok := allowedTSCols[tsvCol]
	if !ok {
		return nil, fmt.Errorf("invalid tsvector column: %s", tsvCol)
	}
	safeConfig, ok := allowedTSConfigs[tsConfig]
	if !ok {
		return nil, fmt.Errorf("invalid text search config: %s", tsConfig)
	}
	sql := fmt.Sprintf(`
		SELECT id, document_id, content,
		       ts_rank(%s, plainto_tsquery('%s', ?))::float8 AS score
		FROM knowledge_chunks
		WHERE %s @@ plainto_tsquery('%s', ?)
	`, safeCol, safeConfig, safeCol, safeConfig)
	args := []any{query}
	if productID != "" {
		sql += " AND product_id = ?" + kbrelease.AndVisible() + " ORDER BY score DESC LIMIT ?"
		args = append(args, query, productID, topK)
	} else {
		sql += kbrelease.AndVisible() + " ORDER BY score DESC LIMIT ?"
		args = append(args, query, topK)
	}
	var rows []chunkScanRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToChunks(rows), nil
}

func (r *LexicalRetriever) ilikeFallback(ctx context.Context, productID string, query string, topK int) ([]Chunk, error) {
	escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(query)
	pattern := "%" + escaped + "%"
	sql := `
		SELECT id, document_id, content, 1.0::float8 AS score
		FROM knowledge_chunks
		WHERE content ILIKE ?
	`
	args := []any{pattern}
	if productID != "" {
		sql += " AND product_id = ?" + kbrelease.AndVisible() + " ORDER BY id DESC LIMIT ?"
		args = append(args, productID, topK)
	} else {
		sql += kbrelease.AndVisible() + " ORDER BY id DESC LIMIT ?"
		args = append(args, topK)
	}
	var rows []chunkScanRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToChunks(rows), nil
}
