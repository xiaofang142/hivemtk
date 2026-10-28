// webhook_outbound_lease_recover_test.go D03 服务层回收语义：
// 派发循环里的 recoverStuckDelayedOutbound 把 sending 悬挂行收敛为
// superseded（会话已被回复，安全终态）或 unknown（外部结果未知，待人工核对），
// 绝不自动重发。租约活跃的行不受影响。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestRecoverStuckDelayedOutbound(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService()
	defer svc.Stop(context.Background())
	now := time.Now()
	baseID := uint(900_000_000 + time.Now().UnixNano()%19_000_000)

	// 会话已被回复的租约过期行 → 自动 superseded
	const convReplied = "conv-recover-replied"
	if err := db.Create(&model.MessageHub{
		MsgID: "mh:in-rec-1", Platform: "telegram", Direction: "inbound",
		MsgType: "text", SenderID: convReplied, Content: "在吗",
		ConversationID: convReplied, SentAt: now.Add(-10 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := db.Create(&model.MessageHub{
		MsgID: "mh:out-rec-1", Platform: "telegram", Direction: "outbound",
		MsgType: "text", SenderID: "agent", ReceiverID: convReplied, Content: "人工已回复",
		ConversationID: convReplied, SentAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed outbound: %v", err)
	}
	expiredReplied := &DelayedOutboundReply{
		ID: baseID, Platform: "telegram", ConversationID: convReplied,
		SenderID: convReplied, Content: "崩溃前的 AI 回复",
		SendAt: now.Add(-time.Hour), Status: model.DelayedStatusSending,
		ClaimToken: "tok-replied", Kind: model.DelayedKindQuietHours,
	}
	leasePast := now.Add(-time.Minute)
	expiredReplied.LeaseUntil = &leasePast
	if err := db.Create(expiredReplied).Error; err != nil {
		t.Fatalf("seed expired replied: %v", err)
	}

	// 无人回复的租约过期行 → unknown（不自动重发）
	expiredUnreplied := &DelayedOutboundReply{
		ID: baseID + 1, Platform: "telegram", ConversationID: "conv-recover-open",
		SenderID: "cust-open", Content: "结果未知的 AI 回复",
		SendAt: now.Add(-time.Hour), Status: model.DelayedStatusSending,
		ClaimToken: "tok-open", Kind: model.DelayedKindQuietHours,
	}
	expiredUnreplied.LeaseUntil = &leasePast
	if err := db.Create(expiredUnreplied).Error; err != nil {
		t.Fatalf("seed expired unreplied: %v", err)
	}

	// 无租约字段的历史悬挂行 → unknown（legacy_unknown）
	legacy := &DelayedOutboundReply{
		ID: baseID + 2, Platform: "telegram", ConversationID: "conv-recover-legacy",
		SenderID: "cust-legacy", Content: "历史遗留",
		SendAt: now.Add(-2 * time.Hour), Status: model.DelayedStatusSending,
		Kind: model.DelayedKindQuietHours,
	}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	// 租约活跃的行 → 不回收
	active := &DelayedOutboundReply{
		ID: baseID + 3, Platform: "telegram", ConversationID: "conv-recover-active",
		SenderID: "cust-active", Content: "正在投递",
		SendAt: now.Add(-time.Minute), Status: model.DelayedStatusSending,
		ClaimToken: "tok-active", Kind: model.DelayedKindQuietHours,
	}
	leaseFuture := now.Add(time.Minute)
	active.LeaseUntil = &leaseFuture
	if err := db.Create(active).Error; err != nil {
		t.Fatalf("seed active: %v", err)
	}

	svc.recoverStuckDelayedOutbound(context.Background(), now)

	assertStatus := func(id uint, want string, label string) {
		t.Helper()
		var got DelayedOutboundReply
		if err := db.First(&got, id).Error; err != nil {
			t.Fatalf("%s: read back %d: %v", label, id, err)
		}
		if got.Status != want {
			t.Fatalf("%s: status = %q, want %q", label, got.Status, want)
		}
	}
	assertStatus(expiredReplied.ID, model.DelayedStatusSuperseded, "会话已被回复的过期租约行应自动 superseded")
	assertStatus(expiredUnreplied.ID, model.DelayedStatusUnknown, "结果未知的过期租约行应转 unknown")
	assertStatus(legacy.ID, model.DelayedStatusUnknown, "legacy 悬挂行应转 unknown")
	assertStatus(active.ID, model.DelayedStatusSending, "活跃租约行不得被回收")

	// unknown 行保留原始内容与原因，人工可核对
	var unk DelayedOutboundReply
	if err := db.First(&unk, expiredUnreplied.ID).Error; err != nil {
		t.Fatalf("read unknown: %v", err)
	}
	if unk.LastError == "" {
		t.Error("unknown 行必须留下原因说明（为什么没有自动重发）")
	}
}
