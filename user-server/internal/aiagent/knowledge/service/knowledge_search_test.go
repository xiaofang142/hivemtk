package service

import (
	"testing"
)

// D-27 回归：检索结果的映射与去重。
//
// 修复前 KnowledgeService.Search 调用全库 Search(ctx,query,topK) 并把请求的 productID
// 回填到每条结果上——别的产品的文档会被标成本产品返回（跨产品串数据），
// 且同一分段经向量路与 BM25 路各命中一次时会出现两条完全相同的记录。
func TestKnowledgeChunkFromMerchant_KeepsHitFields(t *testing.T) {
	got := knowledgeChunkFromMerchant("prod-a", MerchantRAGChunk{
		ID:         101,
		DocumentID: 9,
		Content:    "段落内容",
		Score:      0.87,
		Weight:     2,
	})
	if got.ID != 101 || got.DocumentID != 9 {
		t.Fatalf("命中的 id/document_id 未透传: %+v", got)
	}
	if got.ProductID != "prod-a" {
		t.Fatalf("product_id 应为请求产品: %q", got.ProductID)
	}
	if got.SimilarityScore != 0.87 {
		t.Fatalf("similarity_score 未透传命中分数: %v", got.SimilarityScore)
	}
	if got.Weight != 2 {
		t.Fatalf("weight 未透传: %v", got.Weight)
	}
}

func TestKnowledgeChunkFromMerchant_DefaultWeight(t *testing.T) {
	got := knowledgeChunkFromMerchant("prod-a", MerchantRAGChunk{ID: 1, Weight: 0})
	if got.Weight != 1 {
		t.Fatalf("weight 缺省应回落为 1，实际 %v", got.Weight)
	}
}

func TestDedupeMerchantChunks(t *testing.T) {
	chunks := []MerchantRAGChunk{
		{ID: 11, DocumentID: 1, Content: "a"},
		{ID: 11, DocumentID: 1, Content: "a"}, // 向量路/BM25 路重复命中
		{ID: 12, DocumentID: 1, Content: "b"},
		{ID: 0, DocumentID: 2, Content: "c"}, // 无分段 ID 时按 文档+内容 去重
		{ID: 0, DocumentID: 2, Content: "c"},
	}
	got := dedupeMerchantChunks(chunks)
	if len(got) != 3 {
		t.Fatalf("去重后应剩 3 条，实际 %d: %+v", len(got), got)
	}
	if got[0].ID != 11 || got[1].ID != 12 || got[2].Content != "c" {
		t.Fatalf("去重顺序/内容不符: %+v", got)
	}
}
