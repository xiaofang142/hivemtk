package repository

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestIntentCatalogRepo_CRUDAndTenantScope 真库验证刀1：启停过滤/按键读/
// 更新/软删/租户隔离/db nil 守卫。
func TestIntentCatalogRepo_CRUDAndTenantScope(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentCatalog{})
	repo := NewIntentCatalogRepositoryWithDB(db)
	ctx := context.Background()

	// 1) Create + GetByKey 命中；未命中 (nil, nil)
	if err := repo.Create(ctx, &model.IntentCatalog{
		IntentKey: "price_inquiry", Source: "default", Family: "service",
		Name: "价格咨询", KeywordsJSON: `["多少钱","价格"]`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	row, err := repo.GetByKey(ctx, "price_inquiry")
	if err != nil {
		t.Fatalf("GetByKey 失败: %v", err)
	}
	if row == nil || row.IntentKey != "price_inquiry" || row.Source != "default" || row.Family != "service" {
		t.Fatalf("GetByKey 命中错: %+v", row)
	}
	miss, err := repo.GetByKey(ctx, "no_such_intent")
	if err != nil || miss != nil {
		t.Fatalf("GetByKey 未命中应返 (nil,nil)，实得 (%+v,%v)", miss, err)
	}

	// 2) ListEnabled 只回启用行
	// 禁用行用 map 插入：GORM 对带 default:true 的 bool 列会省略结构体零值，
	// false 会被列默认值 true 顶掉（与 007 看板 LLM 夹具同口径）。
	if err := db.Model(&model.IntentCatalog{}).Create(map[string]interface{}{
		"intent_key": "churn", "source": "default", "family": "service",
		"name": "流失", "keywords_json": "[]", "enabled": false,
	}).Error; err != nil {
		t.Fatalf("插禁用行失败: %v", err)
	}
	enabled, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled 失败: %v", err)
	}
	if len(enabled) != 1 || enabled[0].IntentKey != "price_inquiry" {
		t.Fatalf("ListEnabled 应只回启用行，实得 %+v", enabled)
	}

	// 3) Update 改名+禁用
	row.Name = "价格咨询改"
	row.Enabled = false
	if err := repo.Update(ctx, row); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	after, err := repo.GetByKey(ctx, "price_inquiry")
	if err != nil {
		t.Fatalf("Update 后 GetByKey 失败: %v", err)
	}
	if after != nil {
		t.Fatalf("禁用后 GetByKey 应未命中，实得 %+v", after)
	}

	// 4) Delete 软删：先建专用启用行，取 ID 后删，删后 GetByKey 未命中；
	//    删除不存在行报 ErrRecordNotFound。
	if err := repo.Create(ctx, &model.IntentCatalog{
		IntentKey: "social", Source: "default", Family: "service",
		Name: "社交", KeywordsJSON: `[]`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create(social) 失败: %v", err)
	}
	social, err := repo.GetByKey(ctx, "social")
	if err != nil || social == nil {
		t.Fatalf("GetByKey(social) 失败: %+v err=%v", social, err)
	}
	if err := repo.Delete(ctx, social.ID); err != nil {
		t.Fatalf("Delete(social) 失败: %v", err)
	}
	gone, err := repo.GetByKey(ctx, "social")
	if err != nil || gone != nil {
		t.Fatalf("Delete 后 GetByKey 应 (nil,nil)，实得 (%+v,%v)", gone, err)
	}
	if err := repo.Delete(ctx, 999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Delete 不存在行应返 ErrRecordNotFound，实得 %v", err)
	}

	// 5) 显式租户 stamps
	ctxA := tenant.WithTenantID(context.Background(), 42)
	if err := repo.Create(ctxA, &model.IntentCatalog{
		IntentKey: "greeting", Source: "default", Family: "service",
		Name: "问候", KeywordsJSON: `[]`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create 租户 42 失败: %v", err)
	}
	rowA, err := repo.GetByKey(ctxA, "greeting")
	if err != nil || rowA == nil || rowA.TenantID != 42 {
		t.Fatalf("租户 42 行错: %+v err=%v", rowA, err)
	}

	// 6) db nil 守卫
	nilRepo := NewIntentCatalogRepositoryWithDB(nil)
	if err := nilRepo.Create(ctx, &model.IntentCatalog{IntentKey: "x"}); !errors.Is(err, errIntentCatalogDBNotReady) {
		t.Fatalf("nil db Create 应返 errIntentCatalogDBNotReady，实得 %v", err)
	}
	if _, err := nilRepo.GetByKey(ctx, "x"); !errors.Is(err, errIntentCatalogDBNotReady) {
		t.Fatalf("nil db GetByKey 应返 errIntentCatalogDBNotReady，实得 %v", err)
	}
	if _, err := nilRepo.ListEnabled(ctx); !errors.Is(err, errIntentCatalogDBNotReady) {
		t.Fatalf("nil db ListEnabled 应返 errIntentCatalogDBNotReady，实得 %v", err)
	}
	if err := nilRepo.Update(ctx, &model.IntentCatalog{ID: 1}); !errors.Is(err, errIntentCatalogDBNotReady) {
		t.Fatalf("nil db Update 应返 errIntentCatalogDBNotReady，实得 %v", err)
	}
	if err := nilRepo.Delete(ctx, 1); !errors.Is(err, errIntentCatalogDBNotReady) {
		t.Fatalf("nil db Delete 应返 errIntentCatalogDBNotReady，实得 %v", err)
	}
}
