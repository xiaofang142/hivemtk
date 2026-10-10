package ragcache

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu       sync.Mutex
	exact    map[string]*Entry
	semantic []*Entry
	deleted  []uint64
}

func vecKey(vec []float32) string { return vecToLiteral(vec) }

func (f *fakeStore) GetExact(_ context.Context, kbID, pv string, vec []float32) (*Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.exact[kbID+"|"+pv+"|"+vecKey(vec)]
	if !ok {
		return nil, nil
	}
	cp := *e
	return &cp, nil
}

func (f *fakeStore) GetSemantic(_ context.Context, kbID, pv string, vec []float32, minSim float64) (*Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *Entry
	bestSim := -1.0
	for _, e := range f.semantic {
		if e.KBID != kbID || e.PromptVersion != pv {
			continue
		}
		sim := CosineSimilarity(vec, e.QueryVector)
		if sim >= minSim && sim > bestSim {
			bestSim = sim
			cp := *e
			best = &cp
		}
	}
	return best, nil
}

func (f *fakeStore) Put(_ context.Context, e *Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *e
	f.semantic = append(f.semantic, &cp)
	return nil
}

func (f *fakeStore) Delete(_ context.Context, id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	for i, e := range f.semantic {
		if e.ID == id {
			f.semantic = append(f.semantic[:i], f.semantic[i+1:]...)
			break
		}
	}
	return nil
}

type fakeKBMeta struct {
	updatedAt time.Time
	err       error
}

func (f *fakeKBMeta) GetKBUpdatedAt(context.Context, string) (time.Time, error) {
	return f.updatedAt, f.err
}

var base = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func newTestService(store Store, kb KBMetaReader) *FAQAnswerCacheService {
	svc := NewFAQAnswerCacheService(store, kb, DefaultSemanticThreshold, 0)
	fixed := base
	svc.SetNowFunc(func() time.Time { return fixed })
	return svc
}

func cosPair(cos float64) ([]float32, []float32) {
	a := []float32{1, 0}
	b := []float32{float32(cos), float32(math.Sqrt(1 - cos*cos))}
	return a, b
}

const (
	testKB  = "kb-001"
	testPV  = "v3"
	testAns = "我们的营业时间是周一至周五 9:00-18:00。"
)

func TestLookup_TierExactHit(t *testing.T) {
	vec, _ := cosPair(0.9999)
	store := &fakeStore{exact: map[string]*Entry{
		testKB + "|" + testPV + "|" + vecKey(vec): {ID: 1, KBID: testKB, PromptVersion: testPV, QueryVector: vec, Answer: testAns, KBUpdatedAt: base},
	}}
	kb := &fakeKBMeta{updatedAt: base}
	svc := newTestService(store, kb)

	res, err := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: vec})
	if err != nil {
		t.Fatalf("lookup err: %v", err)
	}
	if res.Tier != TierExact || res.Answer != testAns {
		t.Errorf("want exact hit %q, got tier=%s answer=%q", testAns, res.Tier, res.Answer)
	}
}

func TestLookup_TierSemanticHit(t *testing.T) {
	q, cached := cosPair(0.98)
	store := &fakeStore{semantic: []*Entry{
		{ID: 2, KBID: testKB, PromptVersion: testPV, QueryVector: cached, Answer: testAns, KBUpdatedAt: base},
	}}
	kb := &fakeKBMeta{updatedAt: base}
	svc := newTestService(store, kb)

	res, err := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: q})
	if err != nil {
		t.Fatalf("lookup err: %v", err)
	}
	if res.Tier != TierSemantic {
		t.Fatalf("want semantic hit, got tier=%s", res.Tier)
	}
	if res.Answer != testAns {
		t.Errorf("answer mismatch: %q", res.Answer)
	}
	if math.Abs(res.Similarity-0.98) > 1e-6 {
		t.Errorf("similarity want≈0.98 got=%.6f", res.Similarity)
	}
}

func TestLookup_MissFallsThrough(t *testing.T) {
	q, other := cosPair(0.5)
	store := &fakeStore{semantic: []*Entry{
		{ID: 3, KBID: testKB, PromptVersion: testPV, QueryVector: other, Answer: "无关答案", KBUpdatedAt: base.Add(-time.Hour)},
	}}
	kb := &fakeKBMeta{updatedAt: base}
	svc := newTestService(store, kb)

	res, err := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: q})
	if err != nil {
		t.Fatalf("lookup err: %v", err)
	}
	if res.Tier != TierMiss || res.Answer != "" {
		t.Errorf("want miss, got tier=%s answer=%q", res.Tier, res.Answer)
	}
}

