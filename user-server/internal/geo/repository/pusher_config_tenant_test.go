// pusher_config_tenant_test.go 蜘蛛平台配置的租户契约。
//
// geo_pusher_configs 是租户表：同一平台名在不同租户各存一份（唯一索引为
// (tenant_id, platform)）；跨租户读/改/删必须报不存在；配额回写只落在本租户。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	gpcTenantA = uint(365)
	gpcTenantB = uint(366)
)

func gpcSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoPusherConfig{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoPusherConfigRepository_TenantScoped(t *testing.T) {
	db := gpcSetupDB(t)
	repo := NewGeoPusherConfigRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gpcTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gpcTenantB)
	sysCtx := tenant.WithSystemScope(context.Background())

	cfgA := &model.GeoPusherConfig{
		Platform:   "baidu",
		ConfigJSON: datatypes.JSON(`{"site":"a.example","token":"tok-a"}`),
		DailyLimit: 100,
		Active:     true,
	}
	if err := repo.Create(ctxA, cfgA); err != nil {
		t.Fatalf("租户 A 创建失败: %v", err)
	}
	if cfgA.TenantID != gpcTenantA {
		t.Fatalf("Create 未填租户: %d", cfgA.TenantID)
	}
	// 同名平台在租户 B 必须能并存（唯一索引是 (tenant_id, platform)）。
	cfgB := &model.GeoPusherConfig{
		Platform:   "baidu",
		ConfigJSON: datatypes.JSON(`{"site":"b.example","token":"tok-b"}`),
		DailyLimit: 200,
		Active:     true,
	}
	if err := repo.Create(ctxB, cfgB); err != nil {
		t.Fatalf("租户 B 同名平台创建失败（唯一索引未按租户复合）: %v", err)
	}

	listA, totalA, err := repo.List(ctxA, 1, 20)
	if err != nil {
		t.Fatalf("租户 A 列表失败: %v", err)
	}
	if totalA != 1 || len(listA) != 1 || listA[0].TenantID != gpcTenantA {
		t.Fatalf("租户 A 列表越界: total=%d list=%+v", totalA, listA)
	}

	// 凭据往返：读取出口还原成原 JSON 对象。
	var m map[string]any
	if err := json.Unmarshal(listA[0].ConfigJSON, &m); err != nil {
		t.Fatalf("读取出口不是合法 JSON: %s", listA[0].ConfigJSON)
	}
	if m["token"] != "tok-a" {
		t.Fatalf("凭据解密/还原失败: %v", m)
	}

	// 跨租户读 → 不存在。
	if _, err := repo.GetByID(ctxB, cfgA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 GetByID 应报不存在，实际 err=%v", err)
	}

	// 跨租户改 → 不存在，且不改租户 A 的行。
	hijack := &model.GeoPusherConfig{ID: cfgA.ID, Platform: "baidu", ConfigJSON: datatypes.JSON(`{}`), DailyLimit: 1}
	if err := repo.Update(ctxB, hijack); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 Update 应报不存在，实际 err=%v", err)
	}
	var after model.GeoPusherConfig
	if err := db.WithContext(sysCtx).Where("id = ?", cfgA.ID).First(&after).Error; err != nil {
		t.Fatalf("回查租户 A 配置失败: %v", err)
	}
	if after.DailyLimit != 100 {
		t.Fatalf("跨租户 Update 改动了租户 A 的行: daily_limit=%d", after.DailyLimit)
	}

	// 配额回写只落本租户。
	if err := repo.UpdateUsage(ctxA, "baidu", 42); err != nil {
		t.Fatalf("回写配额失败: %v", err)
	}
	var usedA, usedB int
	if err := db.WithContext(sysCtx).Model(&model.GeoPusherConfig{}).
		Where("tenant_id = ? AND platform = ?", gpcTenantA, "baidu").Pluck("used_today", &usedA).Error; err != nil {
		t.Fatalf("回查租户 A 用量失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Model(&model.GeoPusherConfig{}).
		Where("tenant_id = ? AND platform = ?", gpcTenantB, "baidu").Pluck("used_today", &usedB).Error; err != nil {
		t.Fatalf("回查租户 B 用量失败: %v", err)
	}
	if usedA != 42 || usedB != 0 {
		t.Fatalf("配额回写越界: A=%d B=%d", usedA, usedB)
	}

	// 跨租户删 → 不存在，行仍在。
	if err := repo.Delete(ctxB, cfgA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 Delete 应报不存在，实际 err=%v", err)
	}
	var countA int64
	if err := db.WithContext(sysCtx).Model(&model.GeoPusherConfig{}).
		Where("tenant_id = ?", gpcTenantA).Count(&countA).Error; err != nil {
		t.Fatalf("回查租户 A 配置数失败: %v", err)
	}
	if countA != 1 {
		t.Fatalf("跨租户 Delete 删掉了租户 A 的行: count=%d", countA)
	}

	// fail-closed：无租户 ctx 不允许写租户表。
	if err := repo.Create(context.Background(), &model.GeoPusherConfig{
		Platform:   "indexnow",
		ConfigJSON: datatypes.JSON(`{}`),
	}); err == nil {
		t.Fatal("无租户 ctx 创建应报错，实际成功")
	}
}

// TestGeoPusherConfigRepository_停用值不被列默认值覆盖 同上：Active 声明
// default:true 会让显式 false 被省略，落库变 true。
func TestGeoPusherConfigRepository_停用值不被列默认值覆盖(t *testing.T) {
	db := gpcSetupDB(t)
	repo := NewGeoPusherConfigRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gpcTenantA)

	cfg := &model.GeoPusherConfig{
		Platform: "indexnow", ConfigJSON: datatypes.JSON(`{"host":"a.example"}`), Active: false,
	}
	if err := repo.Create(ctxA, cfg); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	got, err := repo.GetByID(ctxA, cfg.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Active {
		t.Fatal("显式 active=false 落库变成了 true")
	}
}

// TestGeoPusherConfigRepository_删除后可重建同平台 软删除行仍占用唯一索引；
// 删除百度配置后重新添加同平台必须成功（原位复活）。
func TestGeoPusherConfigRepository_删除后可重建同平台(t *testing.T) {
	db := gpcSetupDB(t)
	repo := NewGeoPusherConfigRepositoryWithDB(db)
	ctx := tenant.WithTenantID(context.Background(), gpcTenantA)

	first := &model.GeoPusherConfig{
		Platform:   "baidu",
		ConfigJSON: datatypes.JSON(`{"token":"old"}`),
		DailyLimit: 10,
		Active:     true,
	}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	second := &model.GeoPusherConfig{
		Platform:   "baidu",
		ConfigJSON: datatypes.JSON(`{"token":"new"}`),
		DailyLimit: 20,
		Active:     true,
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("删除后重建同平台失败: %v", err)
	}
	if second.ID != first.ID || second.TenantID != gpcTenantA || second.DailyLimit != 20 {
		t.Fatalf("应原位复活并覆盖字段: id=%s limit=%d", second.ID, second.DailyLimit)
	}
	got, err := repo.GetByID(ctx, first.ID)
	if err != nil || got.DailyLimit != 20 {
		t.Fatalf("复活后读取不对: %+v err=%v", got, err)
	}
}
