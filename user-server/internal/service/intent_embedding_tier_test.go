package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"

	"gorm.io/gorm"
)

func newIntentRecognizerNoDB(t *testing.T) (*IntentRecognizer, *gorm.DB) {
	t.Helper()
	return NewIntentRecognizer(nil, nil, nil), nil
}

func awaitAnchors(rec *IntentRecognizer) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			rec.anchorMu.RLock()
			done := len(rec.anchorVecs) > 0 || rec.embDisabled
			rec.anchorMu.RUnlock()
			if done {
				close(ch)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(ch)
	}()
	return ch
}

type stubEmbedder struct {
	vecs  map[string][]float32
	failN int
	calls int
	dim   int
}

func (s *stubEmbedder) Embed(ctx context.Context, cfg *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		v, err := s.EmbedOne(ctx, cfg, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *stubEmbedder) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	s.calls++
	if s.calls <= s.failN {
		return nil, context.DeadlineExceeded
	}
	if v, ok := s.vecs[text]; ok {
		return v, nil
	}
	return make([]float32, s.dim), nil
}

func (s *stubEmbedder) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Dimension: s.dim}
}

func unitVec(dim int, hotIdx []int) []float32 {
	v := make([]float32, dim)
	for _, i := range hotIdx {
		if i < dim {
			v[i] = 1
		}
	}
	return v
}

func newAnchorStub(t *testing.T) (*IntentRecognizer, *stubEmbedder) {
	t.Helper()
	rec, _ := newIntentRecognizerNoDB(t)
	stub := &stubEmbedder{
		dim: 8,
		vecs: map[string][]float32{
			"这个多少钱？":   unitVec(8, []int{0, 1}),
			"你们价格怎么样？": unitVec(8, []int{0, 1}),
			"能不能便宜点？":  unitVec(8, []int{0, 1}),
			"这个东西太贵了":  unitVec(8, []int{2, 3}),
			"价格有点高啊":   unitVec(8, []int{2, 3}),
			"比别家贵好多":   unitVec(8, []int{2, 3}),
		},
	}
	rec.SetEmbeddingService(stub)
	<-awaitAnchors(rec)
	rec.anchorMu.RLock()
	ready := len(rec.anchorVecs) > 0 && !rec.embDisabled
	rec.anchorMu.RUnlock()
	if !ready {
		t.Fatalf("锚点预计算失败，测试前置条件不满足")
	}
	return rec, stub
}

func TestRecognizeEmbedding_HighConfidenceHit(t *testing.T) {
	rec, _ := newAnchorStub(t)
	r := rec.recognizeByEmbedding(context.Background(), "这个多少钱？")
	if r == nil {
		t.Fatalf("期望 embedding 命中，实际 nil")
	}
	if r.Method != "embedding" {
		t.Fatalf("method 期望 embedding，实际 %s", r.Method)
	}
	if r.IntentType != IntentPriceInquiry {
		t.Fatalf("intent 期望 price_inquiry，实际 %s", r.IntentType)
	}
	if r.Confidence < intentEmbeddingTop1 {
		t.Fatalf("置信度应≥阈值: %.3f", r.Confidence)
	}
}

func TestRecognizeEmbedding_AmbiguityGapFallsThrough(t *testing.T) {
	rec, _ := newIntentRecognizerNoDB(t)

	v15 := []float32{0.966, 0.259, 0, 0, 0, 0, 0, 0}
	v19 := []float32{0.9455, 0.3256, 0, 0, 0, 0, 0, 0}
	stub := &stubEmbedder{
		dim: 8,
		vecs: map[string][]float32{
			"这个多少钱？": v15, "你们价格怎么样？": v15, "能不能便宜点？": v15,
			"这个东西太贵了": v19, "价格有点高啊": v19, "比别家贵好多": v19,
			"混合消息": unitVec(8, []int{0}),
		},
	}
	rec.SetEmbeddingService(stub)
	<-awaitAnchors(rec)

	r := rec.recognizeByEmbedding(context.Background(), "混合消息")
	if r == nil {
		t.Fatalf("歧义缺口应返回 clarify 意图，实际 nil")
	}
	if r.IntentType != IntentClarify {
		t.Fatalf("intent 期望 clarify，实际 %s", r.IntentType)
	}
	if r.Entities["top1_intent"] != IntentPriceInquiry || r.Entities["top2_intent"] != IntentObjectionPrice {
		t.Fatalf("clarify 候选期望 price_inquiry/objection_price，实际 %v", r.Entities)
	}
}

