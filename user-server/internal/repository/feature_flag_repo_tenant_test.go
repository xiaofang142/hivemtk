// feature_flag_repo_tenant_test.go FeatureFlag 的租户契约。
//
// 开关的唯一性语义是 (tenant_id, key)：两个租户可以各建同名开关、各自灰度，
// 互相不能读到/改到对方那一行。仓储层验证三件事：写侧填充、读侧过滤、
// on 模式漏绑 ctx 被拒。
package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

func TestFeatureFlag_CRUDIsTenantScoped(t *testing.T) {
	db := ffSetupDB(t, tenant.ModeOn)
	flagA := ffSeedFlag(t, db, ffTenantA, "ff_same_key")
	ffSeedFlag(t, db, ffTenantB, "ff_same_key")

	repo := NewFeatureFlagRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), ffTenantA)
	ctxB := tenant.WithTenantID(context.Background(), ffTenantB)

	gotA, err := repo.GetByKey(ctxA, "ff_same_key")
	if err != nil {
		t.Fatalf("租户 A 按 key 读开关失败: %v", err)
	}
	if gotA.ID != flagA.ID || gotA.TenantID != ffTenantA {
		t.Fatalf("租户 A 读到 %+v，期望自己的 flag(%d)", gotA, flagA.ID)
	}
	gotB, err := repo.GetByKey(ctxB, "ff_same_key")
	if err != nil {
		t.Fatalf("租户 B 按 key 读开关失败: %v", err)
	}
	if gotB.ID == flagA.ID || gotB.TenantID != ffTenantB {
		t.Fatalf("租户 B 读到了 A 的行（id=%d）⇒ key 的唯一性没按租户收敛", gotB.ID)
	}

	listA, totalA, err := repo.List(ctxA, 1, 10)
	if err != nil {
		t.Fatalf("租户 A 列表失败: %v", err)
	}
	if totalA != 1 || len(listA) != 1 || listA[0].TenantID != ffTenantA {
		t.Fatalf("租户 A 列表 total=%d rows=%+v，期望仅自己 1 条", totalA, listA)
	}

	if _, err := repo.GetByID(ctxA, gotB.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("租户 A 按 B 的 id 读取返回 err=%v，期望 ErrRecordNotFound", err)
	}
}

func TestFeatureFlag_CreateFillsTenantAndEnforcesCompositeUnique(t *testing.T) {
	db := ffSetupDB(t, tenant.ModeOn)
	repo := NewFeatureFlagRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), ffTenantA)

	created := &model.FeatureFlag{Key: "ff_created", Name: "new", RolloutPercentage: 100}
	if err := repo.Create(ctxA, created); err != nil {
		t.Fatalf("租户 A 创建开关失败: %v", err)
	}
	if created.TenantID != ffTenantA {
		t.Fatalf("Create 填充 tenant_id=%d，期望 %d", created.TenantID, ffTenantA)
	}

	// 同租户重复 key 必须被复合唯一键挡住（服务层的存在性预检之外还有库这一道）。
	dup := &model.FeatureFlag{Key: "ff_created", Name: "dup", RolloutPercentage: 100}
	if err := repo.Create(ctxA, dup); err == nil {
		t.Fatal("同租户重复 key 创建成功 ⇒ 唯一键没生效")
	}

	// 另一个租户建同名 key 必须成功——这正是 (tenant_id, key) 的意义。
	other := &model.FeatureFlag{Key: "ff_created", Name: "other", RolloutPercentage: 100}
	if err := repo.Create(tenant.WithTenantID(context.Background(), ffTenantB), other); err != nil {
		t.Fatalf("租户 B 建同名 key 失败（唯一键仍是单列 key）: %v", err)
	}
}

func TestFeatureFlag_CreatePersistsZeroRollout(t *testing.T) {
	db := ffSetupDB(t, tenant.ModeOn)
	repo := NewFeatureFlagRepository(db)
	ctxA := tenant.WithTenantID(context.Background(), ffTenantA)

	// rollout_percentage=0 是合法值（谁都不放量）。model 上的 `default:100` 只是
	// 列默认值，不能让显式 0 在插入时被 GORM 当成"未设置"而改写成 100。
	created := &model.FeatureFlag{Key: "ff_zero_rollout", Name: "zero", RolloutPercentage: 0}
	if err := repo.Create(ctxA, created); err != nil {
		t.Fatalf("创建 rollout=0 的开关失败: %v", err)
	}
	got, err := repo.GetByID(ctxA, created.ID)
	if err != nil {
		t.Fatalf("回读开关失败: %v", err)
	}
	if got.RolloutPercentage != 0 {
		t.Fatalf("显式 rollout=0 落库变成 %d ⇒ 零值被列默认值覆盖", got.RolloutPercentage)
	}
}

func TestFeatureFlag_CreateUnboundCtxIsRejected(t *testing.T) {
	db := ffSetupDB(t, tenant.ModeOn)
	repo := NewFeatureFlagRepository(db)

	err := repo.Create(context.Background(), &model.FeatureFlag{Key: "ff_unbound", Name: "x", RolloutPercentage: 100})
	if err == nil {
		t.Fatal("on 模式无租户 ctx 创建开关成功 ⇒ 行会落默认租户")
	}
}
