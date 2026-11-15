package service

// 关注后补发积压（2026-11-15 全平台关注逻辑 Phase 4）回归钉：
//   - send_failed（限额阻塞）行复活为 pending（预算清零/错误位清空）；
//   - delayed quiet_hours/send_retry 行立即到期；
//   - delivered 终态与无关会话不动；nil 仓储 no-op 不 panic。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestFlushConversationBacklog_RevivesAndMatures(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.DelayedOutboundReply{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	svc := NewInboxIngressService()
	svc.hubRepo = repository.NewMessageHubRepository()
	svc.pendingOutbound = repository.NewDelayedOutboundRepository(database)
	ctx := context.Background()

	const (
		channel = "xiaohongshu"
		account = "acc-flush"
		conv    = "conv-flush"
	)

	// ① 限额阻塞的 send_failed 行（复活目标）
	if err := database.Create(&model.MessageHub{
		TenantID: 18, Platform: channel, AccountID: account, ConversationID: conv,
		MsgID: "mh:flush-sf", Direction: "outbound", Status: "send_failed", MsgType: "text",
		SenderID: account, ReceiverID: "cust", Content: "被限额阻塞的回复",
		IsAIReply: true, IsRead: true, SentAt: time.Now(), PushAttempts: 9, PushError: "platform_limit",
	}).Error; err != nil {
		t.Fatalf("seed send_failed 失败: %v", err)
	}
	// ② delivered 终态（必须不动）
	if err := database.Create(&model.MessageHub{
		TenantID: 18, Platform: channel, AccountID: account, ConversationID: conv,
		MsgID: "mh:flush-done", Direction: "outbound", Status: "delivered", MsgType: "text",
		SenderID: account, ReceiverID: "cust", Content: "已投递", IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed delivered 失败: %v", err)
	}
	// ③ 无关会话 send_failed（必须不动）
	if err := database.Create(&model.MessageHub{
		TenantID: 18, Platform: channel, AccountID: account, ConversationID: "conv-other",
		MsgID: "mh:flush-other", Direction: "outbound", Status: "send_failed", MsgType: "text",
		SenderID: account, ReceiverID: "cust", Content: "无关会话", IsAIReply: true, IsRead: true, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed 无关会话失败: %v", err)
	}
	// ④ delayed 行（到期目标）
	future := time.Now().Add(2 * time.Hour)
	if err := database.Create(&model.DelayedOutboundReply{
		TenantID: 18, Platform: channel, AccountID: account, ConversationID: conv,
		Kind: model.DelayedKindQuietHours, Status: "pending", SendAt: future,
	}).Error; err != nil {
		t.Fatalf("seed delayed 失败: %v", err)
	}

	svc.FlushConversationBacklog(ctx, channel, account, conv)

	var revived model.MessageHub
	database.Where("msg_id = ?", "mh:flush-sf").First(&revived)
	if revived.Status != "pending" || revived.PushAttempts != 0 || revived.PushError != "" {
		t.Fatalf("send_failed 行应复活为 pending/预算清零，实际 %+v", revived)
	}
	var done model.MessageHub
	database.Where("msg_id = ?", "mh:flush-done").First(&done)
	if done.Status != "delivered" {
		t.Fatalf("delivered 终态不应被动，实际 %s", done.Status)
	}
	var other model.MessageHub
	database.Where("msg_id = ?", "mh:flush-other").First(&other)
	if other.Status != "send_failed" {
		t.Fatalf("无关会话不应被复活，实际 %s", other.Status)
	}
	var due model.DelayedOutboundReply
	database.Where("conversation_id = ?", conv).First(&due)
	if due.SendAt.After(time.Now()) {
		t.Fatalf("delayed 行应已到期，实际 send_at=%v", due.SendAt)
	}
}

func TestFlushConversationBacklog_NilReposNoPanic(t *testing.T) {
	svc := NewInboxIngressService()
	svc.FlushConversationBacklog(context.Background(), "douyin", "acc", "conv") // 不得 panic
}