func TestRecognizeEmbedding_LowSimilarityReturnsNil(t *testing.T) {
	rec, stub := newAnchorStub(t)
	stub.vecs["无关闲聊"] = unitVec(8, []int{7})
	if r := rec.recognizeByEmbedding(context.Background(), "无关闲聊"); r != nil {
		t.Fatalf("低相似度应返回 nil，实际 %s", r.IntentType)
	}
}

func TestRecognizeEmbedding_CircuitBreakerAfterConsecutiveFailures(t *testing.T) {
	rec, _ := newIntentRecognizerNoDB(t)
	stub := &stubEmbedder{dim: 4, failN: 1000}
	rec.SetEmbeddingService(stub)
	<-awaitAnchors(rec)
	rec.anchorMu.RLock()
	disabled := rec.embDisabled
	rec.anchorMu.RUnlock()
	if !disabled {
		t.Fatalf("锚点全失败应触发熔断")
	}
	callsBefore := stub.calls
	if r := rec.recognizeByEmbedding(context.Background(), "任意文本"); r != nil || stub.calls != callsBefore {
		t.Fatalf("熔断后不应再发起 Embedding 调用")
	}
}

func TestRecognizeEmbedding_FailOpenThenRecover(t *testing.T) {
	rec, _ := newIntentRecognizerNoDB(t)
	stub := &stubEmbedder{
		dim: 8,

		failN: 2,
		vecs: map[string][]float32{
			"这个多少钱？":   unitVec(8, []int{0, 1}),
			"你们价格怎么样？": unitVec(8, []int{0, 1}),
			"能不能便宜点？":  unitVec(8, []int{0, 1}),
		},
	}
	rec.SetEmbeddingService(stub)
	<-awaitAnchors(rec)
	r := rec.recognizeByEmbedding(context.Background(), "这个多少钱？")
	if r == nil || r.IntentType != IntentPriceInquiry {
		t.Fatalf("瞬时失败后应恢复命中，实际 %+v", r)
	}
}

func TestLLMContract_LowConfidenceDowngradedToUnknown(t *testing.T) {

	parsed := struct {
		IntentType string  `json:"intent_type"`
		Confidence float64 `json:"confidence"`
	}{IntentType: IntentPurchase, Confidence: 0.55}
	intentName := parsed.IntentType
	known := false
	for _, def := range DefaultIntents {
		if def.Type == parsed.IntentType {
			known = true
			break
		}
	}
	if !known {
		parsed.IntentType = "unknown"
		intentName = "未知意图"
	}
	if parsed.IntentType != IntentUnknown && parsed.Confidence < 0.7 {
		parsed.IntentType = IntentUnknown
		intentName = "未知意图"
	}
	if parsed.IntentType != IntentUnknown || intentName != "未知意图" {
		t.Fatalf("低置信强选必须降级 unknown")
	}
}

// F1 懒重试：冷却到期后锚点为空应触发重新预计算（TEI 启动慢场景恢复）
func TestRecognizeEmbedding_LazyRetryAfterCooldown(t *testing.T) {
	rec, _ := newIntentRecognizerNoDB(t)
	stub := &stubEmbedder{
		dim: 8,
		vecs: map[string][]float32{
			"这个多少钱？":   unitVec(8, []int{0, 1}),
			"你们价格怎么样？": unitVec(8, []int{0, 1}),
			"能不能便宜点？":  unitVec(8, []int{0, 1}),
		},
	}

	stub.failN = 1000
	rec.SetEmbeddingService(stub)
	<-awaitAnchors(rec)
	callsAfterFirstPrecompute := stub.calls

	if r := rec.recognizeByEmbedding(context.Background(), "任意"); r != nil || stub.calls != callsAfterFirstPrecompute {
		t.Fatalf("冷却期内不应重试")
	}

	rec.anchorMu.Lock()
	rec.embLastPrecompute = time.Now().Add(-embRetryCooldown() - time.Second)
	rec.embFailCount = 0
	rec.anchorMu.Unlock()
	stub.failN = 0

	if r := rec.recognizeByEmbedding(context.Background(), "这个多少钱？"); r != nil {
		t.Fatalf("后台重试模式下本请求应返回 nil，实际 %+v", r)
	}

	waitOk := false
	for i := 0; i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
		rec.anchorMu.RLock()
		ready := len(rec.anchorVecs) > 0 && !rec.embDisabled
		rec.anchorMu.RUnlock()
		if ready {
			waitOk = true
			break
		}
	}
	if !waitOk {
		t.Fatalf("后台重试未完成锚点预计算")
	}
	r := rec.recognizeByEmbedding(context.Background(), "这个多少钱？")
	if r == nil || r.IntentType != IntentPriceInquiry {
		t.Fatalf("冷却到期懒重试后应恢复命中，实际 %+v", r)
	}
}
