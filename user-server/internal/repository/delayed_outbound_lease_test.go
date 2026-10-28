// delayed_outbound_lease_test.go D03（sending 悬挂回收）仓储语义：
// 租约签发、栅栏回写、unknown 收敛与人工核对出口。
// 验收口径见 docs/assessment/functionality-2026-10-27/开发顺序与验收.md D03：
//   - 旧 worker 回写被 token 拒；
//   - 新 worker 不二次盲发（可能已发的行转 unknown，不自动回 pending）；
//   - 每条记录最终有 pending/sent/superseded/unknown 待办，不永久无主悬挂。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestDelayedOutboundLease_ClaimAndFence(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
	repo := NewDelayedOutboundRepository(db)
	ctx := context.Background()
	now := time.Now()

	rec := &model.DelayedOutboundReply{
		Platform: "telegram", AccountID: "5", ConversationID: "conv-lease-1",
		SenderID: "cust1", Content: "回复内容", SendAt: now.Add(-time.Minute),
		Status: model.DelayedStatusPending,
	}
	if err := repo.CreatePending(ctx, rec); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// 抢占必须签发 token + 租约
	picked, err := repo.PickDueForUpdate(ctx, now, 10)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(picked) != 1 {
		t.Fatalf("expect 1 picked, got %d", len(picked))
	}
	token := picked[0].ClaimToken
	if token == "" {
		t.Fatal("抢占未签发 claim_token（旧 worker 回写将无栅栏可拒）")
	}
	if picked[0].LeaseUntil == nil || !picked[0].LeaseUntil.After(now) {
		t.Fatalf("lease_until = %v, want future", picked[0].LeaseUntil)
	}

	// 栅栏：伪造另一个 worker 的 token 回写必须 0 行拒绝
	if err := repo.MarkSent(ctx, rec.ID, "stale-token", now); err == nil {
		t.Fatal("旧 token 回写 sent 应被拒绝（EnsureAffected 0 行报错），实际成功")
	}
	var after model.DelayedOutboundReply
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.Status != model.DelayedStatusSending {
		t.Fatalf("旧 token 回写后 status = %q, want sending（状态不得被旧 worker 改写）", after.Status)
	}

	// 持有正确 token 的回写成功
	if err := repo.MarkSent(ctx, rec.ID, token, now); err != nil {
		t.Fatalf("正确 token 回写 MarkSent: %v", err)
	}
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.Status != model.DelayedStatusSent {
		t.Fatalf("正确 token 回写后 status = %q, want sent", after.Status)
	}
}

func TestDelayedOutboundLease_ScheduleRetryClearsLease(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
	repo := NewDelayedOutboundRepository(db)
	ctx := context.Background()
	now := time.Now()

	rec := &model.DelayedOutboundReply{
		Platform: "telegram", ConversationID: "conv-lease-2",
		Content: "r", SendAt: now.Add(-time.Minute), Status: model.DelayedStatusPending,
	}
	if err := repo.CreatePending(ctx, rec); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	picked, err := repo.PickDueForUpdate(ctx, now, 10)
	if err != nil || len(picked) != 1 {
		t.Fatalf("pick: %v (%d)", err, len(picked))
	}
	token := picked[0].ClaimToken

	// 旧 token 的改排被拒
	if err := repo.ScheduleRetry(ctx, rec.ID, "stale", now.Add(time.Minute), "e"); err == nil {
		t.Fatal("旧 token ScheduleRetry 应被拒绝")
	}
	// 正确 token 改排成功且清租约
	if err := repo.ScheduleRetry(ctx, rec.ID, token, now.Add(time.Minute), "tg timeout"); err != nil {
		t.Fatalf("ScheduleRetry: %v", err)
	}
	var after model.DelayedOutboundReply
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.Status != model.DelayedStatusPending || after.ClaimToken != "" || after.LeaseUntil != nil {
		t.Fatalf("改排后应回 pending 且清租约，实得 status=%q token=%q lease=%v",
			after.Status, after.ClaimToken, after.LeaseUntil)
	}
	if after.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", after.Attempts)
	}
}

