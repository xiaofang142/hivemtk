package repository

// 关注事件驱动的积压补发（2026-11-15 全平台关注逻辑）回归钉：
//   - MarkDueByConversation：delayed 行 send_at 立即到期（常规认领消费）；
//   - ReviveSendFailedByConversation：限额阻塞的 send_failed 行恢复 pending（保留预算）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupFollowFlushDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{})
	db.SetTestDB(database)
	return database
}

func TestFollowFlush_MarkDueByConversation(t *testing.T) {
	g := setupFollowFlushDB(t)
	r := &DelayedOutboundRepository{db: g}
	ctx := context.Background()

	future := time.Now().Add(2 * time.Hour)
	if err := g.Create(&model.DelayedOutboundReply{
		TenantID: 18, Platform: "xiaohongshu", AccountID: "acc-f", ConversationID: "conv-f",
		Kind: model.DelayedKindQuietHours, Status: "pending", SendAt: future,
	}).Error; err != nil {
		t.Fatalf("seed delayed 行失败: %v", err)
	}

	n, err := r.MarkDueByConversation(ctx, "xiaohongshu", "acc-f", "conv-f", time.Now())
	if err != nil || n != 1 {
		t.Fatalf("MarkDueByConversation 应到期 1 行，实际 n=%d err=%v", n, err)
	}
	var row model.DelayedOutboundReply
	g.Where("conversation_id = ?", "conv-f").First(&row)
	if row.SendAt.After(time.Now()) {
		t.Fatalf("到期后 send_at 应 ≤ now，实际 %v", row.SendAt)
	}

	// 无关会话不受影响（send_at 保持未来值）
	if err := g.Create(&model.DelayedOutboundReply{
		TenantID: 18, Platform: "xiaohongshu", AccountID: "acc-f", ConversationID: "conv-other",
		Kind: model.DelayedKindQuietHours, Status: "pending", SendAt: future,
	}).Error; err != nil {
		t.Fatalf("seed 无关会话行失败: %v", err)
	}
	if _, err := r.MarkDueByConversation(ctx, "xiaohongshu", "acc-f", "conv-f", time.Now()); err != nil {
		t.Fatalf("二次到期标记失败: %v", err)
	}
	var other model.DelayedOutboundReply
	g.Where("conversation_id = ?", "conv-other").First(&other)
	if !other.SendAt.Equal(future) {
		t.Fatalf("无关会话 send_at 应保持未来值，实际 %v", other.SendAt)
	}
}

func TestFollowFlush_ReviveSendFailedByConversation(t *testing.T) {
	g := setupFollowFlushDB(t)
	hub := &MessageHubRepository{db: g}
	ctx := context.Background()

	if err := g.Create(&model.MessageHub{
		TenantID: 18, Platform: "xiaohongshu", AccountID: "acc-f", ConversationID: "conv-f",
		MsgID: "mh:flush-sf", Direction: "outbound", Status: "send_failed", MsgType: "text",
		SenderID: "acc-f", ReceiverID: "cust", Content: "被限额阻塞的回复",
		IsAIReply: true, IsRead: true, SentAt: time.Now(), PushAttempts: 7, PushError: "platform_limit",
	}).Error; err != nil {
		t.Fatalf("seed send_failed 行失败: %v", err)
	}

	n, err := hub.ReviveSendFailedByConversation(ctx, "xiaohongshu", "acc-f", "conv-f")
	if err != nil || n != 1 {
		t.Fatalf("ReviveSendFailedByConversation 应复活 1 行，实际 n=%d err=%v", n, err)
	}
	var row model.MessageHub
	g.Where("msg_id = ?", "mh:flush-sf").First(&row)
	if row.Status != "pending" || row.PushAttempts != 7 || row.PushError != "" {
		t.Fatalf("复活后应 pending/保留7次已用预算/错误位清空，实际 %+v", row)
	}

	// delivered 终态不动
	if err := g.Create(&model.MessageHub{
		TenantID: 18, Platform: "xiaohongshu", AccountID: "acc-f", ConversationID: "conv-f",
		MsgID: "mh:flush-done", Direction: "outbound", Status: "delivered", MsgType: "text",
		SenderID: "acc-f", ReceiverID: "cust", Content: "已投递", IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed delivered 行失败: %v", err)
	}
	n2, _ := hub.ReviveSendFailedByConversation(ctx, "xiaohongshu", "acc-f", "conv-f")
	if n2 != 0 {
		t.Fatalf("delivered 终态不应被复活，实际 %d", n2)
	}
}
