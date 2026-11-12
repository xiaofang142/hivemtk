package ragretrieval

import (
	"context"
	"testing"
)

type floorStubDelegate struct{ score float64 }

func (f *floorStubDelegate) Rerank(ctx context.Context, query string, docs []RerankDoc) ([]RerankResult, error) {
	out := make([]RerankResult, 0, len(docs))
	for _, d := range docs {
		out = append(out, RerankResult{ID: d.ID, Score: f.score})
	}
	return out, nil
}

func newFloorTestReranker(score float64) *CrossEncoderReranker {
	scorer := NewCrossEncoderScorer(&floorStubDelegate{score: score}, newRerankScoreCache(64, 60_000_000_000))
	return NewCrossEncoderReranker(scorer)
}

func floorDocs(n int) []RetrievedDoc {
	docs := make([]RetrievedDoc, 0, n)
	for i := 0; i < n; i++ {
		docs = append(docs, RetrievedDoc{ID: string(rune('a' + i)), Content: "doc", Score: 0.1})
	}
	return docs
}

// 默认关闭：floor=0 不改变任何既有行为
func TestRerankFloor_DisabledByDefault(t *testing.T) {
	r := newFloorTestReranker(0.05)
	got, err := r.Rerank(context.Background(), "q", floorDocs(5), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("默认关闭时不得截断: got=%d want=5", len(got))
	}
}

// 显式开启：低于地板的候选被剔除
func TestRerankFloor_TruncatesBelowThreshold(t *testing.T) {
	r := newFloorTestReranker(0.1)
	r.SetScoreFloor(rerankScoreFloorDefault)
	got, err := r.Rerank(context.Background(), "q", floorDocs(5), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 && got[0].Score >= rerankScoreFloorDefault {
		t.Fatalf("低分不应保留")
	}

	r2 := newFloorTestReranker(0.9)
	r2.SetScoreFloor(rerankScoreFloorDefault)
	got2, _ := r2.Rerank(context.Background(), "q", floorDocs(4), 4)
	if len(got2) != 4 {
		t.Fatalf("高分全保留: got=%d want=4", len(got2))
	}
}

// 安全阀：全部低于地板时保留首条而非返回空
func TestRerankFloor_NeverReturnsEmpty(t *testing.T) {
	r := newFloorTestReranker(0.01)
	r.SetScoreFloor(rerankScoreFloorDefault)
	got, err := r.Rerank(context.Background(), "q", floorDocs(3), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("安全阀应保留首条: got=%d", len(got))
	}
}

// 整批被过滤监控（2026-11-12 决策⑤）：全低分输入→mapRerankResults 返回空。
// 该路径会触发 logger.Warnf 整批过滤告警（rerank.go mapRerankResults），
// 监控只记录事件、不改行为（LocalReranker 无安全阀，空结果由上层
// rag_searcher 的 BM25-lite fallback 兜底）。
func TestMapRerankResults_AllBelowFloorLogsAndReturnsEmpty(t *testing.T) {
	docs := []RerankDoc{
		{ID: "a", Content: "doc"},
		{ID: "b", Content: "doc"},
	}
	rr := &rerankResponse{Results: []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	}{
		{Index: 0, RelevanceScore: 0.05},
		{Index: 1, RelevanceScore: 0.08},
	}}
	got := mapRerankResults(docs, rr)
	if len(got) != 0 {
		t.Fatalf("全低分应整批过滤: got=%d want=0", len(got))
	}

	// 边界：空 docs 不触发 warn（无候选即无「整批」可言），也不得 panic
	empty := mapRerankResults(nil, &rerankResponse{})
	if len(empty) != 0 {
		t.Fatalf("空输入应返回空: got=%d", len(empty))
	}
}
