package service

import (
	"context"
	"sort"

	"hivemtk-user/internal/pkg/utils/bm25"
)

func (s *RagSearcher) bm25SearchAll(ctx context.Context, query string, topK int) ([]RAGChunk, error) {
	if !s.chunkRepo.Connected() {
		return nil, nil
	}
	terms := bm25.Tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := s.chunkRepo.BM25ScanRows(ctx, BM25ScanLimit())
	if err != nil {
		return nil, err
	}
	return s.bm25RankAndReturn(toServiceChunkRows(rows), terms, topK), nil
}

func (s *RagSearcher) bm25SearchIndex(ctx context.Context, productID string, query string, topK int) ([]MerchantRAGChunk, error) {
	if !s.chunkRepo.Connected() {
		return nil, nil
	}
	terms := bm25.Tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	repoRows, err := s.chunkRepo.BM25ScanRowsByProduct(ctx, productID, BM25ScanLimit())
	if err != nil {
		return nil, err
	}
	rows := toServiceChunkRows(repoRows)
	pairs := s.bm25Rank(rows, terms)
	if len(pairs) > topK {
		pairs = pairs[:topK]
	}
	result := make([]MerchantRAGChunk, 0, len(pairs))
	for _, p := range pairs {
		result = append(result, MerchantRAGChunk{
			ID:         p.row.ID,
			DocumentID: p.row.DocumentID,
			Content:    truncateText(p.row.Content, ChunkContentPreview()),
			Score:      p.score,
		})
	}
	return result, nil
}

func (s *RagSearcher) bm25RankAndReturn(rows []chunkRow, terms []string, topK int) []RAGChunk {
	pairs := s.bm25Rank(rows, terms)
	if len(pairs) > topK {
		pairs = pairs[:topK]
	}
	return s.toRAGChunks(pairs)
}

type scored struct {
	row   chunkRow
	score float64
}

func (s *RagSearcher) bm25Rank(rows []chunkRow, terms []string) []scored {
	pairs := make([]scored, 0, len(rows))
	for _, r := range rows {
		sc := bm25.ScoreText(r.Content, terms)
		if sc > 0 {
			pairs = append(pairs, scored{row: r, score: sc})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})
	return pairs
}

// ScoreText BM25-lite 文本打分(公开版,供跨包调用)
//
// 薄包装，实现见 internal/pkg/utils/bm25.ScoreText。
func ScoreText(text string, terms []string) float64 {
	return bm25.ScoreText(text, terms)
}
