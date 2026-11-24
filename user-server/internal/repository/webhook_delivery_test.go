package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestWebhookDeliveryRepository_ClaimReplayCAS(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebhookDelivery{})
	row := &model.WebhookDelivery{TenantID: 1, SubscriptionID: 1, Event: "hook.test", Payload: `{}`, Status: "dead_letter", Attempts: 3}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	repo := NewWebhookDeliveryRepositoryWithDB(database)
	now := time.Now()
	claim, err := repo.ClaimReplay(context.Background(), row.ID, 1, 8, now, 10*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("首次 claim 应成功: claim=%+v err=%v", claim, err)
	}
	if claim.Attempts != 4 || claim.Token() == "" {
		t.Fatalf("claim 应携带版本 token 且 attempts 仅 +1: %+v", claim)
	}
	second, err := repo.ClaimReplay(context.Background(), row.ID, 1, 8, now, 10*time.Second)
	if err != nil || second != nil {
		t.Fatalf("lease 内第二次 claim 必须失败: claim=%+v err=%v", second, err)
	}
	completed, err := repo.CompleteReplay(context.Background(), *claim, "success", 200, nil, now.Add(time.Second))
	if err != nil || !completed {
		t.Fatalf("持权 worker 应完成成功: completed=%v err=%v", completed, err)
	}
	completed, err = repo.CompleteReplay(context.Background(), *claim, "dead_letter", 500, errors.New("stale"), now.Add(2*time.Second))
	if err != nil || completed {
		t.Fatalf("同一 token 不得二次完成: completed=%v err=%v", completed, err)
	}
	var got model.WebhookDelivery
	if err := database.First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "success" || got.Attempts != 4 || got.DeliveredAt == nil {
		t.Fatalf("完成状态不符: %+v", got)
	}
}

func TestWebhookDeliveryRepository_StaleClaimCannotCompleteNewerClaim(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebhookDelivery{})
	row := &model.WebhookDelivery{TenantID: 1, SubscriptionID: 1, Event: "hook.test", Payload: `{}`, Status: "dead_letter", Attempts: 3, LastError: "old"}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	repo := NewWebhookDeliveryRepositoryWithDB(database)
	now := time.Now()
	oldClaim, err := repo.ClaimReplay(context.Background(), row.ID, 1, 8, now, 10*time.Second)
	if err != nil || oldClaim == nil {
		t.Fatalf("旧 claim 应成功: %+v err=%v", oldClaim, err)
	}
	newVersion := now.Add(20 * time.Second)
	if err := database.Model(&model.WebhookDelivery{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status": "replaying", "attempts": 5, "updated_at": newVersion, "last_error": "new-claim",
	}).Error; err != nil {
		t.Fatalf("simulate newer claim: %v", err)
	}
	completed, err := repo.CompleteReplay(context.Background(), *oldClaim, "dead_letter", 500, errors.New("stale worker"), now.Add(21*time.Second))
	if err != nil || completed {
		t.Fatalf("旧 token 不得覆盖新 claim: completed=%v err=%v", completed, err)
	}
	var got model.WebhookDelivery
	if err := database.First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "replaying" || got.Attempts != 5 || got.LastError != "new-claim" || !got.UpdatedAt.Equal(newVersion) {
		t.Fatalf("新 claim 被旧 worker 覆盖: %+v", got)
	}
}

func TestWebhookDeliveryRepository_StaleReplayRecoveryAndCap(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebhookDelivery{})
	now := time.Now()
	rows := []*model.WebhookDelivery{
		{TenantID: 1, SubscriptionID: 1, Event: "stale", Payload: `{}`, Status: "replaying", Attempts: 3, UpdatedAt: now.Add(-11 * time.Second)},
		{TenantID: 1, SubscriptionID: 1, Event: "fresh", Payload: `{}`, Status: "replaying", Attempts: 3, UpdatedAt: now.Add(-9 * time.Second)},
		{TenantID: 1, SubscriptionID: 1, Event: "capped", Payload: `{}`, Status: "dead_letter", Attempts: 8, UpdatedAt: now.Add(-time.Hour)},
	}
	for _, row := range rows {
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("seed delivery %s: %v", row.Event, err)
		}
	}
	repo := NewWebhookDeliveryRepositoryWithDB(database)
	candidates, err := repo.ListReplayCandidates(context.Background(), 1, 8, now.Add(-10*time.Second), 10)
	if err != nil || len(candidates) != 1 || candidates[0].Event != "stale" {
		t.Fatalf("候选应仅含过期 replaying: rows=%+v err=%v", candidates, err)
	}
	claim, err := repo.ClaimReplay(context.Background(), candidates[0].ID, 1, 8, now, 10*time.Second)
	if err != nil || claim == nil || claim.Attempts != 4 {
		t.Fatalf("过期 replaying 应可恢复且 attempts +1: claim=%+v err=%v", claim, err)
	}
}
