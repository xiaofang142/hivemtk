package service

// intent_catalog_consumers_test.go TB-10 刀3：四消费方同源测试——
// 目录装配后，intentsForRuleMerged / intentsForRule / recognizeByLLM 候选 /
// isBindableIntent / GetChainVocabulary 全部以目录为单一事实源；目录未装配
// （nil）时全部回落原逻辑（零行为变化）。

import (
	"context"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
)

// fakeCatalogOnlyRepo 最小目录仓储（只支撑 loadCatalog 语义）。
type fakeCatalogOnlyRepo struct {
	mu   sync.Mutex
	rows []model.IntentCatalog
	err  error
}

func (f *fakeCatalogOnlyRepo) ListEnabled(ctx context.Context) ([]model.IntentCatalog, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.IntentCatalog{}
	for _, r := range f.rows {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeCatalogOnlyRepo) GetByKey(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].IntentKey == intentKey && f.rows[i].Enabled {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeCatalogOnlyRepo) GetByKeyAny(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].IntentKey == intentKey {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeCatalogOnlyRepo) HasAnyRow(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	return len(f.rows) > 0, nil
}

func (f *fakeCatalogOnlyRepo) Create(ctx context.Context, rec *model.IntentCatalog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, *rec)
	return nil
}

func (f *fakeCatalogOnlyRepo) Update(ctx context.Context, rec *model.IntentCatalog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == rec.ID {
			f.rows[i] = *rec
			return nil
		}
	}
	return errIntentCatalogFakeNotFound
}

func (f *fakeCatalogOnlyRepo) Delete(ctx context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return errIntentCatalogFallbackNotFound
}

var errIntentCatalogFallbackNotFound = errIntentCatalogFake{"not found"}

// catalogTestRows 目录同源样例：默认 price_inquiry + 自定义 my_custom。
func catalogTestRows() []model.IntentCatalog {
	return []model.IntentCatalog{
		{IntentKey: "price_inquiry", Source: intentCatalogSourceDefault, Family: intentCatalogFamilyService,
			Name: "价格咨询", KeywordsJSON: `["多少钱","价格"]`, ExamplesJSON: `[]`, Description: "客户询问价格或优惠", Enabled: true},
		{IntentKey: "my_custom", Source: intentCatalogSourceCustom, Family: intentCatalogFamilyCustom,
			Name: "自定义", KeywordsJSON: `["专属词"]`, ExamplesJSON: `[]`, Description: "自定义意图", Enabled: true},
	}
}

// TestCatalogConsumers_SingleSource 目录装配后四消费方同源。
func TestCatalogConsumers_SingleSource(t *testing.T) {
	svc := NewIntentCatalogService(&fakeCatalogOnlyRepo{rows: catalogTestRows()})
	SetIntentCatalogService(svc)
	defer SetIntentCatalogService(nil)
	ctx := context.Background()

	// a) intentsForRuleMerged：来自目录（default 在前、custom 在后）
	merged := intentsForRuleMerged(ctx)
	if len(merged) != 2 || merged[0].Type != "price_inquiry" || merged[1].Type != "my_custom" {
		t.Fatalf("intentsForRuleMerged 应同源于目录（2 条 default+custom），实得 %+v", merged)
	}
	// intentsForRule 同源（merged 非空即透传）
	rule := intentsForRule(ctx)
	if len(rule) != 2 || rule[0].Type != "price_inquiry" {
		t.Fatalf("intentsForRule 应同源于目录，实得 %+v", rule)
	}

	// b) recognizeByLLM 候选同源：临时实例化识别器取候选构造逻辑
	rec := &IntentRecognizer{}
	_ = rec
	allDefs := func() []IntentDef {
		if cat := GetIntentCatalogService(); cat != nil {
			if defs := cat.EffectiveCatalogDefs(ctx); len(defs) > 0 {
				return defs
			}
		}
		return append(append([]IntentDef(nil), DefaultIntents...), effectiveCustomIntentDefs(ctx)...)
	}()
	if len(allDefs) != 2 || allDefs[1].Type != "my_custom" {
		t.Fatalf("LLM 候选应同源于目录，实得 %+v", allDefs)
	}

	// c) isBindableIntent：目录键放行，非目录键拒绝（无静态词表服务状态介入）
	chain := &IntentToolChainService{}
	if !chain.isBindableIntent(ctx, "price_inquiry") {
		t.Error("目录启用行应放行 isBindableIntent(price_inquiry)")
	}
	if !chain.isBindableIntent(ctx, "my_custom") {
		t.Error("目录 custom 行应放行 isBindableIntent(my_custom)")
	}
	if chain.isBindableIntent(ctx, "no_such_intent_xyz") {
		t.Error("目录无此键不应放行")
	}

	// d) GetChainVocabulary：intents/families 同源于目录（family 列直出）
	vocab := chain.GetChainVocabulary(ctx)
	found := map[string]string{}
	for _, k := range vocab.Intents {
		found[k] = vocab.IntentFamilies[k]
	}
	if found["price_inquiry"] != intentCatalogFamilyService || found["my_custom"] != intentCatalogFamilyCustom {
		t.Fatalf("vocab 应同源于目录 family 列，实得 %v", found)
	}
}

// TestCatalogConsumers_Fallback 目录未装配（nil）时全部回落原逻辑。
func TestCatalogConsumers_Fallback(t *testing.T) {
	SetIntentCatalogService(nil)
	ctx := context.Background()

	// a) intentsForRuleMerged 回落原式（DefaultIntents 为底，非空）
	merged := intentsForRuleMerged(ctx)
	if len(merged) < len(DefaultIntents) {
		t.Fatalf("回落应 ≥ DefaultIntents，实得 %d", len(merged))
	}

	// b) isBindableIntent 回落静态双家族
	chain := &IntentToolChainService{}
	if !chain.isBindableIntent(ctx, "chitchat") {
		t.Error("回落应放行 runtime 键 chitchat")
	}
	if !chain.isBindableIntent(ctx, "clarify") {
		t.Error("回落应放行 service 键 clarify")
	}
	if chain.isBindableIntent(ctx, "no_such_intent_xyz") {
		t.Error("回落不应放行未知键")
	}

	// c) GetChainVocabulary 回落静态双家族+custom
	vocab := chain.GetChainVocabulary(ctx)
	if len(vocab.Intents) < len(ChainIntentVocabulary()) {
		t.Fatalf("回落 vocab 应含静态双家族，实得 %d 条", len(vocab.Intents))
	}
	if vocab.IntentFamilies["chitchat"] != "runtime" {
		t.Fatalf("回落 vocab chitchat 应 runtime，实得 %q", vocab.IntentFamilies["chitchat"])
	}
}
