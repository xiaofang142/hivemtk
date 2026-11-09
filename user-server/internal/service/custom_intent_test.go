package service

import (
	"context"
	"sync"
	"testing"

	"gorm.io/gorm"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------- fake 仓储（按 ctx 租户分桶） ----------

type fakeCustomIntentRepo struct {
	mu   sync.Mutex
	rows map[uint][]*model.CustomIntent
	seq  uint
}

func newFakeCustomIntentRepo() *fakeCustomIntentRepo {
	return &fakeCustomIntentRepo{rows: map[uint][]*model.CustomIntent{}}
}

func (f *fakeCustomIntentRepo) tenantOf(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (f *fakeCustomIntentRepo) Create(ctx context.Context, rec *model.CustomIntent) error {
	if rec.TenantID == 0 {
		rec.TenantID = f.tenantOf(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	rec.ID = f.seq
	cp := *rec
	f.rows[rec.TenantID] = append(f.rows[rec.TenantID], &cp)
	return nil
}

func (f *fakeCustomIntentRepo) Update(ctx context.Context, rec *model.CustomIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ID == rec.ID {
			r.Name, r.KeywordsJSON, r.ExamplesJSON, r.Description, r.Enabled =
				rec.Name, rec.KeywordsJSON, rec.ExamplesJSON, rec.Description, rec.Enabled
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeCustomIntentRepo) Delete(ctx context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.rows[f.tenantOf(ctx)]
	for i, r := range rows {
		if r.ID == id {
			f.rows[f.tenantOf(ctx)] = append(rows[:i], rows[i+1:]...)
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeCustomIntentRepo) GetByID(ctx context.Context, id uint) (*model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ID == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeCustomIntentRepo) GetByType(ctx context.Context, intentType string) (*model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.Type == intentType {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeCustomIntentRepo) ListEnabled(ctx context.Context) ([]model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.CustomIntent{}
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.Enabled {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeCustomIntentRepo) List(ctx context.Context, page, pageSize int) ([]model.CustomIntent, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.CustomIntent{}
	for _, r := range f.rows[f.tenantOf(ctx)] {
		out = append(out, *r)
	}
	return out, int64(len(out)), nil
}

func customIntentInput(t, name string, keywords []string) CustomIntentUpsertInput {
	enabled := true
	return CustomIntentUpsertInput{Type: t, Name: name, Keywords: keywords, Examples: []string{}, Description: "测试词条", Enabled: &enabled}
}

// ---------- 写侧校验矩阵 ----------

func TestCustomIntentValidation(t *testing.T) {
	svc := NewCustomIntentService(newFakeCustomIntentRepo())
	ctx := tenant.ContextWithDefault(context.Background())

	cases := []struct {
		name string
		in   CustomIntentUpsertInput
		code int
	}{
		{"type 形态非法", customIntentInput("Bad-Type", "x", []string{"词"}), bizerr.CodeParamInvalid},
		{"name 为空", customIntentInput("return_policy", " ", []string{"词"}), bizerr.CodeParamInvalid},
		{"keywords 全空", customIntentInput("return_policy", "x", []string{"  ", ""}), bizerr.CodeParamInvalid},
		{"与内置词典冲突", customIntentInput("after_sale", "x", []string{"词"}), bizerr.CodeConflict},
	}
	for _, tc := range cases {
		_, err := svc.Create(ctx, tc.in)
		if be, ok := err.(*bizerr.BizError); !ok || be.Code != tc.code {
			t.Fatalf("%s: 应 %d, got %v", tc.name, tc.code, err)
		}
	}
}

func TestCustomIntent_DuplicateTypeConflict(t *testing.T) {
	svc := NewCustomIntentService(newFakeCustomIntentRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	if _, err := svc.Create(ctx, customIntentInput("return_policy", "退换政策", []string{"怎么退"})); err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}
	_, err := svc.Create(ctx, customIntentInput("return_policy", "另一个", []string{"词2"}))
	if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeConflict {
		t.Fatalf("重复 type 应 4009, got %v", err)
	}
	// 禁用态仍占位（不可重建）
	if _, err := svc.Update(ctx, 1, customIntentInput("return_policy", "退换政策", []string{"怎么退"})); err != nil {
		t.Fatalf("update: %v", err)
	}
	disabled := false
	if _, err := svc.Update(ctx, 1, CustomIntentUpsertInput{Type: "return_policy", Name: "退换政策", Keywords: []string{"怎么退"}, Enabled: &disabled}); err != nil {
		t.Fatalf("禁用 update: %v", err)
	}
	if _, err := svc.Create(ctx, customIntentInput("return_policy", "重建", []string{"词"})); err == nil {
		t.Fatal("禁用态占位：重建应被拒")
	}
}

func TestCustomIntent_UpdateTypeImmutable(t *testing.T) {
	svc := NewCustomIntentService(newFakeCustomIntentRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	if _, err := svc.Create(ctx, customIntentInput("return_policy", "退换", []string{"怎么退"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := svc.Update(ctx, 1, customIntentInput("other_type", "改名", []string{"词"}))
	if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeParamInvalid {
		t.Fatalf("改 type 应拒绝, got %v", err)
	}
}

// ---------- 合并 E2E：词面命中自定义意图（真 PG + 真识别器规则层） ----------

func TestCustomIntent_RecognizerRuleMerge_PG(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CustomIntent{}, &model.IntentToolChain{})
	svc := NewCustomIntentService(repository.NewCustomIntentRepositoryWithDB(db))
	ctx := tenant.ContextWithDefault(context.Background())

	// 单例注入（识别器经包级 accessor 消费）
	SetCustomIntentService(svc)
	t.Cleanup(func() { SetCustomIntentService(nil) })

	if _, err := svc.Create(ctx, customIntentInput("return_policy", "退换货政策", []string{"怎么退", "如何退货"})); err != nil {
		t.Fatalf("create: %v", err)
	}

	rec := NewIntentRecognizer(nil, nil)
	res := rec.recognizeByRule(ctx, "我想退货怎么退？")
	if res == nil {
		t.Fatal("词面「怎么退」应命中自定义意图")
	}
	if res.IntentType != "return_policy" || res.IntentName != "退换货政策" || res.Method != "rule" {
		t.Fatalf("命中形态不符: %+v", res)
	}
	// 未知词面不受影响
	if r2 := rec.recognizeByRule(ctx, "这个多少钱"); r2 != nil && r2.IntentType == "return_policy" {
		t.Fatal("无关词面不应命中自定义意图")
	}

	// 写失效：改词面后新词面立即可用（30s 缓存不挡）
	if _, err := svc.Update(ctx, 1, customIntentInput("return_policy", "退换货政策", []string{"换货流程"})); err != nil {
		t.Fatalf("update: %v", err)
	}
	if r3 := rec.recognizeByRule(ctx, "帮我走换货流程"); r3 == nil || r3.IntentType != "return_policy" {
		t.Fatalf("写失效后新词面应命中, got %+v", r3)
	}
}

// ---------- 绑定层动态放行 ----------

func TestCustomIntent_BindingDynamicAllow_PG(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CustomIntent{}, &model.IntentToolChain{})
	ciSvc := NewCustomIntentService(repository.NewCustomIntentRepositoryWithDB(db))
	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	ctx := tenant.ContextWithDefault(context.Background())

	// 未放行：自定义意图 type 不能绑定
	if _, err := chainSvc.Create(ctx, chainUpsertInput("return_policy", step("knowledge.search", nil, 1))); err == nil {
		t.Fatal("自定义词条未建时绑定应被拒")
	}

	// 建词条 → 绑定放行
	if _, err := ciSvc.Create(ctx, customIntentInput("return_policy", "退换货政策", []string{"怎么退"})); err != nil {
		t.Fatalf("create custom intent: %v", err)
	}
	chainSvc.SetCustomIntentService(ciSvc)
	_, err := chainSvc.Create(ctx, chainUpsertInput("return_policy", step("knowledge.search", nil, 1)))
	if err != nil {
		t.Fatalf("自定义意图绑定应放行: %v", err)
	}
	// Lookup 命中（动态放行同样作用于行映射）
	spec, err := chainSvc.LookupIntentChain(ctx, "return_policy")
	if err != nil || spec == nil {
		t.Fatalf("Lookup 应命中, got (%v, %v)", spec, err)
	}
	if spec.IntentCode != "return_policy" {
		t.Fatalf("映射不符: %+v", spec)
	}

	// 单例注入后 IsCustomIntentType 面向识别侧
	SetCustomIntentService(ciSvc)
	t.Cleanup(func() { SetCustomIntentService(nil) })
	if !GetCustomIntentService().IsCustomIntentType(ctx, "return_policy") {
		t.Fatal("IsCustomIntentType 应放行")
	}

	// 删除词条 → 行映射 fail-closed（绑定行保留但不再供给规划）
	if err := ciSvc.Delete(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if spec2, err := chainSvc.LookupIntentChain(ctx, "return_policy"); err != nil || spec2 != nil {
		t.Fatalf("词条删除后行映射应 fail-closed, got (%v, %v)", spec2, err)
	}
}
