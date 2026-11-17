package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 错归去重闸专项（批：Bridge 重复消息根治）：
// 线上实证（tz3rmdj 2026-10-22 20:27:01/03）——扩展多窗口 DOM 采集把 A 会话的一批消息
// 错归到 B 会话二次上报，同裸 msg_id（内容哈希）2 秒内落了两行（conversation_id 不同，
// 唯一索引 uni_message_hub_platform_msg_conv 含 conversation_id 拦不住），触发重复回复。
// 闸的语义：错归窗（InboundMisattribWindow）内同裸 msg_id 跨会话二次上报 ⇒ 判重复拦截；
// 带 #N 后缀（上报方断言独立消息）与窗口过期 ⇒ 放行。

func misattribEvent(nonce, eventID, conv, content string) *model.MessageEvent {
	return &model.MessageEvent{
		Channel:        model.ChannelDouyin,
		SenderID:       "customer-" + nonce,
		SenderName:     "说缘" + nonce,
		SenderType:     "customer",
		Content:        content,
		EventID:        eventID,
		ConversationID: conv,
		MsgType:        model.MsgTypeText,
		Timestamp:      time.Now(),
		Extra:          map[string]interface{}{"account_id": "acct-" + nonce},
	}
}

func TestIngress_MisattributedSameMsgIDAcrossConversationsBlocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	svc.hubRepo = repository.NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()

	nonce := fmt.Sprintf("misattr-%d", time.Now().UnixNano())
	eventID := "mh:" + nonce[:8] // 裸内容哈希，无 #N 后缀
	content := "你是不是弄错了 " + nonce

	// 第一条：A 会话正常入库
	d1, err := svc.interceptInbound(ctx, misattribEvent(nonce, eventID, "conv:A-"+nonce, content))
	if err != nil {
		t.Fatalf("interceptInbound(A): %v", err)
	}
	if d1 == nil {
		t.Fatal("会话 A 的入站判定不能为空")
	}
	if d1.Blocked {
		t.Fatalf("首条不该被拦：%s", d1.Reason)
	}
	if err := db.Create(&model.MessageHub{
		TenantID: 1, Platform: model.ChannelDouyin, MsgID: eventID, AccountID: "acct-" + nonce,
		Direction: "inbound", Status: "received", MsgType: model.MsgTypeText,
		SenderID: "customer-" + nonce, Content: content,
		ConversationID: "conv:A-" + nonce, SentAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed A 行: %v", err)
	}

	// 第二条：同 msg_id、不同会话（错归上报）⇒ 必须被错归闸拦截
	d2, err := svc.interceptInbound(ctx, misattribEvent(nonce, eventID, "conv:B-"+nonce, content))
	if err != nil {
		t.Fatalf("interceptInbound(B): %v", err)
	}
	if d2 == nil {
		t.Fatal("会话 B 的入站判定不能为空")
	}
	if !d2.Blocked || !d2.IsDup {
		t.Fatalf("错归闸未生效：同裸 msg_id 错归窗内跨会话二次上报应拦截，got blocked=%v dup=%v reason=%q",
			d2.Blocked, d2.IsDup, d2.Reason)
	}
}

func TestIngress_MisattribGateAllowsOccurrenceSuffixAndExpiredWindow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	svc.hubRepo = repository.NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()

	nonce := fmt.Sprintf("misattr2-%d", time.Now().UnixNano())
	base := "mh:" + nonce[:8]

	seed := func(msgID, conv string, createdAt time.Time) {
		if err := db.Create(&model.MessageHub{
			TenantID: 1, Platform: model.ChannelDouyin, MsgID: msgID, AccountID: "acct-" + nonce,
			Direction: "inbound", Status: "received", MsgType: model.MsgTypeText,
			SenderID: "customer-" + nonce, Content: "内容 " + nonce,
			ConversationID: conv, SentAt: createdAt, CreatedAt: createdAt,
		}).Error; err != nil {
			t.Fatalf("seed %s: %v", msgID, err)
		}
	}

	// 带 #N 后缀：上报方断言独立消息，即使跨会话也不拦
	seed(base, "conv:A-"+nonce, time.Now())
	d1, err := svc.interceptInbound(ctx, misattribEvent(nonce, base+"#1", "conv:B-"+nonce, "内容 "+nonce))
	if err != nil {
		t.Fatalf("interceptInbound(#N): %v", err)
	}
	if d1 == nil {
		t.Fatal("带发生次数后缀的入站判定不能为空")
	}
	if d1.Blocked && d1.Reason == "duplicate(same msg_id ingested in another conversation within misattribution window)" {
		t.Fatalf("#N 后缀不应被错归闸拦（上报方已断言独立消息），reason=%q", d1.Reason)
	}

	// 窗口过期：错归窗外的跨会话同 hash 是真实新发言，放行
	old := time.Now().Add(-InboundMisattribWindow - time.Minute)
	seed(base+"aaaa", "conv:C-"+nonce, old)
	d2, err := svc.interceptInbound(ctx, misattribEvent(nonce, base+"aaaa", "conv:D-"+nonce, "内容 "+nonce))
	if err != nil {
		t.Fatalf("interceptInbound(expired): %v", err)
	}
	if d2 == nil {
		t.Fatal("窗口过期后的入站判定不能为空")
	}
	if d2.Blocked && d2.Reason == "duplicate(same msg_id ingested in another conversation within misattribution window)" {
		t.Fatalf("错归窗外不应拦截（历史原话再说一遍是真实消息），reason=%q", d2.Reason)
	}
}