func TestLookup_KBUpdateInvalidatesExactHit(t *testing.T) {
	vec, _ := cosPair(1)
	cachedAt := base.Add(-2 * time.Hour)
	store := &fakeStore{exact: map[string]*Entry{
		testKB + "|" + testPV + "|" + vecKey(vec): {ID: 10, KBID: testKB, PromptVersion: testPV, QueryVector: vec, Answer: testAns, CreatedAt: cachedAt, KBUpdatedAt: cachedAt},
	}}

	kb := &fakeKBMeta{updatedAt: base}
	svc := newTestService(store, kb)

	res, _ := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: vec})
	if res.Tier != TierMiss {
		t.Fatalf("kb updated after cache write: want miss, got tier=%s", res.Tier)
	}
	if len(store.deleted) != 1 || store.deleted[0] != 10 {
		t.Errorf("stale entry should be deleted, deleted=%v", store.deleted)
	}
}

func TestLookup_NoKBUpdateKeepsHit(t *testing.T) {
	vec, _ := cosPair(1)
	cachedAt := base.Add(-2 * time.Hour)
	store := &fakeStore{exact: map[string]*Entry{
		testKB + "|" + testPV + "|" + vecKey(vec): {ID: 11, KBID: testKB, PromptVersion: testPV, QueryVector: vec, Answer: testAns, CreatedAt: cachedAt, KBUpdatedAt: cachedAt},
	}}

	kb := &fakeKBMeta{updatedAt: cachedAt}
	svc := newTestService(store, kb)

	res, _ := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: vec})
	if res.Tier != TierExact {
		t.Errorf("cache should stay valid, got tier=%s", res.Tier)
	}
}

func TestStore_RefusalNotCached(t *testing.T) {
	cases := []struct {
		name   string
		answer string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"refusal_kb_miss", "抱歉，知识不足，暂时无法回答您的问题。"},
		{"refusal_transfer", "该问题需要转人工处理。"},
		{"personalized", "您好 {{name}}，您的订单 {order_id} 已发货。"},
		{"personalized_single", "尊敬的 {customer_name} 您好。"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeStore{}
			svc := newTestService(store, &fakeKBMeta{updatedAt: base})
			fromKB := c.answer != ""
			_ = svc.Store(context.Background(), StoreRequest{
				KBID: testKB, PromptVersion: testPV,
				QueryVector: []float32{1, 0}, Answer: c.answer, FromKnowledgeBase: fromKB,
			})
			if len(store.semantic) != 0 {
				t.Errorf("answer %q must NOT be cached, but store has %d entries", c.answer, len(store.semantic))
			}
		})
	}
}

func TestStore_NonKBSourceNotCached(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(store, &fakeKBMeta{updatedAt: base})
	_ = svc.Store(context.Background(), StoreRequest{
		KBID: testKB, PromptVersion: testPV,
		QueryVector: []float32{1, 0}, Answer: testAns, FromKnowledgeBase: false,
	})
	if len(store.semantic) != 0 {
		t.Errorf("non-KB answer must not be cached")
	}
}

func TestStore_ValidAnswerCached(t *testing.T) {
	store := &fakeStore{}
	kbTime := base.Add(-time.Hour)
	svc := newTestService(store, &fakeKBMeta{updatedAt: kbTime})
	err := svc.Store(context.Background(), StoreRequest{
		KBID: testKB, PromptVersion: testPV,
		QueryVector: []float32{1, 0}, Answer: testAns, FromKnowledgeBase: true,
	})
	if err != nil {
		t.Fatalf("store err: %v", err)
	}
	if len(store.semantic) != 1 {
		t.Fatalf("valid answer should be cached")
	}
	if !store.semantic[0].KBUpdatedAt.Equal(kbTime) {
		t.Errorf("entry.kb_updated_at should record kb meta time, got %v want %v", store.semantic[0].KBUpdatedAt, kbTime)
	}
}

func TestThresholdBoundary_AtAndBelow(t *testing.T) {

	exact, at := cosPair(0.950001)
	if sim := CosineSimilarity(exact, at); sim < 0.95 {
		t.Fatalf("setup error: boundary pair sim=%.8f should be >= 0.95", sim)
	}

	store := &fakeStore{semantic: []*Entry{
		{ID: 20, KBID: testKB, PromptVersion: testPV, QueryVector: at, Answer: "at-threshold", KBUpdatedAt: base},
	}}
	kb := &fakeKBMeta{updatedAt: base}
	svc := newTestService(store, kb)

	res, _ := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: exact})
	if res.Tier == TierMiss {
		t.Errorf("cosine=0.95 exactly should hit (>= threshold), got miss")
	}

	qBelow, cachedBelow := cosPair(0.9499)
	storeBelow := &fakeStore{semantic: []*Entry{
		{ID: 21, KBID: testKB, PromptVersion: testPV, QueryVector: cachedBelow, Answer: "below-threshold", KBUpdatedAt: base},
	}}
	svcBelow := newTestService(storeBelow, kb)
	resBelow, _ := svcBelow.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: qBelow})
	if resBelow.Tier != TierMiss {
		t.Errorf("cosine<0.95 must not hit, got tier=%s", resBelow.Tier)
	}
}

