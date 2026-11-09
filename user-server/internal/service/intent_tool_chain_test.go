package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------- fake 仓储（按 ctx 租户分桶，模拟仓储层租户作用域） ----------

type fakeChainRepo struct {
	mu    sync.Mutex
	rows  map[uint][]*model.IntentToolChain // tenantID -> rows
	seq   uint
	dbErr error
}

func newFakeChainRepo() *fakeChainRepo {
	return &fakeChainRepo{rows: map[uint][]*model.IntentToolChain{}}
}

func (f *fakeChainRepo) tenantOf(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (f *fakeChainRepo) Create(ctx context.Context, rec *model.IntentToolChain) error {
	if f.dbErr != nil {
		return f.dbErr
	}
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

func (f *fakeChainRepo) Update(ctx context.Context, rec *model.IntentToolChain) error {
	if f.dbErr != nil {
		return f.dbErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ID == rec.ID {
			r.Name, r.IntentCode, r.StepsJSON, r.ReplyHint, r.Enabled = rec.Name, rec.IntentCode, rec.StepsJSON, rec.ReplyHint, rec.Enabled
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeChainRepo) Delete(ctx context.Context, id uint) error {
	if f.dbErr != nil {
		return f.dbErr
	}
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

func (f *fakeChainRepo) GetByID(ctx context.Context, id uint) (*model.IntentToolChain, error) {
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

func (f *fakeChainRepo) GetByIntent(ctx context.Context, intentCode string) (*model.IntentToolChain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.IntentCode == intentCode {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeChainRepo) ListEnabled(ctx context.Context) ([]model.IntentToolChain, error) {
	if f.dbErr != nil {
		return nil, f.dbErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.IntentToolChain{}
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.Enabled {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeChainRepo) List(ctx context.Context, intentCode string, page, pageSize int) ([]model.IntentToolChain, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.IntentToolChain{}
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if intentCode == "" || r.IntentCode == intentCode {
			out = append(out, *r)
		}
	}
	return out, int64(len(out)), nil
}

// ---------- 夹具 ----------

func chainUpsertInput(intent string, steps ...model.IntentChainStepConfig) ChainUpsertInput {
	return ChainUpsertInput{Name: "n", IntentCode: intent, Steps: steps, ReplyHint: "hint", Enabled: tbPtr(true)}
}

func tbPtr(b bool) *bool { return &b }

func step(tool string, args map[string]any, prio int) model.IntentChainStepConfig {
	return model.IntentChainStepConfig{ToolName: tool, Args: args, Priority: prio, Enabled: true}
}

// ---------- 写侧校验矩阵 ----------

func TestChainUpsertValidation(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())

	cases := []struct {
		name string
		in   ChainUpsertInput
	}{
		{"意图不在词表", chainUpsertInput("made_up_intent", step("order.lookup", nil, 1))},
		{"零步骤", chainUpsertInput("order_status")},
		{"空工具名", chainUpsertInput("order_status", step("  ", nil, 1))},
		{"工具重复", chainUpsertInput("order_status", step("order.lookup", nil, 1), step("order.lookup", nil, 2))},
		{"白名单外模板变量", chainUpsertInput("order_status", step("order.lookup", map[string]any{"customer_id": "{{evil}}"}, 1))},
		{"未闭合占位", chainUpsertInput("order_status", step("order.lookup", map[string]any{"k": "{{open"}, 1))},
		{"值类型不支持", chainUpsertInput("order_status", step("order.lookup", map[string]any{"k": []string{"x"}}, 1))},
	}
	for _, tc := range cases {
		_, err := svc.Create(ctx, tc.in)
		if err == nil {
			t.Fatalf("%s: 应拒绝", tc.name)
		}
		if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeParamInvalid {
			t.Fatalf("%s: 应 4000 参数错误, got %v", tc.name, err)
		}
	}
}

func TestChainUpsertValidation_StepLimit(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	steps := make([]model.IntentChainStepConfig, agent_runtime.ChainMaxSteps+1)
	for i := range steps {
		steps[i] = step("knowledge.search", nil, i+1)
	}
	_, err := svc.Create(ctx, chainUpsertInput("inquiry", steps...))
	if err == nil {
		t.Fatal("超步数上限应拒绝")
	}
}

func TestChainVocabulary_PinnedToRuntimeIntents(t *testing.T) {
	vocab := ChainIntentVocabulary()
	for _, want := range []string{
		string(agent_runtime.IntentOrderStatus), string(agent_runtime.IntentAfterSales),
		string(agent_runtime.IntentRefund), string(agent_runtime.IntentInquiry),
	} {
		found := false
		for _, v := range vocab {
			if v == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("词表缺 %s", want)
		}
	}
	vars := agent_runtime.ChainTemplateVarNames()
	for _, want := range []string{"customer_id", "session_id", "content", "channel_type", "account_id"} {
		found := false
		for _, v := range vars {
			if v == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("模板变量白名单缺 %s", want)
		}
	}
}

// ---------- CRUD + 冲突 ----------

func TestChainCRUD_DuplicateIntentConflict(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	steps := []model.IntentChainStepConfig{step("order.lookup", nil, 1)}

	if _, err := svc.Create(ctx, chainUpsertInput("order_status", steps...)); err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}
	_, err := svc.Create(ctx, chainUpsertInput("order_status", steps...))
	if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeConflict {
		t.Fatalf("重复意图应 4009 冲突, got %v", err)
	}
}

func TestChainCRUD_UpdateAndDelete(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	rec, err := svc.Create(ctx, chainUpsertInput("after_sales", step("order.lookup", nil, 1)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	upd, err := svc.Update(ctx, rec.ID, chainUpsertInput("refund", step("knowledge.search", nil, 1)))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.IntentCode != "refund" {
		t.Fatalf("update 后 intent_code 应为 refund, got %s", upd.IntentCode)
	}
	if err := svc.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, rec.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除后读取应 NotFound, got %v", err)
	}
}

// ---------- Lookup：命中/未命中/坏行跳过/租户隔离/写失效 ----------

func TestChainLookup_MissReturnsNilNil(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	spec, err := svc.LookupIntentChain(ctx, "order_status")
	if err != nil || spec != nil {
		t.Fatalf("无绑定应 (nil,nil), got (%v,%v)", spec, err)
	}
}

func TestChainLookup_HitMapsSpec(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	enabled := true
	rec, err := svc.Create(ctx, ChainUpsertInput{
		Name: "n", IntentCode: "order_status",
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 1, Enabled: true},
			{ToolName: "knowledge.search", Priority: 2, Enabled: false},
		},
		ReplyHint: "先查订单", Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	spec, err := svc.LookupIntentChain(ctx, "order_status")
	if err != nil || spec == nil {
		t.Fatalf("命中应返回 spec, got (%v,%v)", spec, err)
	}
	if spec.IntentCode != "order_status" || spec.ReplyHint != "先查订单" || len(spec.Steps) != 2 ||
		spec.Steps[0].ToolName != "order.lookup" || spec.Steps[0].Args["customer_id"] != "{{customer_id}}" ||
		spec.Steps[1].Enabled {
		t.Fatalf("映射不符: %+v", spec)
	}

	// 整链禁用（同意图单绑定，禁用走编辑而非重建）→ 不入 ListEnabled，应 (nil,nil)
	if _, err := svc.Update(ctx, rec.ID, ChainUpsertInput{
		Name: "n", IntentCode: "order_status",
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 1, Enabled: true},
		},
		ReplyHint: "先查订单", Enabled: tbPtr(false),
	}); err != nil {
		t.Fatalf("禁用 update: %v", err)
	}
	if spec2, err := svc.LookupIntentChain(ctx, "order_status"); err != nil || spec2 != nil {
		t.Fatalf("禁用后应 (nil,nil), got (%v,%v)", spec2, err)
	}
}

func TestChainLookup_MalformedRowSkipped(t *testing.T) {
	repo := newFakeChainRepo()
	repo.Create(tenant.ContextWithDefault(context.Background()), &model.IntentToolChain{
		IntentCode: "order_status", StepsJSON: "{not-json", Enabled: true,
	})
	repo.Create(tenant.ContextWithDefault(context.Background()), &model.IntentToolChain{
		IntentCode: "made_up_intent", StepsJSON: "[]", Enabled: true,
	})
	svc := NewIntentToolChainService(repo)
	ctx := tenant.ContextWithDefault(context.Background())
	spec, err := svc.LookupIntentChain(ctx, "order_status")
	if err != nil || spec != nil {
		t.Fatalf("坏行应 fail-closed 跳过, got (%v,%v)", spec, err)
	}
}

func TestChainLookup_TenantIsolation(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctxA := tenant.WithTenantID(context.Background(), 501)
	ctxB := tenant.WithTenantID(context.Background(), 502)
	enabled := true
	if _, err := svc.Create(ctxA, ChainUpsertInput{
		Name: "a", IntentCode: "order_status",
		Steps:   []model.IntentChainStepConfig{{ToolName: "order.lookup", Priority: 1, Enabled: true}},
		Enabled: &enabled,
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	specA, err := svc.LookupIntentChain(ctxA, "order_status")
	if err != nil || specA == nil {
		t.Fatalf("A 应命中, got (%v,%v)", specA, err)
	}
	specB, err := svc.LookupIntentChain(ctxB, "order_status")
	if err != nil || specB != nil {
		t.Fatalf("B 不应看到 A 的绑定, got (%v,%v)", specB, err)
	}
}

func TestChainLookup_WriteInvalidatesCache(t *testing.T) {
	svc := NewIntentToolChainService(newFakeChainRepo())
	ctx := tenant.ContextWithDefault(context.Background())
	enabled := true
	in := ChainUpsertInput{
		Name: "n", IntentCode: "after_sales",
		Steps:   []model.IntentChainStepConfig{{ToolName: "order.lookup", Priority: 1, Enabled: true}},
		Enabled: &enabled,
	}
	if _, err := svc.Create(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	if spec, _ := svc.LookupIntentChain(ctx, "after_sales"); spec == nil {
		t.Fatal("首次查询应命中")
	}
	// 更新为禁用 → 下一次查询必须反映新状态（写失效生效）
	if _, err := svc.Update(ctx, 1, ChainUpsertInput{Name: "n", IntentCode: "after_sales",
		Steps:   []model.IntentChainStepConfig{{ToolName: "order.lookup", Priority: 1, Enabled: true}},
		Enabled: tbPtr(false)}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if spec, err := svc.LookupIntentChain(ctx, "after_sales"); err != nil || spec != nil {
		t.Fatalf("写失效后应 (nil,nil), got (%v,%v)", spec, err)
	}
}

func TestChainLookup_RepoErrorPropagates(t *testing.T) {
	repo := newFakeChainRepo()
	repo.dbErr = errors.New("db down")
	svc := NewIntentToolChainService(repo)
	spec, err := svc.LookupIntentChain(tenant.ContextWithDefault(context.Background()), "order_status")
	if err == nil || spec != nil {
		t.Fatalf("仓储错误应上抛, got (%v,%v)", spec, err)
	}
}

// ---------- 真实 PG（8217 专用库）：仓储与 service 端到端 ----------

func setupChainTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t, &model.IntentToolChain{})
}

func TestChainService_PG_RoundTrip(t *testing.T) {
	db := setupChainTestDB(t)
	svc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	ctxA := tenant.WithTenantID(context.Background(), 911)
	ctxB := tenant.WithTenantID(context.Background(), 912)
	enabled := true

	rec, err := svc.Create(ctxA, ChainUpsertInput{
		Name: "物流链", IntentCode: "after_sales",
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 1, Enabled: true},
			{ToolName: "logistics.track", Args: map[string]any{"channel": "{{channel_type}}"}, Priority: 2, Enabled: true},
		},
		ReplyHint: "先查订单再查物流", Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.TenantID != 911 {
		t.Fatalf("租户应取自 ctx, got %d", rec.TenantID)
	}

	specA, err := svc.LookupIntentChain(ctxA, "after_sales")
	if err != nil || specA == nil || len(specA.Steps) != 2 {
		t.Fatalf("A 端到端应命中两步链, got (%v,%v)", specA, err)
	}
	specB, _ := svc.LookupIntentChain(ctxB, "after_sales")
	if specB != nil {
		t.Fatal("B 租户不应命中 A 的绑定")
	}

	list, total, err := svc.List(ctxA, "", 1, 50)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list 应恰 1 行, got total=%d len=%d err=%v", total, len(list), err)
	}
	if err := svc.Delete(ctxA, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if specA2, _ := svc.LookupIntentChain(ctxA, "after_sales"); specA2 != nil {
		t.Fatal("删除+写失效后不应再命中")
	}
}

func TestChainService_PG_UpdateConflictAcrossIntents(t *testing.T) {
	db := setupChainTestDB(t)
	svc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	ctx := tenant.ContextWithDefault(context.Background())
	enabled := true
	steps := []model.IntentChainStepConfig{{ToolName: "order.lookup", Priority: 1, Enabled: true}}
	if _, err := svc.Create(ctx, ChainUpsertInput{IntentCode: "order_status", Steps: steps, Enabled: &enabled}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	rec2, err := svc.Create(ctx, ChainUpsertInput{IntentCode: "refund", Steps: steps, Enabled: &enabled})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	_, err = svc.Update(ctx, rec2.ID, ChainUpsertInput{IntentCode: "order_status", Steps: steps, Enabled: &enabled})
	if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeConflict {
		t.Fatalf("改到已占用意图应 4009, got %v", err)
	}
}
