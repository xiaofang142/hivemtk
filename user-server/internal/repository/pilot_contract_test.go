package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
)

// nil DB 与缺租户必须区分；即使 off 模式也不隐式使用默认租户。
func TestPilotContractRepository_FailClosed(t *testing.T) {
	repo := NewPilotContractRepository(nil)
	if _, err := repo.Load(context.Background()); !errors.Is(err, ErrKVTenantContextMissing) {
		t.Fatalf("无租户未拒绝: %v", err)
	}
	if err := repo.Save(context.Background(), "", "fake"); !errors.Is(err, ErrKVTenantContextMissing) {
		t.Fatalf("无租户写未拒绝: %v", err)
	}
	ctx := tenant.OnlyTenant(context.Background(), 42)
	if _, err := repo.Load(ctx); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Fatalf("nil DB 未拒绝: %v", err)
	}
	if err := repo.Save(ctx, "", "fake"); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Fatalf("nil DB 写未拒绝: %v", err)
	}
}

// 复用测试专用 Postgres 验证真实 CAS 与物理租户键；不触碰业务库/平台。
func TestPilotContractRepository_TenantCAS(t *testing.T) {
	database := kvSetupDB(t)
	repo := NewPilotContractRepository(database)
	a := tenant.OnlyTenant(context.Background(), 42)
	b := tenant.OnlyTenant(context.Background(), 43)
	if err := repo.Save(a, "", "fake-a1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(a, "", "fake-duplicate"); !errors.Is(err, ErrPilotContractConflict) {
		t.Fatalf("首次写竞争未拒绝: %v", err)
	}
	if err := repo.Save(a, "fake-a1", "fake-a2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(a, "fake-a1", "fake-stale"); !errors.Is(err, ErrPilotContractConflict) {
		t.Fatalf("旧修订覆盖: %v", err)
	}
	if err := repo.Save(b, "", "fake-b1"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{a, "fake-a2"}, {b, "fake-b1"}} {
		got, err := repo.Load(tc.ctx)
		if err != nil || got != tc.want {
			t.Fatalf("租户值=%q %v，期望=%q", got, err, tc.want)
		}
	}
}
