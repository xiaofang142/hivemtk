package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// TestTelegramOutboundHubMsgID 锁住出站 msg_id 的账号维度唯一性。
//
// 反例（2026-09-19 实测）：msg_id 只写 tg-<message_id> 时，账号 9 在会话 8608488936
// 已有的 tg-26/tg-27 会让账号 5 的同编号出站行撞唯一键 (platform,msg_id,conversation_id)：
// 消息真的发出去了，落库却失败，于是 recheck 判定「未回复」并再次生成、再次投递。
func TestTelegramOutboundHubMsgID(t *testing.T) {
	got := telegramOutboundHubMsgID(5, 26)
	if got != "tg-out-5-26" {
		t.Fatalf("msg_id 应为 tg-out-<account>-<message_id>，实际 %q", got)
	}
	if telegramOutboundHubMsgID(9, 26) == got {
		t.Fatal("不同账号的同一 message_id 必须不撞唯一键")
	}
	if id := telegramOutboundHubMsgID(5, 0); id == got || id == "" {
		t.Fatalf("message_id<=0 时应回退到唯一占位 ID，实际 %q", id)
	}
}

// TestPushTelegramSendFailureTrace 投递失败必须留下站轨迹，且不把这次投递镜像进坐席会话。
//
// 断网实测（2026-09-19 13:00）：日志里 "outbound send failed" 明确发生，message_hub 却
// 一行都没有——事后无法区分「没生成回复」与「生成了但没投出去」。修复后：
// 落 status='send_failed' 的出站行 + 原因写 extra，同时「已回复」判定仍视为未回复，
// 保证补触发那一次真实重试的机会不被这条假投递抢走。
func TestPushTelegramSendFailureTrace(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.InboxConversation{})
	svc := &TelegramIntegrationService{hub: NewMessageHubServiceWithDB(db, nil)}
	ctx := context.Background()
	chatID := int64(8608488936)
	conv := "8608488936"

	if err := db.Create(&model.MessageHub{
		MsgID: "tg_upd_trace_1", Platform: "telegram", AccountID: "5", Direction: "inbound",
		ConversationID: conv, Content: "客户消息", SenderID: "8608488936", SentAt: time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}

	svc.pushTelegramSendFailureTrace(ctx, 5, chatID, "生成的回复",
		errors.New("tg send exhausted 3 retries: proxyconnect tcp: connection refused"))

	var got model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = ?", conv, "outbound").
		Order("id DESC").First(&got).Error; err != nil {
		t.Fatalf("失败轨迹未落库: %v", err)
	}
	if got.Status != "send_failed" {
		t.Fatalf("status 应为 send_failed，实际 %q", got.Status)
	}
	if got.Content != "生成的回复" || !got.IsAIReply {
		t.Fatalf("轨迹应还原本次要投递的内容: %+v", got)
	}
	if reason, _ := got.Extra["send_failed_reason"].(string); reason == "" {
		t.Fatalf("extra 应带失败原因，实际 %v", got.Extra)
	}

	var mirrored int64
	if err := db.Model(&model.InboxConversation{}).Where("conversation_id = ?", conv).Count(&mirrored).Error; err != nil {
		t.Fatalf("count inbox mirror: %v", err)
	}
	if mirrored != 0 {
		t.Fatal("未投递成功的回复不应出现在坐席会话镜像里")
	}

	hubRepo := repository.NewMessageHubRepositoryWithDB(db)
	unreplied, within, err := hubRepo.HasUnrepliedCustomerMessage(ctx, conv, 5*time.Minute)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if !unreplied || !within {
		t.Fatalf("send_failed 轨迹不得冒充已回复（否则重试机会被吞），实得 (%v,%v)", unreplied, within)
	}
	var rows int64
	if err := db.Model(&model.MessageHub{}).Where("conversation_id = ? AND direction = ?", conv, "outbound").Count(&rows).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("count outbound: %v", err)
	}
	if rows != 1 {
		t.Fatalf("应恰好一条失败轨迹，实得 %d", rows)
	}
}
