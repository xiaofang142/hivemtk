package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
)

type fakeFewShotEmbedder struct {
	vecs  map[string][]float32
	calls int
	fail  bool
}

func newFakeFewShotEmbedder() *fakeFewShotEmbedder {
	return &fakeFewShotEmbedder{vecs: map[string][]float32{}}
}

func (f *fakeFewShotEmbedder) Embed(_ context.Context, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		f.calls++
		if f.fail {
			return nil, errEmbedderDown
		}
		if v, ok := f.vecs[t]; ok {
			out = append(out, v)
			continue
		}
		v := make([]float32, 8)
		v[f.calls%8] = 1
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeFewShotEmbedder) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	vs, err := f.Embed(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

func (f *fakeFewShotEmbedder) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Model: "bge-m3", Dimension: 8}
}

var errEmbedderDown = errFakeEmbedder{}

type errFakeEmbedder struct{}

func (errFakeEmbedder) Error() string { return "embedder down" }

func resetFewShotShared(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		fewShotMu.Lock()
		fewShotShared = nil
		fewShotMu.Unlock()
	})
}

// TestCosineSimilarity_Basics 余弦相似度纯函数边界：平行/正交/反向/维度不等/零向量
func TestCosineSimilarity_Basics(t *testing.T) {
	e0 := []float32{1, 0, 0}
	e1 := []float32{0, 1, 0}
	if got := cosineSimilarity(e0, e0); got < 0.9999 {
		t.Errorf("平行向量 cos 应为 1, got %v", got)
	}
	if got := cosineSimilarity(e0, e1); got != 0 {
		t.Errorf("正交向量 cos 应为 0, got %v", got)
	}
	if got := cosineSimilarity(e0, []float32{-1, 0, 0}); got > -0.9999 {
		t.Errorf("反向向量 cos 应为 -1, got %v", got)
	}
	if got := cosineSimilarity(e0, []float32{1, 0}); got != 0 {
		t.Errorf("维度不等 cos 应为 0, got %v", got)
	}
	if got := cosineSimilarity(e0, []float32{0, 0, 0}); got != 0 {
		t.Errorf("零向量 cos 应为 0, got %v", got)
	}
}

// TestTopKByCosine_OrderAndFilter top-k 纯函数：minCos 过滤 + 降序 + k 截断
func TestTopKByCosine_OrderAndFilter(t *testing.T) {
	query := []float32{1, 0, 0}
	cands := []fewShotCandidate{
		{Text: "A", Vec: []float32{0.99, 0.1, 0}},
		{Text: "B", Vec: []float32{0.9, 0.1, 0}},
		{Text: "C", Vec: []float32{0.8, 0.1, 0}},
		{Text: "D", Vec: []float32{0, 1, 0}},
	}
	picked := topKByCosine(query, cands, 2, 0.7)
	if len(picked) != 2 {
		t.Fatalf("应截断为 k=2 条, got %d", len(picked))
	}
	if picked[0].Text != "A" || picked[1].Text != "B" {
		t.Errorf("应按相似度降序取前 2: got [%s, %s]", picked[0].Text, picked[1].Text)
	}
	all := topKByCosine(query, cands, 10, 0.7)
	if len(all) != 3 {
		t.Errorf("低于 minCos 的候选应被过滤, got %d", len(all))
	}
	if got := topKByCosine(nil, cands, 3, 0.7); got != nil {
		t.Errorf("空 query 应返回 nil")
	}
}

// TestFewShotShouldFallbackStatic fallback 判定纯函数：选中不足 k 即回退
func TestFewShotShouldFallbackStatic(t *testing.T) {
	if !shouldFallbackStatic(0, 3) || !shouldFallbackStatic(2, 3) {
		t.Errorf("选中数 < k 应回退静态")
	}
	if shouldFallbackStatic(3, 3) || shouldFallbackStatic(5, 3) {
		t.Errorf("选中数 >= k 不应回退")
	}
}

