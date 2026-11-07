package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
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

	rows, err := s.chunkRepo.VectorSearchRows(ctx, productID, vecLiteral, topK)
	if err != nil {
		return nil, err
	}
	pairs := make([]scored, 0, len(rows))
	for _, r := range rows {
		pairs = append(pairs, scored{row: chunkRow(r), score: r.Score})
	}
	return pairs, nil
}

// toServiceChunkRows 仓储行 → service 行（同形字段直转换）。
func toServiceChunkRows(rows []knowledgeRepo.RagSearchChunkRow) []chunkRow {
	out := make([]chunkRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, chunkRow(r))
	}
	return out
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
