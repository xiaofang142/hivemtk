package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestInferenceDeliveryRepositoryLeaseAndTenant(t *testing.T) {
	db := testutil.NewTestDB(t, &model.InferenceDelivery{})
	repo := NewInferenceDeliveryRepository(db)
	ctx := tenant.WithTenantID(context.Background(), 773)
	row := &model.InferenceDelivery{TenantID: 773, Key: "lease-key", Fingerprint: "immutable", IntentJSON: "{}", Kind: "reply", State: string(delivery.Intended)}
	if _, err := repo.Put(ctx, row); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, claimed, err := repo.Claim(ctx, row.Key, "old-worker", now, now.Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("首次未认领：%v", err)
	}
	if _, claimed, err := repo.Claim(ctx, row.Key, "other-worker", now, now.Add(time.Minute)); err != nil || claimed {
		t.Fatal("有效租约被抢占")
	}
	// 显式推进受控时钟，用同一 PG 行验证重启后的新 token 排斥旧 worker。
	later := now.Add(2 * time.Minute)
	if _, claimed, err := repo.Claim(ctx, row.Key, "new-worker", later, later.Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("到期租约未恢复：%v", err)
	}
	if !errors.Is(repo.MarkSend(ctx, row.Key, "old-worker"), delivery.ErrLease) {
		t.Fatal("旧租约仍可预留发送")
	}
	if !errors.Is(repo.Finish(ctx, row.Key, "old-worker", string(delivery.Confirmed), "", "forged", &now, &now), delivery.ErrLease) {
		t.Fatal("旧租约仍可写确认")
	}
	other := tenant.WithTenantID(context.Background(), 774)
	if _, err := repo.Get(other, row.Key); !errors.Is(err, delivery.ErrNotFound) {
		t.Fatal("跨租户读取")
	}
	if !errors.Is(repo.MarkSend(other, row.Key, "new-worker"), delivery.ErrLease) {
		t.Fatal("跨租户修改预留")
	}
	if !errors.Is(repo.Finish(other, row.Key, "new-worker", string(delivery.Confirmed), "", "forged", &now, &now), delivery.ErrLease) {
		t.Fatal("跨租户修改确认")
	}
	if _, err := repo.Put(other, row); !errors.Is(err, delivery.ErrIdentity) {
		t.Fatal("写入不属于上下文的租户")
	}
	if _, err := repo.Get(context.Background(), row.Key); !errors.Is(err, delivery.ErrTenant) {
		t.Fatal("无租户读取放行")
	}
	if !errors.Is(repo.MarkSend(context.Background(), row.Key, "new-worker"), delivery.ErrTenant) {
		t.Fatal("无租户修改放行")
	}
	if err := repo.MarkSend(ctx, row.Key, "new-worker"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, row.Key, "new-worker", string(delivery.Confirmed), "", "actual-receipt", &now, &now); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, row.Key)
	if err != nil || stored.SendAttempts != 1 || stored.ReceiptID != "actual-receipt" || stored.State != string(delivery.Confirmed) {
		t.Fatalf("持久计数或确认错误：%+v %v", stored, err)
	}
	if _, claimed, err := repo.Claim(ctx, row.Key, "terminal-worker", later, later.Add(time.Minute)); err != nil || claimed {
		t.Fatal("确认终态仍被认领")
	}
}