// TestFewShotStore_SelectExamples_KNN 语义 kNN：登记近邻命中，无关正交基被 minCos 过滤
func TestFewShotStore_SelectExamples_KNN(t *testing.T) {
	fake := newFakeFewShotEmbedder()
	qv := []float32{1, 1, 1, 0, 0, 0, 0, 0}
	hits := []string{"我要买，怎么付款？", "怎么下单？", "我直接拍了啊"}
	near := [][]float32{
		{1, 1, 1, 0, 0, 0, 0, 0},
		{0.98, 1, 1.02, 0, 0, 0, 0, 0},
		{1.01, 0.99, 1, 0, 0, 0, 0, 0},
	}
	for i, txt := range hits {
		fake.vecs[txt] = near[i]
	}
	store := NewFewShotStore(fake)
	got := store.SelectExamples(qv, 3, fewShotMinCos())
	if len(got) != 3 {
		t.Fatalf("应选中 3 条登记近邻（其余示例为正交基被过滤）, got %d: %v", len(got), got)
	}
	hitSet := map[string]bool{}
	for _, txt := range hits {
		hitSet[txt] = true
	}
	for _, g := range got {
		if !hitSet[g] {
			t.Errorf("选中了未登记近邻的示例: %s", g)
		}
	}
}

// TestFewShotStore_AllBelowMinCos_ReturnsNil 全部低于 minCos → 返回 nil 触发静态回退
func TestFewShotStore_AllBelowMinCos_ReturnsNil(t *testing.T) {
	fake := newFakeFewShotEmbedder()

	fake.vecs["我要买，怎么付款？"] = []float32{0, 0, 0, 0, 0, 0, 0, 1}
	store := NewFewShotStore(fake)
	if got := store.SelectExamples([]float32{1, 1, 1, 0, 0, 0, 0, 0}, 3, fewShotMinCos()); got != nil {
		t.Errorf("全部低于 minCos 应返回 nil, got %v", got)
	}
}

// TestFewShotStore_EmbedderFail_ReturnsNil embedder 全失败 → 缓存为空 → nil（上层静态回退）
func TestFewShotStore_EmbedderFail_ReturnsNil(t *testing.T) {
	fake := newFakeFewShotEmbedder()
	fake.fail = true
	store := NewFewShotStore(fake)
	if got := store.SelectExamples([]float32{1, 1, 1, 0, 0, 0, 0, 0}, 3, fewShotMinCos()); got != nil {
		t.Errorf("embedder 失败应返回 nil, got %v", got)
	}
}

// TestFewShotStore_CooldownNoRetryStorm 全量失败后进入冷却，冷却期内不再调用 embedder（防重试风暴）
func TestFewShotStore_CooldownNoRetryStorm(t *testing.T) {
	fake := newFakeFewShotEmbedder()
	fake.fail = true
	store := NewFewShotStore(fake)
	store.cooldown = 60 * time.Millisecond
	ctx := context.Background()
	store.EnsureReady(ctx)
	afterFirst := fake.calls
	if afterFirst == 0 {
		t.Fatal("首次加载应触发向量化调用")
	}
	for i := 0; i < 3; i++ {
		store.EnsureReady(ctx)
	}
	if fake.calls != afterFirst {
		t.Errorf("冷却期内不应重试: first=%d now=%d", afterFirst, fake.calls)
	}
	time.Sleep(80 * time.Millisecond)
	store.EnsureReady(ctx)
	if fake.calls <= afterFirst {
		t.Errorf("冷却到期后应重试: calls=%d", fake.calls)
	}
}

func newInjectedRecognizer(fake *fakeFewShotEmbedder) *IntentRecognizer {
	rec := NewIntentRecognizer(nil, nil, nil)
	rec.embedSvc = fake
	return rec
}

