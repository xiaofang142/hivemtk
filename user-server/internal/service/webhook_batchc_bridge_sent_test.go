package service

// 批C D-02：桥接五渠道（抖音/小红书/TikTok/闲鱼/快手）出站把「写库交接」当成「已送达」，
// 且 hubMsg==nil、落库失败、被标 failed 三种情况一律 sent=true。
// 后果：重放 worker 按 sent 收口直接 MarkSent，这条 AI 回复静默丢失、无人再补。
// 本用例要求 sent 只在「确实进入出库队列」时为真。

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newBridgeSentTestService(t *testing.T) (*WebhookService, context.Context) {
	t.Helper()
	// 免打扰窗口会把出站改判为「次日首发」直接 return，与 sent 口径无关，显式关掉。
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService(db)
	return svc, context.Background()
}

func bridgeInboundHub(platform, account, conv, sender string) *model.MessageHub {
	return &model.MessageHub{
		MsgID:          "mh:in-" + conv,
		Platform:       platform,
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       sender,
		Content:        "在吗",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
}

// TestSendOutbound_Bridge_HubMsgNilIsNotSent 无入站上下文＝没有出库目标，不得判成功。
func TestSendOutbound_Bridge_HubMsgNilIsNotSent(t *testing.T) {
	svc, ctx := newBridgeSentTestService(t)
	defer svc.Stop(ctx)

	releaseReplyClaimForTest(t, "evt-d02-nil")
	p := &ParsedPayload{EventID: "evt-d02-nil", Sender: "cust-d02-nil", Content: "在吗", ChatID: "conv-d02-nil"}

	sent, sendErr := svc.sendOutbound(ctx, ChannelDouyin, "acct-d02-nil", p, "AI 回复", nil, nil)
	if sent {
		t.Errorf("hubMsg 为空时不得返回 sent=true（D-02），got sent=%v err=%v", sent, sendErr)
	}
}

// TestSendOutbound_Bridge_UndeliverableIsNotSent 占位账号被标 failed：不得判成功，
// 且该失败不可重试（重投同样不可达），不能污染重试队列。
func TestSendOutbound_Bridge_UndeliverableIsNotSent(t *testing.T) {
	svc, ctx := newBridgeSentTestService(t)
	defer svc.Stop(ctx)

	db := svc.lazyDB()
	hub := bridgeInboundHub("xiaohongshu", "xiaohongshu-unknown", "conv-d02-undel", "cust-d02-undel")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-d02-undel")
	p := &ParsedPayload{EventID: "evt-d02-undel", Sender: hub.SenderID, Content: "在吗", ChatID: hub.ConversationID}

	sent, sendErr := svc.sendOutbound(ctx, ChannelXiaohongshu, hub.AccountID, p, "AI 回复（不可达）", hub, nil)
	if sent {
		t.Errorf("不可达目标不得判 sent=true（D-02）")
	}
	if sendErr == nil {
		t.Fatal("不可达目标必须回传错误，否则重放 worker 无从改判")
	}
	ce := AsChannelError(sendErr)
	if ce == nil || ce.Retryable {
		t.Errorf("占位账号不可达应为不可重试错误，got %+v", ce)
	}

	var retryCnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("kind = ?", model.DelayedKindSendRetry).Count(&retryCnt).Error; err != nil {
		t.Fatalf("count retry: %v", err)
	}
	if retryCnt != 0 {
		t.Errorf("不可重试的失败不得进重试队列，实际 %d 条", retryCnt)
	}
}

// TestSendOutbound_Bridge_PersistFailureIsNotSent message_hub 写不进去时不得判成功，
// 且必须按可重试失败进重试队列（否则这条回复既没出库也没人再投）。
func TestSendOutbound_Bridge_PersistFailureIsNotSent(t *testing.T) {
	svc, ctx := newBridgeSentTestService(t)
	defer svc.Stop(ctx)

	db := svc.lazyDB()
	// message_hub.conversation_id 是 varchar(100)，用超长会话号真实触发一次出库行入库失败
	// （入站行只在内存里造，避免先撞同一个长度限制）；
	// delayed_outbound_reply.conversation_id 是 varchar(128)，重试记录写得下。
	longConv := "conv-d02-overflow-" + strings.Repeat("x", 110)
	hub := bridgeInboundHub("douyin", "acct-d02-overflow", longConv, "cust-d02-overflow")
	releaseReplyClaimForTest(t, "evt-d02-overflow")
	p := &ParsedPayload{EventID: "evt-d02-overflow", Sender: hub.SenderID, Content: "在吗", ChatID: hub.ConversationID}

	sent, sendErr := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "AI 回复（落库失败）", hub, nil)
	if sent {
		t.Errorf("出站未落库不得判 sent=true（D-02），got err=%v", sendErr)
	}
	if sendErr == nil {
		t.Fatal("落库失败必须回传错误")
	}
	if ce := AsChannelError(sendErr); ce == nil || !ce.Retryable {
		t.Errorf("落库失败应按可重试错误回传，got %+v", ce)
	}

	var rec DelayedOutboundReply
	if err := db.Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, longConv).
		First(&rec).Error; err != nil {
		t.Fatalf("落库失败应进入持久化重试队列: %v", err)
	}
}

// TestSendOutbound_Bridge_QueuedIsSent 反面对照：正常出库仍须 sent=true，
// 否则上面的三条红可能是「桥接渠道永远 false」造成的假修复。
func TestSendOutbound_Bridge_QueuedIsSent(t *testing.T) {
	svc, ctx := newBridgeSentTestService(t)
	defer svc.Stop(ctx)

	db := svc.lazyDB()
	hub := bridgeInboundHub("douyin", "acct-d02-ok", "conv-d02-ok", "cust-d02-ok")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-d02-ok")
	p := &ParsedPayload{EventID: "evt-d02-ok", Sender: hub.SenderID, Content: "在吗", ChatID: hub.ConversationID}

	sent, sendErr := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "AI 回复（正常出库）", hub, nil)
	if !sent || sendErr != nil {
		t.Fatalf("正常出库应 sent=true 且无错误，got sent=%v err=%v", sent, sendErr)
	}
	var out model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ? AND status = ?",
		"outbound", hub.ConversationID, "pending").First(&out).Error; err != nil {
		t.Fatalf("出库行应为 pending 等待扩展领取: %v", err)
	}
}
