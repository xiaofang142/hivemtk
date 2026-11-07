// config_brand_profile_test.go 探针品牌提示词投影读的 SQL 契约（PG 定向，8217）。
//
// AV-04c 批次3 自 geo/service/search_probe.go NewEngineProbesFromDB 下沉。
// 两处必须钉死的行为：
//  1. Table 直查形态：不挂软删除过滤（改前即如此——差异说明见 GetBrandProfile 注释）；
//  2. 租户作用域：ctx 租户 ⇒ 本租户行；system scope ⇒ 平台级首行（探针旧调用兼容路径）。
package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func brandProfileSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoConfig{})
	if db == nil {
		t.Fatal("测试库不可达：品牌投影契约判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoConfigRepo_GetBrandProfileProjection(t *testing.T) {
	db := brandProfileSetupDB(t)
	repo := NewGeoConfigRepositoryWithDB(db)
	ctx := context.Background()

	if err := db.WithContext(ctx).Create(&model.GeoConfig{
		BrandName:        "蜂巢",
		BrandDescription: "AI 工作流平台",
	}).Error; err != nil {
		t.Fatalf("创建配置行失败: %v", err)
	}

	cfg, err := repo.GetBrandProfile(ctx)
	if err != nil {
		t.Fatalf("GetBrandProfile 失败: %v", err)
	}
	if cfg.BrandName != "蜂巢" || cfg.BrandDescription != "AI 工作流平台" {
		t.Fatalf("两列投影应完整读回，实际 %+v", cfg)
	}

	// 空表 ⇒ First 的 ErrRecordNotFound 原样上抛（调用方 fail-open 置空提示词）。
	db2 := brandProfileSetupDB(t)
	_, err = NewGeoConfigRepositoryWithDB(db2).GetBrandProfile(ctx)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("空表应返回 ErrRecordNotFound，实际 %v", err)
	}
}

func TestGeoConfigRepo_GetBrandProfileSoftDeletedRowStillVisible(t *testing.T) {
	// 钉住 Table 直查形态：软删除行（deleted_at 非空）不挂过滤，与改前逐字一致。
	// （geo_config 无删除路径，此断言是形态保真的证据，不是产品行为承诺。）
	db := brandProfileSetupDB(t)
	repo := NewGeoConfigRepositoryWithDB(db)
	ctx := context.Background()

	if err := db.WithContext(ctx).Create(&model.GeoConfig{BrandName: "被软删的品牌"}).Error; err != nil {
		t.Fatalf("创建配置行失败: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`UPDATE geo_config SET deleted_at = NOW()`).Error; err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	cfg, err := repo.GetBrandProfile(ctx)
	if err != nil {
		t.Fatalf("软删行应仍可见（Table 直查不挂软删过滤），实际错误 %v", err)
	}
	if cfg.BrandName != "被软删的品牌" {
		t.Fatalf("软删行投影应原样读回，实际 %+v", cfg)
	}
}

func TestGeoConfigRepo_GetBrandProfileTenantScoped(t *testing.T) {
	db := brandProfileSetupDB(t)
	repo := NewGeoConfigRepositoryWithDB(db)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	const (
		ta = uint(351)
		tb = uint(352)
	)
	sysCtx := tenant.WithSystemScope(context.Background())

	if err := db.WithContext(sysCtx).Create(&model.GeoConfig{BrandName: "占位A"}).Error; err != nil {
		t.Fatalf("租户 A 建配置失败: %v", err)
	}
	// 租户归属用带 WHERE 的定向改写固定（system scope Create 落列默认值 1）。
	if err := db.WithContext(sysCtx).Exec(`UPDATE geo_config SET brand_name = '租户A品牌', tenant_id = ? WHERE brand_name = '占位A'`, ta).Error; err != nil {
		t.Fatalf("钉租户 A 行失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Create(&model.GeoConfig{BrandName: "占位B"}).Error; err != nil {
		t.Fatalf("租户 B 建配置失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Exec(`UPDATE geo_config SET brand_name = '租户B品牌', tenant_id = ? WHERE brand_name = '占位B'`, tb).Error; err != nil {
		t.Fatalf("钉租户 B 行失败: %v", err)
	}

	cfgA, err := repo.GetBrandProfile(tenant.WithTenantID(context.Background(), ta))
	if err != nil {
		t.Fatalf("租户 A 投影读失败: %v", err)
	}
	if cfgA.BrandName != "租户A品牌" {
		t.Fatalf("ctx 租户应读到本租户行，实际 %+v", cfgA)
	}

	cfgSys, err := repo.GetBrandProfile(sysCtx)
	if err != nil {
		t.Fatalf("system scope 投影读失败: %v", err)
	}
	// system scope = 跨租户 union 首行（First 按表主键序），与改前兼容路径一致。
	if cfgSys.BrandName != "租户A品牌" && cfgSys.BrandName != "租户B品牌" {
		t.Fatalf("system scope 应读到平台级首行，实际 %+v", cfgSys)
	}
}
