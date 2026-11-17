package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestImportCredentialsRepository_ScopeAndUpdates(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SystemConfigKV{})
	repo := NewImportCredentialsRepositoryWithDB(database)
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx18 := tenant.OnlyTenant(context.Background(), 18)
	ctx19 := tenant.OnlyTenant(context.Background(), 19)
	const key = "knowledge.import_credentials"
	if err := repo.Upsert(ctx18, key, "synthetic-18"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx19, key, "synthetic-19"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx18, key, "synthetic-18-updated"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "原租户更新", ctx: ctx18, want: "synthetic-18-updated"},
		{name: "另一租户未被更新", ctx: ctx19, want: "synthetic-19"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.Get(tc.ctx, key)
			if err != nil || got != tc.want {
				t.Fatalf("凭证仓储必须按租户读写：得到%q，错误%v", got, err)
			}
		})
	}
	if got, err := repo.Get(ctx18, "missing-synthetic-key"); err != nil || got != "" {
		t.Fatalf("尚未配置应返回空值，而不是别的租户配置：%q，%v", got, err)
	}
	canceled, cancel := context.WithCancel(ctx18)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺上下文"},
		{name: "缺SaaS租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(ctx18)},
		{name: "已取消", ctx: canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := repo.Get(tc.ctx, key); err == nil {
				t.Fatal("仓储读取必须拒绝未确认上下文")
			}
			if err := repo.Upsert(tc.ctx, key, "must-not-write"); err == nil {
				t.Fatal("仓储写入必须拒绝未确认上下文")
			}
		})
	}
	tenant.SetMode(tenant.ModeOff)
	if err := repo.Upsert(context.Background(), key, "synthetic-default"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(context.Background(), key); err != nil || got != "synthetic-default" {
		t.Fatal("单租户模式无绑定只使用明确默认租户")
	}
	if got, err := repo.Get(ctx18, key); err != nil || got != "synthetic-18-updated" {
		t.Fatal("单租户模式也不能将默认配置覆盖显式租户")
	}
}

func TestImportCredentialsRepository_FailuresRemainVisible(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SystemConfigKV{})
	repo := NewImportCredentialsRepositoryWithDB(database)
	ctx := tenant.OnlyTenant(context.Background(), 18)
	injected := errors.New("synthetic credential write failure")
	callback := "test:knowledge-import-write-fault"
	if err := database.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "system_config_kv" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(callback) })
	if err := repo.Upsert(ctx, "knowledge.import_credentials", "must-not-write"); !errors.Is(err, injected) {
		t.Fatalf("幂等建表重试不能吞掉实际写入错误：%v", err)
	}
	var count int64
	if err := database.WithContext(ctx).Model(&model.SystemConfigKV{}).Where("tenant_id = ?", 18).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("失败的凭证写入不能留下成功记录")
	}
	for _, missing := range []*ImportCredentialsRepository{nil, NewImportCredentialsRepositoryWithDB(nil)} {
		if _, err := missing.Get(ctx, "knowledge.import_credentials"); !errors.Is(err, gorm.ErrInvalidDB) {
			t.Fatal("缺失仓储依赖必须返回数据库未就绪错误")
		}
		if err := missing.Upsert(ctx, "knowledge.import_credentials", "synthetic"); !errors.Is(err, gorm.ErrInvalidDB) {
			t.Fatal("缺失写入依赖必须返回数据库未就绪错误")
		}
	}
}
