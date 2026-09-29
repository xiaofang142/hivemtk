package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/pkg/kbrelease"
)

func (s *RagSearcher) vectorSearch(ctx context.Context, productID string, query string, topK int) ([]scored, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service 未初始化")
	}
	cfg := s.embeddingService.DefaultConfig()

	queryVec, err := s.embeddingService.EmbedOne(ctx, cfg, query)
	if err != nil {
		return nil, fmt.Errorf("TEI 编码失败: %w", err)
	}

	vecLiteral := vecToPGString(queryVec)

	var rows []chunkRow
	if productID != "" {
		sql := `
			SELECT id, document_id, content,
			       (1 - (embedding <=> ?::vector))::float8 AS score
			FROM knowledge_chunks
			WHERE embedding IS NOT NULL
			  AND embedding_source = 'tei'
			  AND product_id = ?` + kbrelease.AndVisible() + `
			ORDER BY embedding <=> ?::vector
			LIMIT ?
		`
		if err := s.db.WithContext(ctx).Raw(sql, vecLiteral, productID, vecLiteral, topK).Scan(&rows).Error; err != nil {
			return nil, err
		}
	} else {
		sql := `
			SELECT id, document_id, content,
			       (1 - (embedding <=> ?::vector))::float8 AS score
			FROM knowledge_chunks
			WHERE embedding IS NOT NULL
			  AND embedding_source = 'tei'` + kbrelease.AndVisible() + `
			ORDER BY embedding <=> ?::vector
			LIMIT ?
		`
		if err := s.db.WithContext(ctx).Raw(sql, vecLiteral, vecLiteral, topK).Scan(&rows).Error; err != nil {
			return nil, err
		}
	}
	pairs := make([]scored, 0, len(rows))
	for _, r := range rows {
		pairs = append(pairs, scored{row: r, score: r.Score})
	}
	return pairs, nil
}

func vecToPGString(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 12)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
