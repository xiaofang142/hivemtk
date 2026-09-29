package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestHandleIngress_SelfEcho_BridgeRelay_Blocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform        = "xiaohongshu"
		account         = "acct-selfecho"
		conv            = "conv-selfecho-1"
		outboundContent = "您好！😊 我是 HiveMTK 销售助手，专注为您提供一站式私域方案。"
	)

	if err := db.Create(&model.MessageHub{
		MsgID:          ContentHashMsgID(platform, conv, outboundContent),
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		Content:        outboundContent,
		SentAt:         time.Now(),
	}).Error; err != nil {
		t.Fatalf("预置 outbound 失败: %v", err)
	}

	relayedContent := "您好！　😊 我是 HiveMTK 销售助手，专注为您提供一站式私域方案。\u200b"
	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       conv,
		SenderName:     "雪大王",
		SenderType:     "customer",
		Content:        relayedContent,
		EventID:        "mh:relay-selfecho-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("自回显应 Accepted=true（幂等跳过）")
	}
	if res.QueuedForAI {
		t.Fatalf("自回显必须 QueuedForAI=false（不触发 AI，避免回环）")
	}

	var inboundCount int64
	db.Model(&model.MessageHub{}).
		Where("platform=? AND conversation_id=? AND direction='inbound'", platform, conv).
		Count(&inboundCount)
	if inboundCount != 0 {
		t.Fatalf("自回显话术不应以 inbound 入库，实际 inbound 数=%d", inboundCount)
	}
}

// 反向回归：真实客户消息（内容不匹配本账号任何出站）必须正常入库并触发 AI，不被误杀。
func TestHandleIngress_SelfEcho_RealCustomer_NotBlocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform  = "xiaohongshu"
		account   = "acct-selfecho-real"
		conv      = "conv-selfecho-real-1"
		customerQ = "我不会技术，如何安装部署呢？"
	)

	evt := &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       conv,
		SenderName:     "小红薯ABC",
		SenderType:     "customer",
		Content:        customerQ,
		EventID:        "mh:real-customer-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage 不应报错: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("真实客户消息应 Accepted=true")
	}

	var inboundCount int64
	db.Model(&model.MessageHub{}).
		Where("platform=? AND conversation_id=? AND direction='inbound' AND md5(content)=md5(?)", platform, conv, customerQ).
		Count(&inboundCount)
	if inboundCount != 1 {
		t.Fatalf("真实客户消息应入库 1 条 inbound，实际=%d", inboundCount)
	}
}
