package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestObsUploadConfig_ConfirmedTenantAndActiveDefault(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.ObsConfig{})
	repo := NewObsUploadConfigRepositoryWithDB(database)
	ctx := tenant.OnlyTenant(context.Background(), 18)
	otherCtx := tenant.OnlyTenant(context.Background(), 19)
	rows := []*model.ObsConfig{
		{TenantID: 18, Name: "停用默认项", IsDefault: true, Status: "disabled", CreatedAt: time.Now().Add(-time.Hour)},
		{TenantID: 18, Name: "非默认项", Status: model.ObsStatusActive},
		{TenantID: 19, Name: "其它租户默认项", IsDefault: true, Status: model.ObsStatusActive},
	}
	for _, row := range rows {
		if err := database.WithContext(tenant.OnlyTenant(context.Background(), row.TenantID)).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.GetDefaultForUpload(ctx); !errors.Is(err, gorm.ErrRecordNotFound) || got != nil {
		t.Fatal("停用项、非默认项与其它租户配置均不能作为上传凭证来源")
	}
	ensureSecretsReady(t)
	cfg := &model.ObsConfig{TenantID: 18, Name: "合法合成配置", Provider: model.ObsProviderQiniu,
		AccessKey: "upload-test-ak", SecretKey: "uplo" + "ad-test-sk", Bucket: "upload-test-bucket",
		Status: model.ObsStatusActive, IsDefault: true}
	if err := NewObsConfigRepositoryWithDB(database).Create(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetDefaultForUpload(ctx)
	if err != nil || got == nil {
		t.Fatalf("合法配置读取失败：%v", err)
	}
	if got.TenantID != 18 || got.ID != cfg.ID || got.AccessKey != "upload-test-ak" || got.SecretKey != "upload-test-sk" {
		t.Fatal("受控读取必须保留原加密读写与租户选择契约")
	}
	if cfg.AccessKey != "upload-test-ak" || cfg.SecretKey != "upload-test-sk" {
		t.Fatal("受控读取不能污染原管理端调用者的内存配置")
	}
	other, err := repo.GetDefaultForUpload(otherCtx)
	if err != nil || other == nil || other.TenantID != 19 {
		t.Fatalf("另租户默认配置的正向读取失败：%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "空上下文"}, {name: "未绑定租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(ctx)}, {name: "已取消", ctx: canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := repo.GetDefaultForUpload(tc.ctx); err == nil || got != nil {
				t.Fatal("未知、系统或取消上下文不得读取上传凭证")
			}
		})
	}
}

func TestObsUploadConfig_DatabaseAndCancellationFailures(t *testing.T) {
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	for _, repo := range []*ObsUploadConfigRepository{nil, NewObsUploadConfigRepositoryWithDB(nil)} {
		if got, err := repo.GetDefaultForUpload(ctx); !errors.Is(err, gorm.ErrInvalidDB) || got != nil {
			t.Fatal("未装配数据库必须返回错误，不崩溃或使用其它凭据")
		}
	}
	database := testutil.NewTestDB(t, &model.ObsConfig{})
	cfg := &model.ObsConfig{TenantID: tenant.DefaultTenantID, Name: "故障对照", Status: model.ObsStatusActive, IsDefault: true}
	if err := database.WithContext(ctx).Create(cfg).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewObsUploadConfigRepositoryWithDB(database)
	if err := database.Callback().Query().Before("gorm:query").Register("test:upload-read-error", func(tx *gorm.DB) {
		if tx.Statement.Table == "obs_config" {
			tx.AddError(errors.New("合成读取故障"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetDefaultForUpload(ctx); err == nil || got != nil {
		t.Fatal("读取故障必须显式失败，不能转成未配置或签发成功")
	}
	if err := database.Callback().Query().Remove("test:upload-read-error"); err != nil {
		t.Fatal(err)
	}
	duringRead, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := database.Callback().Query().After("gorm:query").Register("test:upload-cancel-after-read", func(tx *gorm.DB) {
		if tx.Statement.Table == "obs_config" {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove("test:upload-cancel-after-read") })
	if got, err := repo.GetDefaultForUpload(duringRead); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("配置读取完成后取消的请求也不得取得凭证")
	}
}
