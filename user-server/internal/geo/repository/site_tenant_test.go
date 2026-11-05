// site_tenant_test.go 站点配置的租户契约。
//
// geo_sites 是租户表：列表只含本租户；同域名在不同租户各存一份；
// 跨租户读/改/删必须报不存在；GetActive 只取本租户的启用站点且结果确定。
package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	gsiteTenantA = uint(373)
	gsiteTenantB = uint(374)
)

func gsiteSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.GeoSite{})
	if db == nil {
		t.Fatal("测试库不可达：租户遮挡判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func TestGeoSiteRepository_TenantScoped(t *testing.T) {
	db := gsiteSetupDB(t)
	repo := NewGeoSiteRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gsiteTenantA)
	ctxB := tenant.WithTenantID(context.Background(), gsiteTenantB)
	sysCtx := tenant.WithSystemScope(context.Background())

	siteA := &model.GeoSite{Domain: "a.example", Provider: "cloudflare", Active: true}
	if err := repo.Create(ctxA, siteA); err != nil {
		t.Fatalf("租户 A 创建失败: %v", err)
	}
	if siteA.TenantID != gsiteTenantA {
		t.Fatalf("Create 未填租户: %d", siteA.TenantID)
	}
	// 同域名在租户 B 独立存在（唯一索引按租户复合）。
	if err := repo.Create(ctxB, &model.GeoSite{Domain: "a.example", Provider: "cloudflare", Active: true}); err != nil {
		t.Fatalf("租户 B 同域名创建失败（唯一索引未按租户复合？）: %v", err)
	}

	listA, totalA, err := repo.List(ctxA, 1, 20)
	if err != nil {
		t.Fatalf("租户 A 列表失败: %v", err)
	}
	if totalA != 1 || len(listA) != 1 || listA[0].TenantID != gsiteTenantA {
		t.Fatalf("租户 A 列表越界: total=%d list=%+v", totalA, listA)
	}

	activeA, err := repo.GetActive(ctxA)
	if err != nil {
		t.Fatalf("租户 A GetActive 失败: %v", err)
	}
	if activeA.TenantID != gsiteTenantA || activeA.Domain != "a.example" {
		t.Fatalf("GetActive 越界: %+v", activeA)
	}

	if _, err := repo.GetByID(ctxB, siteA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 GetByID 应报不存在，实际 err=%v", err)
	}

	hijack := &model.GeoSite{ID: siteA.ID, Domain: "hijack.example", Provider: "x", Active: false}
	if err := repo.Update(ctxB, hijack); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 Update 应报不存在，实际 err=%v", err)
	}
	var after model.GeoSite
	if err := db.WithContext(sysCtx).Where("id = ?", siteA.ID).First(&after).Error; err != nil {
		t.Fatalf("回查租户 A 站点失败: %v", err)
	}
	if after.Domain != "a.example" || !after.Active {
		t.Fatalf("跨租户 Update 改动了租户 A 的行: %+v", after)
	}

	// 部署时间回写只落本租户。
	now := time.Now()
	if err := repo.UpdateLastDeployAt(ctxA, siteA.ID, now); err != nil {
		t.Fatalf("回写部署时间失败: %v", err)
	}
	var bSite model.GeoSite
	if err := db.WithContext(sysCtx).Where("tenant_id = ?", gsiteTenantB).First(&bSite).Error; err != nil {
		t.Fatalf("回查租户 B 站点失败: %v", err)
	}
	if bSite.LastDeployAt != nil {
		t.Fatalf("部署时间回写越界到租户 B: %v", bSite.LastDeployAt)
	}

	if err := repo.Delete(ctxB, siteA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨租户 Delete 应报不存在，实际 err=%v", err)
	}
	var countA int64
	if err := db.WithContext(sysCtx).Model(&model.GeoSite{}).
		Where("tenant_id = ?", gsiteTenantA).Count(&countA).Error; err != nil {
		t.Fatalf("回查租户 A 站点数失败: %v", err)
	}
	if countA != 1 {
		t.Fatalf("跨租户 Delete 删掉了租户 A 的行: count=%d", countA)
	}

	if err := repo.Create(context.Background(), &model.GeoSite{Domain: "x.example"}); err == nil {
		t.Fatal("无租户 ctx 创建应报错，实际成功")
	}
}

// TestGeoSiteRepository_停用值不被列默认值覆盖 与 #37 同型：Active 声明
// default:true 时显式 false 会被 GORM 省略，落库变 true。
func TestGeoSiteRepository_停用值不被列默认值覆盖(t *testing.T) {
	db := gsiteSetupDB(t)
	repo := NewGeoSiteRepositoryWithDB(db)
	ctxA := tenant.WithTenantID(context.Background(), gsiteTenantA)

	site := &model.GeoSite{Domain: "off.example", Provider: "cloudflare", Active: false}
	if err := repo.Create(ctxA, site); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	got, err := repo.GetByID(ctxA, site.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Active {
		t.Fatal("显式 active=false 落库变成了 true")
	}
}

// TestGeoSiteRepository_删除后可重建同域名 软删除行仍占用唯一索引；
// 运营删站点后重新添加同域名必须成功（原位复活），而不是抛 23505。
func TestGeoSiteRepository_删除后可重建同域名(t *testing.T) {
	db := gsiteSetupDB(t)
	repo := NewGeoSiteRepositoryWithDB(db)
	ctx := tenant.WithTenantID(context.Background(), gsiteTenantA)

	first := &model.GeoSite{Domain: "recreate.example", Provider: "cloudflare", HugoPath: "/old", Active: true}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	second := &model.GeoSite{Domain: "recreate.example", Provider: "cloudflare", HugoPath: "/new", Active: true}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("删除后重建同域名失败: %v", err)
	}
	if second.ID != first.ID || second.TenantID != gsiteTenantA || second.HugoPath != "/new" {
		t.Fatalf("应原位复活并覆盖字段: id=%s path=%s", second.ID, second.HugoPath)
	}
	got, err := repo.GetByID(ctx, first.ID)
	if err != nil || got.HugoPath != "/new" {
		t.Fatalf("复活后读取不对: %+v err=%v", got, err)
	}
}