func TestDelayedOutboundLease_RecoverExpiredToUnknown(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
	repo := NewDelayedOutboundRepository(db)
	ctx := context.Background()
	now := time.Now()

	// 两条租约过期的 sending 行：一条 token 过期（模拟崩溃），一条无 token（legacy）
	crashed := &model.DelayedOutboundReply{
		Platform: "telegram", ConversationID: "conv-lease-3",
		Content: "r", SendAt: now.Add(-time.Hour), Status: model.DelayedStatusSending,
		ClaimToken: "expired-token",
	}
	lease := now.Add(-time.Minute)
	crashed.LeaseUntil = &lease
	if err := repo.CreatePending(ctx, crashed); err != nil {
		t.Fatalf("seed crashed: %v", err)
	}
	legacy := &model.DelayedOutboundReply{
		Platform: "telegram", ConversationID: "conv-lease-4",
		Content: "r", SendAt: now.Add(-2 * time.Hour), Status: model.DelayedStatusSending,
	}
	if err := repo.CreatePending(ctx, legacy); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	// 活跃租约的 sending 行不受回收影响
	active := &model.DelayedOutboundReply{
		Platform: "telegram", ConversationID: "conv-lease-5",
		Content: "r", SendAt: now.Add(-time.Minute), Status: model.DelayedStatusSending,
		ClaimToken: "live-token",
	}
	activeLease := now.Add(time.Minute)
	active.LeaseUntil = &activeLease
	if err := repo.CreatePending(ctx, active); err != nil {
		t.Fatalf("seed active: %v", err)
	}

	expired, err := repo.ListExpiredLeaseSending(ctx, now, 100)
	if err != nil {
		t.Fatalf("ListExpiredLeaseSending: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != crashed.ID {
		t.Fatalf("过期租约行应只有 crashed 一条，实得 %d", len(expired))
	}
	legacyRows, err := repo.ListLegacyStuckSending(ctx, now.Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("ListLegacyStuckSending: %v", err)
	}
	if len(legacyRows) != 1 || legacyRows[0].ID != legacy.ID {
		t.Fatalf("legacy 悬挂行应只有 legacy 一条，实得 %d", len(legacyRows))
	}

	// 回收：转 unknown（绝不自动回 pending 盲发）
	if err := repo.MarkSendingUnknown(ctx, crashed.ID, crashed.ClaimToken, "租约过期"); err != nil {
		t.Fatalf("MarkSendingUnknown: %v", err)
	}
	if err := repo.MarkSendingUnknown(ctx, legacy.ID, "", "legacy"); err != nil {
		t.Fatalf("MarkSendingUnknown legacy: %v", err)
	}
	after := readReply(t, db, crashed.ID)
	if after.Status != model.DelayedStatusUnknown {
		t.Fatalf("租约过期行应转 unknown，实得 %q", after.Status)
	}
	after = readReply(t, db, active.ID)
	if after.Status != model.DelayedStatusSending {
		t.Fatalf("活跃租约行不得被回收，实得 %q", after.Status)
	}

	// 人工核对出口：unknown → pending（确认没发出，重新投递）
	if err := repo.ResolveUnknown(ctx, crashed.ID, model.DelayedStatusPending, now, "渠道后台确认未送达"); err != nil {
		t.Fatalf("ResolveUnknown → pending: %v", err)
	}
	after = readReply(t, db, crashed.ID)
	if after.Status != model.DelayedStatusPending {
		t.Fatalf("核对后应回 pending，实得 %q", after.Status)
	}

	// legacy 行人工确认已送达
	if err := repo.ResolveUnknown(ctx, legacy.ID, model.DelayedStatusSent, now, "渠道后台显示已送达"); err != nil {
		t.Fatalf("ResolveUnknown → sent: %v", err)
	}
	after = readReply(t, db, legacy.ID)
	if after.Status != model.DelayedStatusSent || after.SentAt == nil {
		t.Fatalf("核对后应转 sent 且补记时刻，实得 status=%q sent_at=%v", after.Status, after.SentAt)
	}

	// 非法出口被拒
	if err := repo.ResolveUnknown(ctx, crashed.ID, "deleted", now, ""); err == nil {
		t.Fatal("不支持的核对出口应报错")
	}
}

// readReply 按主键读回一行。每次用全新 dest：GORM 的 First(&dest, id) 会把
// dest 里已存在的主键一并并进 WHERE（id=? AND id=?），复用变量必然 not found。
func readReply(t *testing.T, db *gorm.DB, id uint) model.DelayedOutboundReply {
	t.Helper()
	var rec model.DelayedOutboundReply
	if err := db.First(&rec, id).Error; err != nil {
		t.Fatalf("read back reply %d: %v", id, err)
	}
	return rec
}
