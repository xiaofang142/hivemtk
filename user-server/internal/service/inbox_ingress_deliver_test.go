package service

import (
	"context"
	"hivemtk-user/internal/capregistry"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestInboxIngress_DeliverOutbound_OutboxLifecycle(t *testing.T) {
	// NewTestDB 自动 SetTestDB；无参构造经全局 GetDB 拿到本用例的库。
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)

	const (
		channel        = "douyin"
		accountID      = "acc_manual_1"
		conversationID = "conv_manual_1"
		content        = "您好，这是人工回复"
	)

	h := &model.MessageHub{
		MsgID:          ContentHashMsgID(channel, conversationID, content),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       "agent_1",
		ReceiverID:     conversationID,
		Content:        content,
		ConversationID: conversationID,
		IsAIReply:      false,
		IsRead:         true,
	}
	if err := svc.DeliverOutbound(localBridgeSubmitContext(t, context.Background(), channel, accountID, "text"), h); err != nil {
		t.Fatalf("DeliverOutbound 失败: %v", err)
	}

	pending, err := svc.ListPendingOutbound(context.Background(), channel, accountID)
	if err != nil {
		t.Fatalf("ListPendingOutbound 失败: %v", err)
	}
	var found *model.MessageHub
	for _, m := range pending {
		if m.MsgID == h.MsgID {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("人工回复未出现在待下发队列，可能静默丢失（旧 EnqueueReply 缺陷）")
	}
	if found.Status != "pending" || found.Direction != "outbound" {
		t.Fatalf("期望 outbound/pending，实际 direction=%q status=%q", found.Direction, found.Status)
	}

	n, err := svc.AckOutboundDelivered(context.Background(), channel, accountID, []string{h.MsgID})
	if err != nil {
		t.Fatalf("AckOutboundDelivered 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("期望确认 1 条，实际 %d", n)
	}
	pending2, err := svc.ListPendingOutbound(context.Background(), channel, accountID)
	if err != nil {
		t.Fatalf("ListPendingOutbound(2) 失败: %v", err)
	}
	for _, m := range pending2 {
		if m.MsgID == h.MsgID {
			t.Fatalf("确认后人工回复仍出现在待下发队列")
		}
	}
}
