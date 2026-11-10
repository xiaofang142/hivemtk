package service

// intent_catalog_test.go TB-10 刀2：统一意图目录视图层纯逻辑用例（不连库）。
import (
	"context"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// fakeIntentCatalogRepo 内存目录仓储（刀2 用例只判视图层语义）。
type fakeIntentCatalogRepo struct {
	mu      sync.Mutex
	rows    []model.IntentCatalog
	nextID  uint
	listErr error
}

func (f *fakeIntentCatalogRepo) ListEnabled(ctx context.Context) ([]model.IntentCatalog, error) {
	if f.listErr != nil {
		return nil, f.listErr
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

func (f *fakeIntentCatalogRepo) GetByKey(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].IntentKey == intentKey && f.rows[i].Enabled {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeIntentCatalogRepo) GetByKeyAny(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].IntentKey == intentKey {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeIntentCatalogRepo) HasAnyRow(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows) > 0, nil
}

func (f *fakeIntentCatalogRepo) Create(ctx context.Context, rec *model.IntentCatalog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	rec.ID = f.nextID
	f.rows = append(f.rows, *rec)
	return nil
}

func (f *fakeIntentCatalogRepo) Update(ctx context.Context, rec *model.IntentCatalog) error {
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

func (f *fakeIntentCatalogRepo) Delete(ctx context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return errIntentCatalogFakeNotFound
}

var errIntentCatalogFakeNotFound = errIntentCatalogFake{"not found"}

type errIntentCatalogFake struct{ msg string }

func (e errIntentCatalogFake) Error() string { return e.msg }

func TestIntentFamilyOf_CompatView(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"chitchat", intentCatalogFamilyRuntime},
		{"handoff_human", intentCatalogFamilyRuntime},
		{"price_inquiry", intentCatalogFamilyService},
		{"clarify", intentCatalogFamilyService},
		{"greeting", intentCatalogFamilyService},  // 重叠键归 service
		{"complaint", intentCatalogFamilyService}, // 重叠键归 service
		{"unknown", intentCatalogFamilyService},   // 重叠键归 service
		{"my_custom_intent", intentCatalogFamilyCustom},
	}
	for _, c := range cases {
		if got := IntentFamilyOf(c.key); got != c.want {
			t.Errorf("IntentFamilyOf(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestEffectiveCatalogDefs_FallbackUnionAndOverride(t *testing.T) {
	// a) 未装配 repo → 回落 DefaultIntents（零行为变化）
	svcNil := NewIntentCatalogService(nil)
	if defs := svcNil.EffectiveCatalogDefs(context.Background()); len(defs) != len(DefaultIntents) {
		t.Fatalf("repo 未装配应回落 DefaultIntents（%d 条），实得 %d", len(DefaultIntents), len(defs))
	}

	// b) 读失败 → 回落 DefaultIntents
	svcErr := NewIntentCatalogService(&fakeIntentCatalogRepo{listErr: errIntentCatalogFakeNotFound})
	if defs := svcErr.EffectiveCatalogDefs(context.Background()); len(defs) != len(DefaultIntents) {
		t.Fatalf("读失败应回落 DefaultIntents，实得 %d 条", len(defs))
	}

	// c) 有行但全禁用 → 空词表（尊重管理端禁用动作）
	svcAllOff := NewIntentCatalogService(&fakeIntentCatalogRepo{rows: []model.IntentCatalog{
		{IntentKey: "price_inquiry", Source: intentCatalogSourceDefault, Enabled: false},
	}})
	if defs := svcAllOff.EffectiveCatalogDefs(context.Background()); len(defs) != 0 {
		t.Fatalf("全禁用应返空词表，实得 %d 条", len(defs))
	}

	// d) 视图层 union：default 行在前 + override KV 先合 + custom 行追加在后
	fake := &fakeIntentCatalogRepo{rows: []model.IntentCatalog{
		{IntentKey: "price_inquiry", Source: intentCatalogSourceDefault, Name: "价格咨询",
			KeywordsJSON: `["多少钱","价格"]`, ExamplesJSON: `["这个多少钱？"]`, Description: "客户询问价格或优惠", Enabled: true},
		{IntentKey: "my_custom", Source: intentCatalogSourceCustom, Name: "自定义",
			KeywordsJSON: `["专属词"]`, Description: "自定义意图", Enabled: true},
		{IntentKey: "purchase", Source: intentCatalogSourceDefault, Name: "购买意向",
			KeywordsJSON: `["怎么买"]`, Description: "客户明确购买意向", Enabled: true},
	}}
	svc := NewIntentCatalogService(fake)

	intentOverrideMu.Lock()
	intentKeywordOverride = map[string][]string{"price_inquiry": {"新词", "价格"}}
	intentOverrideMu.Unlock()
	defer func() {
		intentOverrideMu.Lock()
		intentKeywordOverride = nil
		intentOverrideMu.Unlock()
	}()

	defs := svc.EffectiveCatalogDefs(context.Background())
	if len(defs) != 3 {
		t.Fatalf("视图层应 3 条（default 2 + custom 1），实得 %d", len(defs))
	}
	if defs[0].Type != "price_inquiry" || defs[1].Type != "purchase" || defs[2].Type != "my_custom" {
		t.Fatalf("顺序应为 default 在前 custom 在后，实得 %v", []string{defs[0].Type, defs[1].Type, defs[2].Type})
	}
	if len(defs[0].Keywords) != 3 || defs[0].Keywords[2] != "新词" {
		t.Fatalf("override 合并错（去重+追加在后）：%v", defs[0].Keywords)
	}
}

func TestIsCatalogIntent_PlacingAndFallback(t *testing.T) {
	fake := &fakeIntentCatalogRepo{rows: []model.IntentCatalog{
		{IntentKey: "price_inquiry", Source: intentCatalogSourceDefault, Enabled: true},
		{IntentKey: "disabled_one", Source: intentCatalogSourceCustom, Enabled: false},
	}}
	svc := NewIntentCatalogService(fake)
	ctx := context.Background()
	if !svc.IsCatalogIntent(ctx, "price_inquiry") {
		t.Error("目录启用行应放行 price_inquiry")
	}
	if svc.IsCatalogIntent(ctx, "disabled_one") {
		t.Error("目录禁用行不应放行 disabled_one")
	}
	if svc.IsCatalogIntent(ctx, "no_such_key") {
		t.Error("目录无此键不应放行")
	}
	if svc.IsCatalogIntent(ctx, "") {
		t.Error("空键不应放行")
	}

	// 未播种（读失败）回落既有双词表校验
	svcFallback := NewIntentCatalogService(&fakeIntentCatalogRepo{listErr: errIntentCatalogFakeNotFound})
	if !svcFallback.IsCatalogIntent(ctx, "chitchat") {
		t.Error("回落路径应放行 runtime 词表键 chitchat")
	}
	if !svcFallback.IsCatalogIntent(ctx, "clarify") {
		t.Error("回落路径应放行 service 词表键 clarify")
	}
	if svcFallback.IsCatalogIntent(ctx, "no_such_key") {
		t.Error("回落路径不应放行未知键")
	}
}

func TestSeedBuiltinDefaults_Idempotent(t *testing.T) {
	fake := &fakeIntentCatalogRepo{}
	svc := NewIntentCatalogService(fake)
	ctx := context.Background()

	n, err := svc.SeedBuiltinDefaults(ctx)
	if err != nil {
		t.Fatalf("种子写入失败: %v", err)
	}
	// 期望键数 = service 家族 ∪ runtime 词表（重叠键去重）
	want := len(validChainServiceIntents)
	for k := range validChainIntents {
		if !validChainServiceIntents[k] {
			want++
		}
	}
	if n != want || len(fake.rows) != want {
		t.Fatalf("种子应写 %d 行，实写 %d 行（fake 持有 %d）", want, n, len(fake.rows))
	}
	for key := range validChainServiceIntents {
		if _, err := fake.GetByKeyAny(ctx, key); err != nil || !catalogFakeHasKey(fake, key) {
			t.Fatalf("service 家族键 %s 未入目录", key)
		}
	}
	for key := range validChainIntents {
		if !validChainServiceIntents[key] && !catalogFakeHasKey(fake, key) {
			t.Fatalf("runtime 独有键 %s 未入目录", key)
		}
	}
	// family 映射：重叠键归 service，runtime 独有归 runtime
	for _, r := range fake.rows {
		switch r.IntentKey {
		case "greeting", "complaint", "unknown":
			if r.Family != intentCatalogFamilyService {
				t.Errorf("重叠键 %s 应归 service，实得 %s", r.IntentKey, r.Family)
			}
		case "chitchat", "handoff_human", "faq":
			if r.Family != intentCatalogFamilyRuntime {
				t.Errorf("runtime 独有键 %s 应归 runtime，实得 %s", r.IntentKey, r.Family)
			}
		}
		if r.Source != intentCatalogSourceDefault || !r.Enabled || r.TenantID != tenant.DefaultTenantID {
			t.Errorf("种子行形态错: %+v", r)
		}
	}

	// 幂等：二次种子零写入
	n2, err := svc.SeedBuiltinDefaults(ctx)
	if err != nil || n2 != 0 || len(fake.rows) != want {
		t.Fatalf("二次种子应幂等（n=0，行数不变），实得 n=%d rows=%d err=%v", n2, len(fake.rows), err)
	}
	// 禁用占位行也跳过重建
	for i := range fake.rows {
		if fake.rows[i].IntentKey == "social" {
			fake.rows[i].Enabled = false
			break
		}
	}
	n3, err := svc.SeedBuiltinDefaults(ctx)
	if err != nil || n3 != 0 || len(fake.rows) != want {
		t.Fatalf("禁用占位行应跳过重建（n=0），实得 n=%d rows=%d err=%v", n3, len(fake.rows), err)
	}
}

func catalogFakeHasKey(f *fakeIntentCatalogRepo, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.IntentKey == key {
			return true
		}
	}
	return false
}
