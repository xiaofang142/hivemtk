package service

import (
	"context"
	"sort"
	"strconv"
)

// SearchProducts 在指定的多个产品（知识库）下分别检索后合并。
//
// 背景：SalesEngine.recallRAG 历史上只调 Search（全产品检索），智能体上配置的
// rag_product_ids 从未生效——任意接入的新知识都会被所有智能体全局召回（污染）。
// 本方法让 rag_product_ids 真正生效：优先分产品召回，无命中时由调用方回退全局。
func (s *RagSearcher) SearchProducts(ctx context.Context, productIDs []string, query string, topK int) ([]RAGChunk, error) {
	if s.db == nil {
		return nil, nil
	}
	if topK <= 0 {
		topK = DefaultTopK()
	}
	seen := make(map[string]struct{})
	merged := make([]RAGChunk, 0, len(productIDs)*topK)
	for _, pid := range productIDs {
		if pid == "" {
			continue
		}
		chunks, err := s.SearchIndex(ctx, pid, query, topK, nil)
		if err != nil || len(chunks) == 0 {
			continue
		}
		for _, c := range chunks {
			key := strconv.FormatUint(c.DocumentID, 10) + ":" + strconv.FormatUint(c.ID, 10)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, RAGChunk{
				Content: c.Content,
				Source:  "product:" + pid,
				Score:   c.Score,
				DocID:   strconv.FormatUint(c.DocumentID, 10),
				ChunkID: strconv.FormatUint(c.ID, 10),
				Weight:  c.Weight,
			})
		}
	}
	if len(merged) == 0 {
		return nil, nil
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}
