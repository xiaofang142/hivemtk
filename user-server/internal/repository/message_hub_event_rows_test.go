package repository

// 渠道系统事件行（msg_type='event'：TG 入群/退群、审批通知）不是「客户发来的问话」。
// 它一旦混进 HasUnrepliedCustomerMessage / GetLastInboundByConversation，
// 补触发就会拿「[入群事件] 用户 X 加入群组」这句系统通知去问 AI（三人行 15:24:28
// 那条对刚被门控禁言的新人喊欢迎的回复就是这么产生的）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

func newHubEvent(conv, msgID string, sentAt time.Time, content string) *model.MessageHub {
	h := newHubWithConv("telegram", "acc_evt", msgID, msgID, "inbound", "received", sentAt)
	h.ConversationID = conv
	h.MsgType = "event"
	h.Content = content
	return h
}

func TestMessageHubRepository_EventRowIsNotLastInbound(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	now := time.Now()

	conv := "conv:evt-only"
	if err := db.Create(newHubEvent(conv, "e1", now.Add(-3*time.Second), "[入群事件] 用户 Dark 加入群组 三人行")).Error; err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetLastInboundByConversation(ctx, conv)
	if !errors.Is(err, gorm.ErrRecordNotFound) || got != nil {
		t.Errorf("只有系统事件行的会话不该有「最后一条客户消息」，得 (%v, %v)", got, err)
	}

	textMsg := newHubWithConv("telegram", "acc_evt", "t9", "t9", "inbound", "received", now.Add(-30*time.Second))
	textMsg.ConversationID = conv
	if err := db.Create(textMsg).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(newHubEvent(conv, "e2", now, "[退群事件] 用户 Dark 离开群组")).Error; err != nil {
		t.Fatal(err)
	}
	last, err := repo.GetLastInboundByConversation(ctx, conv)
	if err != nil {
		t.Fatalf("取最后一条客户消息失败: %v", err)
	}
	if last.MsgID != "t9" {
		t.Errorf("最后一条客户消息应为文本行 t9（事件行 e2 更晚但不是客户消息），实际 %s/%s", last.MsgID, last.MsgType)
	}
}

// 窗口判定必须按真实的客户消息算：事件行不得把超窗的旧问话「洗白」成刚收到的消息。
func TestMessageHubRepository_EventRowDoesNotRefreshReplyWindow(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	now := time.Now()

	conv := "conv:evt-window"
	stale := newHubWithConv("telegram", "acc_evt", "stale-1", "t-old", "inbound", "received", now.Add(-4*time.Minute))
	stale.ConversationID = conv
	if err := db.Create(stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(newHubEvent(conv, "t-new", now.Add(-3*time.Second), "[入群事件] 用户 虾 加入群组 三人行")).Error; err != nil {
		t.Fatal(err)
	}

	unreplied, within, err := repo.HasUnrepliedCustomerMessage(ctx, conv, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !unreplied {
		t.Error("有一条没回的客户问话 ⇒ unreplied 应为 true")
	}
	if within {
		t.Error("最近的客户问话在 4min 前、窗口 2min ⇒ within 应为 false，事件行不得替它续期")
	}
}
