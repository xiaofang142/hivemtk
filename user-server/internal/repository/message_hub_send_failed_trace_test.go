package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestHasUnrepliedCustomerMessage_SendFailedOutboundIsNotAReply 投递失败的出站行不算「已回复」。
//
// 出站发送失败现在会留一条 status='send_failed' 的出站轨迹；若 "已回复" 判定只看
// direction，这条根本没到达客户的行会把会话判成已回复，补触发链路就此失效，
// 客户再也收不到回复。status 列可空（默认 'pending' 但历史/直插行为 NULL），
// 所以判定必须 NULL 安全：用 status = 'send_failed' 取反会把 NULL 行一起滤掉。
func TestHasUnrepliedCustomerMessage_SendFailedOutboundIsNotAReply(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()
	conv := "conv-send-failed-oracle"
	now := time.Now()

	rows := []model.MessageHub{
		{MsgID: "in-1", Platform: "telegram", AccountID: "5", Direction: "inbound", ConversationID: conv, Content: "客户消息", SentAt: now.Add(-30 * time.Second)},
		{MsgID: "out-failed", Platform: "telegram", AccountID: "5", Direction: "outbound", Status: "send_failed", ConversationID: conv, Content: "没发出去的回复", SentAt: now.Add(-20 * time.Second)},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	unreplied, withinWindow, err := repo.HasUnrepliedCustomerMessage(ctx, conv, 5*time.Minute)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if !unreplied || !withinWindow {
		t.Fatalf("send_failed 出站不得算已回复，期望 (true,true)，实得 (%v,%v)", unreplied, withinWindow)
	}

	// 反向对照：成功出站（status=pending/NULL）仍按已回复处理，避免补触发风暴回归。
	if err := db.Create(&model.MessageHub{MsgID: "out-ok", Platform: "telegram", AccountID: "5",
		Direction: "outbound", Status: "sent", ConversationID: conv, Content: "已投递", SentAt: now.Add(-10 * time.Second)}).Error; err != nil {
		t.Fatalf("seed ok row: %v", err)
	}
	unreplied, _, err = repo.HasUnrepliedCustomerMessage(ctx, conv, 5*time.Minute)
	if err != nil {
		t.Fatalf("oracle after sent: %v", err)
	}
	if unreplied {
		t.Fatal("存在成功出站时仍判未回复，会重新点燃补触发风暴")
	}

	// NULL 安全对照：status 为 NULL 的出站行必须仍算已回复。
	nullConv := conv + "-null"
	if err := db.Create(&model.MessageHub{MsgID: "in-n", Platform: "telegram", AccountID: "5",
		Direction: "inbound", ConversationID: nullConv, Content: "hi", SentAt: now.Add(-30 * time.Second)}).Error; err != nil {
		t.Fatalf("seed inbound null conv: %v", err)
	}
	if err := db.Create(&model.MessageHub{MsgID: "out-n", Platform: "telegram", AccountID: "5",
		Direction: "outbound", ConversationID: nullConv, Content: "已投递", SentAt: now.Add(-10 * time.Second)}).Error; err != nil {
		t.Fatalf("seed null-status outbound: %v", err)
	}
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", "out-n").
		Update("status", gorm.Expr("NULL")).Error; err != nil {
		t.Fatalf("force NULL status: %v", err)
	}
	unreplied, _, err = repo.HasUnrepliedCustomerMessage(ctx, nullConv, 5*time.Minute)
	if err != nil {
		t.Fatalf("oracle null: %v", err)
	}
	if unreplied {
		t.Fatal("status 为 NULL 的出站被误滤成未回复：判定未做 NULL 安全处理")
	}
}

// TestListByConversationContext_HidesSendFailed 坐席可见消息列表不含投递失败行。
func TestListByConversationContext_HidesSendFailed(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := NewMessageHubRepositoryWithDB(db)
	conv := "conv-list-hide"
	now := time.Now()

	seed := []model.MessageHub{
		{MsgID: "l-in", Platform: "telegram", AccountID: "5", Direction: "inbound", SenderID: "cust1", ConversationID: conv, Content: "在吗", SentAt: now.Add(-2 * time.Minute)},
		{MsgID: "l-fail", Platform: "telegram", AccountID: "5", Direction: "outbound", Status: "send_failed", ReceiverID: "cust1", ConversationID: conv, Content: "发失败了", SentAt: now.Add(-time.Minute)},
		{MsgID: "l-ok", Platform: "telegram", AccountID: "5", Direction: "outbound", Status: "sent", ReceiverID: "cust1", ConversationID: conv, Content: "在的", SentAt: now},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	got, err := repo.ListByConversationContext(context.Background(), "telegram", "5", "cust1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, m := range got {
		if m.MsgID == "l-fail" {
			t.Fatalf("未投递成功的回复不应出现在坐席消息列表： %+v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 条（入站 1 + 成功出站 1），实得 %d", len(got))
	}
}
