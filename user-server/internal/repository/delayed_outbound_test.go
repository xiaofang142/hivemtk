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

// TestDelayedOutbound_CountStuckSending 覆盖悬挂观测：只按 status=sending 且
// send_at 早于阈值计数，绝不改写任何行（回收语义未拍板前必须保持纯只读）。
func TestDelayedOutbound_CountStuckSending(t *testing.T) {
	database := setupDelayedOutboundTestDB(t)
	repo := NewDelayedOutboundRepository(database)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	threshold := now.Add(-time.Hour)

	stuck := newPendingReply(t, repo, "sending", threshold.Add(-time.Minute)) // 悬挂：应计数
	fresh := newPendingReply(t, repo, "sending", now)                         // 正常在途：不计数
	pend := newPendingReply(t, repo, "pending", threshold.Add(-time.Hour))    // 非 sending：不计数
	done := newPendingReply(t, repo, "sent", threshold.Add(-time.Hour))       // 终态：不计数

	n, err := repo.CountStuckSending(ctx, threshold)
	if err != nil {
		t.Fatalf("CountStuckSending failed: %v", err)
	}
	if n != 1 {
		t.Errorf("CountStuckSending = %d, want 1", n)
	}

	// 边界：恰等于 threshold 不计数（与 ExpireStale 一致的严格 <）
	if n, err := repo.CountStuckSending(ctx, stuck.SendAt); err != nil || n != 0 {
		t.Errorf("恰等边界 n=%d err=%v, want 0 悬挂", n, err)
	}

	// 空集：阈值放到极早应得 0
	if n, err := repo.CountStuckSending(ctx, now.Add(-1000*time.Hour)); err != nil || n != 0 {
		t.Errorf("空集 n=%d err=%v, want 0", n, err)
	}

	// 只读不变量：调用后所有行状态原样
	for id, want := range map[uint]string{stuck.ID: "sending", fresh.ID: "sending", pend.ID: "pending", done.ID: "sent"} {
		var rec model.DelayedOutboundReply
		if err := database.First(&rec, id).Error; err != nil {
			t.Fatalf("load reply %d: %v", id, err)
		}
		if rec.Status != want {
			t.Errorf("观测后 reply %d status = %q, want %q（必须只读）", id, rec.Status, want)
		}
	}
}
