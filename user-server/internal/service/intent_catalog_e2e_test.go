package service

// intent_catalog_e2e_test.go TB-10 刀5：真库 E2E——
// 种子幂等 → 四消费方同源（识别/vocab/绑定/写面同步）→ 自定义词条热生效回归。
// PG 注入：POSTGRES_TEST_*（NewTestDBOrSkip 未注入自动跳过）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestIntentCatalogE2E_RealDB(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t,
		&model.IntentCatalog{},
		&model.CustomIntent{},
		&model.IntentToolChain{},
	)
	ctx := tenant.ContextWithDefault(context.Background())

	catRepo := repository.NewIntentCatalogRepositoryWithDB(db)
	customRepo := repository.NewCustomIntentRepositoryWithDB(db)
	chainRepo := repository.NewIntentToolChainRepositoryWithDB(db)

	catSvc := NewIntentCatalogService(catRepo)
	customSvc := NewCustomIntentService(customRepo)
	customSvc.SetIntentCatalogDependency(catSvc)
	chainSvc := NewIntentToolChainService(chainRepo)
	chainSvc.SetCustomIntentService(customSvc)

	// 注入全局：四消费方同源前置（清场防串扰）
	prevCat := GetIntentCatalogService()
	prevCustom := GetCustomIntentService()
	SetIntentCatalogService(catSvc)
	SetCustomIntentService(customSvc)
	t.Cleanup(func() {
		SetIntentCatalogService(prevCat)
		SetCustomIntentService(prevCustom)
	})

	// 1) 种子幂等：首写 N 行、二写 0 行
	n1, err := catSvc.SeedBuiltinDefaults(ctx)
	if err != nil {
		t.Fatalf("种子首写失败: %v", err)
	}
	n2, err := catSvc.SeedBuiltinDefaults(ctx)
	if err != nil || n2 != 0 {
		t.Fatalf("种子应幂等（二次 n=0），实得 n=%d err=%v", n2, err)
	}
	if n1 == 0 {
		t.Fatal("种子首写应为正数行")
	}

	// 2) 识别消费方同源：intentsForRuleMerged 以目录为源。
	// DefaultIntents 共 14 条（unknown/clarify 不在静态词典）；种子行中空词表
	// 键（runtime 独有+unknown/clarify）按 catalogRowToDef fail-closed 跳过，
	// 视图恰 14 条 = 原式零漂移。
	merged := intentsForRuleMerged(ctx)
	if len(merged) != 14 {
		t.Fatalf("种子后 merged 应恰 14 条（=DefaultIntents 零漂移），实得 %d", len(merged))
	}
	if !catSvc.IsCatalogIntent(ctx, "price_inquiry") || !catSvc.IsCatalogIntent(ctx, "chitchat") {
		t.Fatal("种子后目录应放行 price_inquiry 与 chitchat")
	}

	// 3) vocab 消费方同源：family 列直出
	vocab := chainSvc.GetChainVocabulary(ctx)
	if vocab.IntentFamilies["price_inquiry"] != "service" || vocab.IntentFamilies["chitchat"] != "runtime" {
		t.Fatalf("vocab family 直出错: %v / %v", vocab.IntentFamilies["price_inquiry"], vocab.IntentFamilies["chitchat"])
	}

	// 4) 绑定消费方同源：目录键放行
	if !chainSvc.isBindableIntent(ctx, "price_inquiry") {
		t.Fatal("绑定应放行目录键 price_inquiry")
	}

	// 5) 写面同步：自定义词条 Create → 目录 custom 行热生效（invalidate 后立即可见）
	if _, err := customSvc.Create(ctx, CustomIntentUpsertInput{
		Type: "e2e_sync_probe", Name: "E2E 同步探针",
		Keywords: []string{"探针词"}, Description: "E2E", Enabled: boolPtrCatalog(true),
	}); err != nil {
		t.Fatalf("自定义词条 Create 失败: %v", err)
	}
	if !catSvc.IsCatalogIntent(ctx, "e2e_sync_probe") {
		t.Fatal("写面同步后目录应立即放行 e2e_sync_probe（热生效）")
	}
	if !chainSvc.isBindableIntent(ctx, "e2e_sync_probe") {
		t.Fatal("绑定应放行热生效的自定义键")
	}

	// 6) 删除热生效：删除词条 → 目录行同步软删
	rec, err := customRepo.GetByType(ctx, "e2e_sync_probe")
	if err != nil || rec == nil {
		t.Fatalf("回读自定义词条失败: %v", err)
	}
	if err := customSvc.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if catSvc.IsCatalogIntent(ctx, "e2e_sync_probe") {
		t.Fatal("删除后目录不应再放行（软删热生效）")
	}
}

func boolPtrCatalog(b bool) *bool { return &b }
