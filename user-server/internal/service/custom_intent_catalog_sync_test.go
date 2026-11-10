package service

// custom_intent_catalog_sync_test.go TB-10 刀4：custom_intents 写面同步写
// 统一目录（source=custom 行）——Create/Update/启停 upsert、Delete 软删；
// 同步 fail-open（catalog 侧失败不阻塞主写）。

import (
	"context"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
)

// fakeCustomIntentRepoSync 最小自定义词条仓储（支撑写面同步用例）。
type fakeCustomIntentRepoSync struct {
	mu   sync.Mutex
	rows []model.CustomIntent
}

func (f *fakeCustomIntentRepoSync) Create(ctx context.Context, rec *model.CustomIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec.ID = uint(len(f.rows)) + 1
	f.rows = append(f.rows, *rec)
	return nil
}

func (f *fakeCustomIntentRepoSync) Update(ctx context.Context, rec *model.CustomIntent) error {
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

func (f *fakeCustomIntentRepoSync) Delete(ctx context.Context, id uint) error {
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

func (f *fakeCustomIntentRepoSync) GetByID(ctx context.Context, id uint) (*model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id {
			return &f.rows[i], nil
		}
	}
	return nil, errIntentCatalogFallbackNotFound
}

func (f *fakeCustomIntentRepoSync) GetByType(ctx context.Context, intentType string) (*model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].Type == intentType {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeCustomIntentRepoSync) ListEnabled(ctx context.Context) ([]model.CustomIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.CustomIntent{}
	for _, r := range f.rows {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeCustomIntentRepoSync) List(ctx context.Context, page, pageSize int) ([]model.CustomIntent, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.CustomIntent(nil), f.rows...), int64(len(f.rows)), nil
}

// catalogSyncRows 取目录仓当前行快照（测试断言用）。
func catalogSyncRows(f *fakeCatalogOnlyRepo) []model.IntentCatalog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.IntentCatalog(nil), f.rows...)
}

func TestCustomIntentWriteSyncsCatalog(t *testing.T) {
	ctx := context.Background()
	customRepo := &fakeCustomIntentRepoSync{}
	catRepo := &fakeCatalogOnlyRepo{}
	catSvc := NewIntentCatalogService(catRepo)
	customSvc := NewCustomIntentService(customRepo)
	customSvc.SetIntentCatalogDependency(catSvc)

	on := true
	off := false

	// 1) Create → catalog 落 source=custom 行
	rec, err := customSvc.Create(ctx, CustomIntentUpsertInput{
		Type: "vip_member", Name: "会员咨询",
		Keywords: []string{"会员", "VIP"}, Examples: []string{"有会员吗？"},
		Description: "会员相关咨询", Enabled: &on,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	rows := catalogSyncRows(catRepo)
	if len(rows) != 1 {
		t.Fatalf("Create 后目录应 1 行，实得 %d", len(rows))
	}
	row := rows[0]
	if row.IntentKey != "vip_member" || row.Source != intentCatalogSourceCustom ||
		row.Family != intentCatalogFamilyCustom || !row.Enabled || row.Name != "会员咨询" ||
		row.KeywordsJSON != `["会员","VIP"]` {
		t.Fatalf("目录行形态错: %+v", row)
	}

	// 2) Update（改词+禁用）→ catalog 行同步更新
	if _, err := customSvc.Update(ctx, rec.ID, CustomIntentUpsertInput{
		Type: "vip_member", Name: "会员咨询改",
		Keywords: []string{"会员卡"}, Description: "改后描述", Enabled: &off,
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	rows = catalogSyncRows(catRepo)
	if len(rows) != 1 {
		t.Fatalf("Update 后目录仍应 1 行，实得 %d", len(rows))
	}
	row = rows[0]
	if row.Name != "会员咨询改" || row.KeywordsJSON != `["会员卡"]` || row.Enabled {
		t.Fatalf("Update 同步错（应禁用+新词面）: %+v", row)
	}

	// 3) Delete → catalog source=custom 行软删
	if err := customSvc.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if rows = catalogSyncRows(catRepo); len(rows) != 0 {
		t.Fatalf("Delete 后目录应 0 行，实得 %d", len(rows))
	}

	// 4) catalog 侧失败 fail-open：目录读失败不阻塞主写
	brokenCat := &fakeCatalogOnlyRepo{err: errIntentCatalogFakeNotFound}
	customSvc2 := NewCustomIntentService(&fakeCustomIntentRepoSync{})
	customSvc2.SetIntentCatalogDependency(NewIntentCatalogService(brokenCat))
	if _, err := customSvc2.Create(ctx, CustomIntentUpsertInput{
		Type: "sync_fail_ok", Name: "同步失败容忍", Keywords: []string{"词"}, Enabled: &on,
	}); err != nil {
		t.Fatalf("catalog 读失败时主写不应被阻塞: %v", err)
	}

	// 5) catalog 未注入（nil）→ 完全跳过同步（既有单测路径零影响）
	customSvc3 := NewCustomIntentService(&fakeCustomIntentRepoSync{})
	if _, err := customSvc3.Create(ctx, CustomIntentUpsertInput{
		Type: "no_catalog", Name: "无目录", Keywords: []string{"词"}, Enabled: &on,
	}); err != nil {
		t.Fatalf("catalog 未注入时主写不应受影响: %v", err)
	}
}
