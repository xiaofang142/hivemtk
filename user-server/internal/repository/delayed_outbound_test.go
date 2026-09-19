package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupDelayedOutboundTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t, &model.DelayedOutboundReply{})
}

func newPendingReply(t *testing.T, repo *DelayedOutboundRepository, status string, sendAt time.Time) *model.DelayedOutboundReply {
	t.Helper()
	rec := &model.DelayedOutboundReply{
		Platform:       "tg",
		AccountID:      "acc-1",
		ConversationID: "conv-1",
		SenderID:       "sender-1",
		Content:        "hello",
		SendAt:         sendAt,
		Status:         status,
	}
	if err := repo.CreatePending(context.Background(), rec); err != nil {
		t.Fatalf("CreatePending failed: %v", err)
	}
	return rec
}

func TestDelayedOutbound_ExpireStale(t *testing.T) {
	database := setupDelayedOutboundTestDB(t)
	repo := NewDelayedOutboundRepository(database)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	cutoff := now.Add(-24 * time.Hour)

	fresh := newPendingReply(t, repo, "pending", now)                    // 未到期：保持 pending
	stale := newPendingReply(t, repo, "pending", cutoff.Add(-time.Hour)) // 超 24h：判 expired
	boundary := newPendingReply(t, repo, "pending", cutoff)              // 恰好等于 cutoff：严格 < 不动
	sent := newPendingReply(t, repo, "sent", cutoff.Add(-time.Hour))     // 非 pending：不受影响

	n, err := repo.ExpireStale(ctx, cutoff)
	if err != nil {
		t.Fatalf("ExpireStale failed: %v", err)
	}
	if n != 1 {
		t.Errorf("ExpireStale RowsAffected = %d, want 1", n)
	}

	assertStatus := func(id uint, want string) {
		t.Helper()
		var rec model.DelayedOutboundReply
		if err := database.First(&rec, id).Error; err != nil {
			t.Fatalf("load reply %d: %v", id, err)
		}
		if rec.Status != want {
			t.Errorf("reply %d status = %q, want %q", id, rec.Status, want)
		}
	}
	assertStatus(fresh.ID, "pending")
	assertStatus(stale.ID, "expired")
	assertStatus(boundary.ID, "pending")
	assertStatus(sent.ID, "sent")

	// 幂等：再跑一轮不应重复影响已 expired 的行
	n2, err := repo.ExpireStale(ctx, cutoff)
	if err != nil {
		t.Fatalf("second ExpireStale failed: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second ExpireStale RowsAffected = %d, want 0", n2)
	}
}
