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

func seedBridgeAccountTenantRow(t *testing.T, db *gorm.DB, tenantID uint, channel, accountID string) {
	t.Helper()
	row := &model.BridgeAccount{TenantID: tenantID, Channel: channel, AccountID: accountID, Status: "offline"}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("写入 bridge_accounts 夹具失败: %v", err)
	}
}

func TestBridgeAccountTenantRepository_ResolveTenantIDMatrix(t *testing.T) {
	t.Run("no-row", func(t *testing.T) {
		db := testutil.NewTestDB(t, &model.BridgeAccount{})
		repo := NewBridgeAccountTenantRepositoryWithDB(db)
		got, err := repo.ResolveTenantID(context.Background(), "douyin", "missing")
		if err != nil || got != 0 {
			t.Fatalf("未命中应返回 (0,nil): got=%d err=%v", got, err)
		}
	})

	t.Run("tenant-zero", func(t *testing.T) {
		db := testutil.NewTestDB(t, &model.BridgeAccount{})
		seedBridgeAccountTenantRow(t, db, 7, "douyin", "zero-tenant")
		if err := db.WithContext(tenant.WithSystemScope(context.Background())).Model(&model.BridgeAccount{}).
			Where("channel = ? AND account_id = ?", "douyin", "zero-tenant").Update("tenant_id", 0).Error; err != nil {
			t.Fatalf("更新 tenant_id=0 失败: %v", err)
		}
		repo := NewBridgeAccountTenantRepositoryWithDB(db)
		got, err := repo.ResolveTenantID(context.Background(), "douyin", "zero-tenant")
		if err != nil || got != 0 {
			t.Fatalf("tenant_id=0 应按无归属返回 (0,nil): got=%d err=%v", got, err)
		}
	})

	t.Run("one-tenant", func(t *testing.T) {
		db := testutil.NewTestDB(t, &model.BridgeAccount{})
		seedBridgeAccountTenantRow(t, db, 7, "douyin", "one-tenant")
		repo := NewBridgeAccountTenantRepositoryWithDB(db)
		got, err := repo.ResolveTenantID(context.Background(), "douyin", "one-tenant")
		if err != nil || got != 7 {
			t.Fatalf("单租户应返回 7: got=%d err=%v", got, err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		db := testutil.NewTestDB(t, &model.BridgeAccount{})
		if err := db.Migrator().DropIndex(&model.BridgeAccount{}, "uk_bridge_ch_acc"); err != nil {
			t.Fatalf("删除唯一索引以构造历史歧义夹具失败: %v", err)
		}
		seedBridgeAccountTenantRow(t, db, 7, "douyin", "ambiguous")
		seedBridgeAccountTenantRow(t, db, 9, "douyin", "ambiguous")
		repo := NewBridgeAccountTenantRepositoryWithDB(db)
		got, err := repo.ResolveTenantID(context.Background(), "douyin", "ambiguous")
		if got != 0 || !errors.Is(err, tenant.ErrAmbiguousTenantKey) {
			t.Fatalf("多租户必须返回歧义错误: got=%d err=%v", got, err)
		}
	})

	t.Run("query-error", func(t *testing.T) {
		db := testutil.NewTestDB(t, &model.BridgeAccount{})
		queryErr := errors.New("bridge tenant query failed")
		const callback = "test:bridge-account-tenant-query-error"
		if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "bridge_accounts" {
				tx.AddError(queryErr)
			}
		}); err != nil {
			t.Fatalf("注册查询故障失败: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
		repo := NewBridgeAccountTenantRepositoryWithDB(db)
		got, err := repo.ResolveTenantID(context.Background(), "douyin", "query-error")
		if got != 0 || !errors.Is(err, queryErr) {
			t.Fatalf("运行错误必须原样上抛: got=%d err=%v", got, err)
		}
	})
}
