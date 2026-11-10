package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// M-2 客户头像链路（2026-11-11）落库半项验收：
//  ① 桥端 history 链路（PersistBridgeHistory）把 event.SenderAvatar 落 message_hub.sender_avatar；
//  ② 空/超长 URL 与 media_url 同款口径（TruncateMediaURL）——空为空、超 500 字符截断；
//  ③ inbox_conversations.sender_avatar 随 last message upsert：「新值优先、空值保旧」。

func TestPersistBridgeHistory_SenderAvatarPersisted(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-av-1"
		conv     = "conv-av-1"
		avatar   = "https://p3-sign.douyinpic.com/img/avatar.jpeg?x-expires=999"
	)

	ev := &model.MessageEvent{
		Channel:        platform,
		EventID:        "mh:av000001",
		SenderID:       conv,
		SenderType:     "customer",
		SenderName:     "好吃嘴辰辰",
		SenderAvatar:   avatar,
		MsgType:        "text",
		Content:        "在吗",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	if err := svc.PersistBridgeHistory(ctx, ev, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory 失败: %v", err)
	}

	var hub model.MessageHub
	if err := db.Where("msg_id=? AND direction='inbound'", "mh:av000001").First(&hub).Error; err != nil {
		t.Fatalf("取回 message_hub 行失败: %v", err)
	}
	if hub.SenderAvatar != avatar {
		t.Fatalf("① message_hub.sender_avatar 未落库: got %q want %q", hub.SenderAvatar, avatar)
	}
}

func TestPersistBridgeHistory_SenderAvatarClamped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	long := "https://p3-sign.douyinpic.com/img/" + strings.Repeat("x", 600)
	ev := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		EventID:        "mh:av000002",
		SenderID:       "conv-av-2",
		SenderType:     "customer",
		SenderAvatar:   long,
		MsgType:        "text",
		Content:        "hi",
		ConversationID: "conv-av-2",
		Extra:          map[string]interface{}{"account_id": "acct-av-2"},
	}
	if err := svc.PersistBridgeHistory(ctx, ev, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory 失败: %v", err)
	}

	var hub model.MessageHub
	if err := db.Where("msg_id=? AND direction='inbound'", "mh:av000002").First(&hub).Error; err != nil {
		t.Fatalf("取回 message_hub 行失败: %v", err)
	}
	if got := len([]rune(hub.SenderAvatar)); got != model.MaxMediaURLLen {
		t.Fatalf("② 超长 sender_avatar 未截断到列宽: %d 字符 (want %d)", got, model.MaxMediaURLLen)
	}
}

func TestPersistBridgeHistory_SenderAvatarEmptyStaysEmpty(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	ev := &model.MessageEvent{
		Channel:        model.ChannelJD,
		EventID:        "mh:av000003",
		SenderID:       "conv-av-3",
		SenderType:     "customer",
		MsgType:        "text",
		Content:        "hi",
		ConversationID: "conv-av-3",
		Extra:          map[string]interface{}{"account_id": "acct-av-3"},
	}
	if err := svc.PersistBridgeHistory(ctx, ev, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory 失败: %v", err)
	}

	var hub model.MessageHub
	if err := db.Where("msg_id=? AND direction='inbound'", "mh:av000003").First(&hub).Error; err != nil {
		t.Fatalf("取回 message_hub 行失败: %v", err)
	}
	if hub.SenderAvatar != "" {
		t.Fatalf("② 无头像事件 sender_avatar 必须保持空串: got %q", hub.SenderAvatar)
	}
}

// 会话行 sender_avatar：新值写入 → 空值保旧 → 新值替换（与 customer_name 同语义）。
func TestInboxUpsert_SenderAvatar_NewThenKeepOldThenReplace(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.InboxConversation{})
	ctx := context.Background()
	repo := repository.NewInboxConversationRepositoryWithDB(db)

	const (
		platform = "douyin"
		account  = "acct-conv-av"
		customer = "conv-conv-av"
	)
	in := repository.UpsertFromMessageInput{
		Platform:        platform,
		AccountID:       account,
		CustomerID:      customer,
		CustomerName:    "客户A",
		CustomerAvatar:  "https://f/av-new.png",
		ConversationID:  customer,
		LastMessageFrom: "customer",
	}
	if err := repo.UpsertFromMessage(ctx, in); err != nil {
		t.Fatalf("首次 upsert 失败: %v", err)
	}
	var conv model.InboxConversation
	if err := db.Where("platform=? AND account_id=? AND customer_id=?", platform, account, customer).First(&conv).Error; err != nil {
		t.Fatalf("取回会话行失败: %v", err)
	}
	if conv.SenderAvatar != "https://f/av-new.png" {
		t.Fatalf("① 新会话客户头像未落库: got %q", conv.SenderAvatar)
	}

	// 出站/空头像行不清掉已有值（firstNonEmpty 保旧）
	in.CustomerAvatar = ""
	in.LastMessageFrom = "staff"
	if err := repo.UpsertFromMessage(ctx, in); err != nil {
		t.Fatalf("空头像 upsert 失败: %v", err)
	}
	if err := db.Where("platform=? AND account_id=? AND customer_id=?", platform, account, customer).First(&conv).Error; err != nil {
		t.Fatalf("取回会话行失败: %v", err)
	}
	if conv.SenderAvatar != "https://f/av-new.png" {
		t.Fatalf("② 空头像不得清掉已有值: got %q", conv.SenderAvatar)
	}

	// 新值替换
	in.CustomerAvatar = "https://f/av-newer.png"
	if err := repo.UpsertFromMessage(ctx, in); err != nil {
		t.Fatalf("新头像 upsert 失败: %v", err)
	}
	if err := db.Where("platform=? AND account_id=? AND customer_id=?", platform, account, customer).First(&conv).Error; err != nil {
		t.Fatalf("取回会话行失败: %v", err)
	}
	if conv.SenderAvatar != "https://f/av-newer.png" {
		t.Fatalf("③ 新头像应替换旧值: got %q", conv.SenderAvatar)
	}
}

// InboxService.upsertInternal 装配层：msg.SenderAvatar 经 inboxCustomerAvatar 传入
// UpsertFromMessageInput（inbox.go 落库链路接线验证）。
func TestInboxServiceUpsertInternal_SenderAvatarWired(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.InboxConversation{})
	ctx := context.Background()
	svc := &InboxService{
		inboxRepo:      repository.NewInboxConversationRepositoryWithDB(db),
		staffLoadCache: make(map[string]int),
	}

	msg := &model.MessageHub{
		MsgID:          "mh:av000004",
		Platform:       "xianyu",
		AccountID:      "acct-wire-1",
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "conv-wire-1",
		SenderName:     "买家小张",
		SenderAvatar:   "https://f/buyer.png",
		Content:        "这个还在吗",
		ConversationID: "conv-wire-1",
	}
	if err := svc.upsertInternal(ctx, msg); err != nil {
		t.Fatalf("upsertInternal 失败: %v", err)
	}
	var conv model.InboxConversation
	if err := db.Where("platform=? AND account_id=? AND customer_id=?", "xianyu", "acct-wire-1", "conv-wire-1").First(&conv).Error; err != nil {
		t.Fatalf("取回会话行失败: %v", err)
	}
	if conv.SenderAvatar != "https://f/buyer.png" {
		t.Fatalf("会话行 sender_avatar 未从消息行带到 upsert: got %q", conv.SenderAvatar)
	}
	if conv.CustomerName != "买家小张" {
		t.Fatalf("对照组 customer_name 失真（回归信号）: got %q", conv.CustomerName)
	}
}
