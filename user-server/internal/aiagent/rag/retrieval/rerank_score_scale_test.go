package ragretrieval

import (
	"encoding/json"
	"testing"
)

// llama.cpp 实测响应：{"results":[{"index":0,"relevance_score":4.512656211853027},
// {"index":1,"relevance_score":-3.311701774597168}]}
// （2026-09-19，:8209 --reranking + bge-reranker-v2-m3，query「猫」对 4 篇文档打分）
// 相关文档 logit=+4.51、无关 -9.23，即返回原始 logit 而非 TEI 的概率。
// 不归一的话 rerankScoreFloor=0.3 会把 (-∞, 0.3) 区间整体判到地板以下，
// 重排结果恒空 → [Hybrid] 走「视为知识不足」降级分支，重排等于没生效。
func TestNormalizeRerankScores_LogitRange(t *testing.T) {
	logits := []float64{4.512656, -3.311702, -6.427562, -9.2269}
	got := normalizeRerankScores(logits)
	for i, s := range got {
		if s < 0 || s > 1 {
			t.Fatalf("归一后必须落在 [0,1]，got[%d]=%v", i, s)
		}
	}
	if got[0] <= rerankScoreFloor {
		t.Fatalf("相关文档 logit=4.51 归一后应高于地板 0.3，got=%v", got[0])
	}
	// logit=-0.85 → 概率 0.299，刚好压在地板线下；不归一时它是 -0.85，
	// 与 logit=+0.2（概率 0.55，本该过线）一起被误杀。这里校验的是量纲换算本身。
	if one := normalizeRerankScores([]float64{-0.85}); one[0] > 0.3 || one[0] < 0.29 {
		t.Fatalf("logit=-0.85 应换算为 ~0.299，got=%v", one[0])
	}
	// 同批只要有越界分，落在 [0,1] 的那几个（logit=0.2）也必须一起换算，
	// 否则一次响应里两种量纲混用，排序与地板都失去意义。
	batch := normalizeRerankScores([]float64{4.51, 0.2, -6.43})
	if batch[1] <= rerankScoreFloor {
		t.Fatalf("同批 logit=0.2 应随越界分一起 sigmoid 到 ~0.55，got=%v", batch[1])
	}
}

// 反向保护：TEI / 云端已在概率域，不得二次 sigmoid（否则 0.92 会被压成 0.715）。
func TestNormalizeRerankScores_ProbabilityRangeUntouched(t *testing.T) {
	probs := []float64{0.92, 0.31, 0.05, 1.0, 0.0}
	got := normalizeRerankScores(probs)
	for i, s := range probs {
		if got[i] != s {
			t.Fatalf("概率域响应必须原样返回，index=%d want=%v got=%v", i, s, got[i])
		}
	}
}

// 端到端映射：logit 域响应经 mapRerankResults 后只抬升真正相关的文档，
// 且对外暴露的 Score 是概率域（下游 RRF 分数同一量纲）。
func TestMapRerankResults_PromotesRelevantFromLogits(t *testing.T) {
	docs := []RerankDoc{
		{ID: "irrelevant", Content: "量子色动力学与格点规范场论"},
		{ID: "relevant", Content: "在集成中心绑定 WhatsApp Cloud 账号并填写 webhook"},
		{ID: "noise", Content: "今天股市上涨"},
	}
	var rr rerankResponse
	body := `{"results":[{"index":1,"relevance_score":4.512656},{"index":2,"relevance_score":-6.427562}]}`
	if err := json.Unmarshal([]byte(body), &rr); err != nil {
		t.Fatalf("decode: %v", err)
	}

	out := mapRerankResults(docs, &rr)
	if len(out) != 1 {
		t.Fatalf("只有 logit=4.51 应过地板，got=%+v", out)
	}
	if out[0].ID != "relevant" {
		t.Fatalf("首位应为 relevant，got=%s", out[0].ID)
	}
	if out[0].Score < 0 || out[0].Score > 1 {
		t.Fatalf("对外暴露的 Score 必须是概率域，got=%v", out[0].Score)
	}
}
