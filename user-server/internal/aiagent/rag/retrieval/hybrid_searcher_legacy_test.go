package ragretrieval

import "context"

// 历史重排测试保留原返回值契约，生产同时取得可测阶段结果。
func (h *HybridSearcher) applyRerankToFused(ctx context.Context, query string, fused []Chunk, topK int) ([]Chunk, int) {
	result, count, _ := h.applyRerankObserved(ctx, query, fused, topK)
	return result, count
}
