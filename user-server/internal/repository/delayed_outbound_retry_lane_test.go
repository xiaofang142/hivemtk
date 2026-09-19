package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestDelayedOutboundRetryLane 投递重试通道的仓储语义：
// 到期抢占 → 失败原地改排（attempts+1 / 新 send_at / 记 last_error）→ 成功或次数用尽收口，
// 且「会话是否还有待发」能被上层查到（recheck 用它避免「重试 + 重新生成」双投）。
func TestDelayedOutboundRetryLane(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
	repo := NewDelayedOutboundRepository(db)
	ctx := context.Background()

	rec := &model.DelayedOutboundReply{
		Platform: "telegram", AccountID: "5", ConversationID: "conv-retry-1",
		SenderID: "cust1", Content: "回复内容", SendAt: time.Now().Add(time.Minute),
		Status: model.DelayedStatusPending, Kind: model.DelayedKindSendRetry,
	}
	if err := repo.CreatePending(ctx, rec); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if rec.ID == 0 {
		t.Fatal("入队未回写主键")
	}

	pending, err := repo.HasPendingByConversation(ctx, "conv-retry-1")
	if err != nil {
		t.Fatalf("has pending: %v", err)
	}
	if !pending {
		t.Fatal("待发重试必须可查，否则 recheck 会另生成一份回复造成双投")
	}

	// 未到期的抢占应为空，避免到点前被顺带投出去。
	picked, err := repo.PickDueForUpdate(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("pick early: %v", err)
	}
	if len(picked) != 0 {
		t.Fatalf("未到期不得被抢占，实得 %d 条", len(picked))
	}

	if err := repo.ScheduleRetry(ctx, rec.ID, time.Now().Add(60*time.Second), "tg send exhausted 3 retries"); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	var after model.DelayedOutboundReply
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.Status != model.DelayedStatusPending {
		t.Fatalf("改排后应回到 pending 等待下一轮，实得 %q", after.Status)
	}
	if after.Attempts != 1 {
		t.Fatalf("attempts 应递增到 1，实得 %d", after.Attempts)
	}
	if after.LastError != "tg send exhausted 3 retries" {
		t.Fatalf("last_error 未记录: %q", after.LastError)
	}

	// 收口：终态写 superseded 且不再被 HasPendingByConversation 视为待发。
	if err := repo.FinishReplay(ctx, rec.ID, model.DelayedStatusSuperseded, time.Now(), "会话已被回复"); err != nil {
		t.Fatalf("finish: %v", err)
	}
	pending, err = repo.HasPendingByConversation(ctx, "conv-retry-1")
	if err != nil {
		t.Fatalf("has pending after finish: %v", err)
	}
	if pending {
		t.Fatal("终态行仍是待发，recheck 会被永久压制")
	}

	// sending（崩溃遗留）不得算待发：否则进程反复重启时 recheck 一直静默。
	if err := db.Model(&model.DelayedOutboundReply{}).Where("id = ?", rec.ID).
		Update("status", model.DelayedStatusSending).Error; err != nil {
		t.Fatalf("force sending: %v", err)
	}
	pending, _ = repo.HasPendingByConversation(ctx, "conv-retry-1")
	if pending {
		t.Fatal("sending 悬挂行不应压制 recheck（该轮已无人投递）")
	}
}