func TestCosineSimilarity_PureCases(t *testing.T) {
	if s := CosineSimilarity([]float32{1, 0}, []float32{1, 0}); math.Abs(s-1) > 1e-9 {
		t.Errorf("identical vectors sim=1, got %f", s)
	}
	if s := CosineSimilarity([]float32{1, 0}, []float32{0, 1}); math.Abs(s) > 1e-9 {
		t.Errorf("orthogonal vectors sim=0, got %f", s)
	}
	if s := CosineSimilarity(nil, []float32{1}); s != 0 {
		t.Errorf("empty vector sim=0, got %f", s)
	}
	if s := CosineSimilarity([]float32{1, 2}, []float32{1, 2, 3}); s != 0 {
		t.Errorf("dim mismatch sim=0, got %f", s)
	}
}

func TestNewFAQAnswerCacheService_ThresholdNeverLoosened(t *testing.T) {
	if s := NewFAQAnswerCacheService(&fakeStore{}, &fakeKBMeta{}, 0.90, 0); s.threshold != DefaultSemanticThreshold {
		t.Errorf("threshold 0.90 must be clamped to 0.95, got %f", s.threshold)
	}
	if s := NewFAQAnswerCacheService(&fakeStore{}, &fakeKBMeta{}, 0.97, 0); s.threshold != 0.97 {
		t.Errorf("tighter threshold 0.97 allowed, got %f", s.threshold)
	}
}

// TestLookup_TTLExpiry cache.faq_ttl 的读取侧语义：超龄条目既不算命中，也当场删掉，
// 而不是留在库里等下一条相同查询再判一遍。ttl=0 是"不按时间过期"那一档，
// 只靠知识库 updated_at 失效（挂载前的唯一口径），所以它必须自成一格。
func TestLookup_TTLExpiry(t *testing.T) {
	vec, _ := cosPair(0.9999)
	key := testKB + "|" + testPV + "|" + vecKey(vec)

	newSvc := func(store Store, ttl time.Duration) *FAQAnswerCacheService {
		svc := NewFAQAnswerCacheService(store, &fakeKBMeta{updatedAt: base}, DefaultSemanticThreshold, ttl)
		fixed := base
		svc.SetNowFunc(func() time.Time { return fixed })
		return svc
	}
	storeWithAge := func(age time.Duration) *fakeStore {
		return &fakeStore{exact: map[string]*Entry{
			key: {ID: 7, KBID: testKB, PromptVersion: testPV, QueryVector: vec, Answer: testAns,
				CreatedAt: base.Add(-age), KBUpdatedAt: base},
		}}
	}
	lookup := func(svc *FAQAnswerCacheService) *LookupResult {
		t.Helper()
		res, err := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: vec})
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		return res
	}

	ttl := 10 * time.Minute

	if res := lookup(newSvc(storeWithAge(time.Minute), ttl)); res.Tier != TierExact {
		t.Errorf("1 分钟前的条目在未超龄（ttl=%s）时应当命中，got tier=%s", ttl, res.Tier)
	}

	store := storeWithAge(11 * time.Minute)
	if res := lookup(newSvc(store, ttl)); res.Tier != TierMiss {
		t.Errorf("11 分钟前的条目在 ttl=%s 下应当按未命中处理，got tier=%s answer=%q", ttl, res.Tier, res.Answer)
	}
	if len(store.deleted) != 1 || store.deleted[0] != 7 {
		t.Errorf("超龄条目应当被物理删除，deleted=%v", store.deleted)
	}

	old := storeWithAge(100 * 24 * time.Hour)
	if res := lookup(newSvc(old, 0)); res.Tier != TierExact {
		t.Errorf("ttl=0 表示不按时间过期，100 天前的条目仍应命中，got tier=%s", res.Tier)
	}
	if len(old.deleted) != 0 {
		t.Errorf("ttl=0 时不该有任何删除动作，deleted=%v", old.deleted)
	}
}

// TestLookup_TierExactCarriesFullSimilarity Tier1 的判据是向量逐位相等，相似度按定义就是 1。
//
// 为什么单独一格：调用方（智能体编排器）拿 Similarity 过置信度阈值，Tier1 交回 0 会让
// "同一条问题问第二遍"这种最该命中的场景一律落到语义层甚至未命中，等于精确层没有消费者。
func TestLookup_TierExactCarriesFullSimilarity(t *testing.T) {
	vec, _ := cosPair(0.9999)
	store := &fakeStore{exact: map[string]*Entry{
		testKB + "|" + testPV + "|" + vecKey(vec): {
			ID: 1, KBID: testKB, PromptVersion: testPV, QueryVector: vec, Answer: testAns,
			CreatedAt: base, KBUpdatedAt: base,
		},
	}}
	svc := newTestService(store, &fakeKBMeta{updatedAt: base})

	res, err := svc.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: vec})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.Tier != TierExact {
		t.Fatalf("期望 Tier1 精确命中，got tier=%s", res.Tier)
	}
	if math.Abs(res.Similarity-1) > 1e-9 {
		t.Errorf("精确命中的相似度应为 1（逐位相等），got %.6f —— 置信度门会把它当最不可信的一条丢掉", res.Similarity)
	}
}