// TestFewShotInject_DynamicPreferred 注入点：动态 kNN 选中 >= k 条时优先生效
func TestFewShotInject_DynamicPreferred(t *testing.T) {
	resetFewShotShared(t)
	fake := newFakeFewShotEmbedder()
	qv := []float32{1, 1, 1, 0, 0, 0, 0, 0}
	hits := []string{"怎么下单？", "我要买，怎么付款？", "我直接拍了啊"}
	near := [][]float32{
		{1, 1, 1, 0, 0, 0, 0, 0},
		{0.98, 1, 1.02, 0, 0, 0, 0, 0},
		{1.01, 0.99, 1, 0, 0, 0, 0, 0},
	}
	for i, txt := range hits {
		fake.vecs[txt] = near[i]
	}
	fake.vecs["怎么下单购买"] = qv
	fewShotMu.Lock()
	fewShotShared = NewFewShotStore(fake)
	fewShotMu.Unlock()

	rec := newInjectedRecognizer(fake)
	r, err := rec.Recognize(context.Background(), "s-1", "", "怎么下单购买")
	if err != nil {
		t.Fatal(err)
	}
	if r.IntentType != IntentPurchase {
		t.Fatalf("规则层应命中 purchase, got %s", r.IntentType)
	}
	if len(r.TopKExamples) != 3 {
		t.Fatalf("动态注入应填充 3 条, got %d: %v", len(r.TopKExamples), r.TopKExamples)
	}
	hitSet := map[string]bool{}
	for _, txt := range hits {
		hitSet[txt] = true
	}
	for _, g := range r.TopKExamples {
		if !hitSet[g] {
			t.Errorf("注入了非动态选中示例: %s", g)
		}
	}
}

// TestFewShotInject_FallbackStatic_OnEmbedderError embedder 故障 → 静默回退静态示例（与 fillTopKExamples 结果一致）
func TestFewShotInject_FallbackStatic_OnEmbedderError(t *testing.T) {
	resetFewShotShared(t)
	fake := newFakeFewShotEmbedder()
	fake.fail = true
	fewShotMu.Lock()
	fewShotShared = NewFewShotStore(fake)
	fewShotMu.Unlock()

	rec := newInjectedRecognizer(fake)
	r, err := rec.Recognize(context.Background(), "s-1", "", "怎么下单购买")
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.RecognizeResult{IntentType: r.IntentType}
	fillTopKExamples(want)
	if len(r.TopKExamples) != len(want.TopKExamples) {
		t.Fatalf("应回退静态示例 %v, got %v", want.TopKExamples, r.TopKExamples)
	}
	for i := range want.TopKExamples {
		if r.TopKExamples[i] != want.TopKExamples[i] {
			t.Errorf("静态回退不一致 idx=%d: want %s got %s", i, want.TopKExamples[i], r.TopKExamples[i])
		}
	}
}

// TestFewShotInject_FallbackStatic_AllBelowMinCos 全部低于阈值 → 回退静态
func TestFewShotInject_FallbackStatic_AllBelowMinCos(t *testing.T) {
	resetFewShotShared(t)
	fake := newFakeFewShotEmbedder()

	fake.vecs["怎么下单购买"] = []float32{1, 0, 0, 0, 0, 0, 0, 0}
	fake.vecs["我要买，怎么付款？"] = []float32{0, 1, 0, 0, 0, 0, 0, 0}
	fake.vecs["怎么下单？"] = []float32{0, 0, 1, 0, 0, 0, 0, 0}
	fake.vecs["我直接拍了啊"] = []float32{0, 0, 0, 1, 0, 0, 0, 0}
	fewShotMu.Lock()
	fewShotShared = NewFewShotStore(fake)
	fewShotMu.Unlock()

	rec := newInjectedRecognizer(fake)
	r, err := rec.Recognize(context.Background(), "s-1", "", "怎么下单购买")
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.RecognizeResult{IntentType: r.IntentType}
	fillTopKExamples(want)
	if len(r.TopKExamples) != len(want.TopKExamples) {
		t.Fatalf("应回退静态示例 %v, got %v", want.TopKExamples, r.TopKExamples)
	}
}

// TestFewShotInject_NoEmbedder_Static embedder 未注入 → 直接静态路径（零行为变化）
func TestFewShotInject_NoEmbedder_Static(t *testing.T) {
	rec := NewIntentRecognizer(nil, nil, nil)
	r, err := rec.Recognize(context.Background(), "s-1", "", "怎么下单购买")
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.RecognizeResult{IntentType: r.IntentType}
	fillTopKExamples(want)
	if len(r.TopKExamples) != len(want.TopKExamples) {
		t.Fatalf("无 embedder 应走静态 %v, got %v", want.TopKExamples, r.TopKExamples)
	}
}
