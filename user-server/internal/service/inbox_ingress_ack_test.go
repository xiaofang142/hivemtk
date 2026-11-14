package service

import (
	"hivemtk-user/internal/pkg/tenant"
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestInboxIngress_ClaimPendingOutbound_NoDuplicateForward 验证下发通道的服务端权威认领：
// 同一条 pending 不会被两轮轮询重复拉取（根除重复转发）。
func TestInboxIngress_ClaimPendingOutbound_NoDuplicateForward(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressService()
	ctx := tenant.WithTenantID(context.Background(), uint(501))

	const (
		channel   = "douyin_web"
		accountID = "acc_claim_1"
	)
	for i, c := range []string{" outbound A", "outbound B"} {
		conv := "conv_claim_1"
		h := &model.MessageHub{
			MsgID:          ContentHashMsgID(channel, conv, c),
			Platform:       channel,
			AccountID:      accountID,
			Direction:      "outbound",
			Status:         "pending",
			MsgType:        "text",
			SenderID:       "agent_1",
			ReceiverID:     conv,
			Content:        c,
			ConversationID: conv,
			IsRead:         true,
		}
		_ = i
		if err := svc.DeliverOutbound(ctx, h); err != nil {
			t.Fatalf("DeliverOutbound 失败: %v", err)
		}
	}

	claimed1, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound(1) 失败: %v", err)
	}
	if len(claimed1) != 2 {
		t.Fatalf("首次认领应取回 2 条，实际 %d", len(claimed1))
	}
	for _, m := range claimed1 {
		if m.Status != "inflight" {
			t.Errorf("认领后状态应为 inflight，实际 %q", m.Status)
		}
		if m.ClaimedAt == nil {
			t.Errorf("认领后 claimed_at 应已写入")
		}
	}

	claimed2, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound(2) 失败: %v", err)
	}
	if len(claimed2) != 0 {
		t.Fatalf("二次认领应取回 0 条（不能重复转发），实际 %d", len(claimed2))
	}

	var msgIDs []string
	for _, m := range claimed1 {
		msgIDs = append(msgIDs, m.MsgID)
	}
	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, msgIDs)
	if err != nil {
		t.Fatalf("AckOutboundDelivered 失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("期望确认 2 条，实际 %d", n)
	}

	claimed3, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound(3) 失败: %v", err)
	}
	if len(claimed3) != 0 {
		t.Fatalf("已 delivered 的消息不应再被认领，实际 %d", len(claimed3))
	}
}

// TestInboxIngress_ClaimPendingOutbound_StaleReset 验证前端崩溃/ack 丢失导致 inflight 卡死时，
// 超过认领超时后被惰性回收为 pending 并重下发（at-least-once 安全）。
func TestInboxIngress_ClaimPendingOutbound_StaleReset(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressService()
	ctx := tenant.WithTenantID(context.Background(), uint(501))

	const (
		channel   = "douyin_web"
		accountID = "acc_stale_1"
		conv      = "conv_stale_1"
		content   = "stale outbound"
	)
	h := &model.MessageHub{
		MsgID:          ContentHashMsgID(channel, conv, content),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       "agent_1",
		ReceiverID:     conv,
		Content:        content,
		ConversationID: conv,
		IsRead:         true,
	}
	if err := svc.DeliverOutbound(ctx, h); err != nil {
		t.Fatalf("DeliverOutbound 失败: %v", err)
	}

	first, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("首次认领应取回 1 条, err=%v len=%d", err, len(first))
	}

	stale := time.Now().Add(-60 * time.Second)
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", h.MsgID).
		Update("claimed_at", stale).Error; err != nil {
		t.Fatalf("拨动 claimed_at 失败: %v", err)
	}

	again, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("回收后认领失败: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("超时 inflight 应被回收并重认领 1 条，实际 %d", len(again))
	}
	if again[0].Status != "inflight" {
		t.Errorf("回收后状态应为 inflight，实际 %q", again[0].Status)
	}
}

// TestInboxIngress_InterceptEcho_DistinguishesSelfAndCustomer 验证统一收件中间件：
// AI 自己的出站回写（self）被判为回显拦截；客户复述 AI 原话（other，不同发送者）放行不误删。
// 这是「渠道+发送者+内容」哈希去重相对旧「仅 platform+content」的核心修复。
func TestInboxIngress_InterceptEcho_DistinguishesSelfAndCustomer(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressService()
	ctx := tenant.WithTenantID(context.Background(), uint(501))

	const (
		channel   = "douyin_web"
		accountID = "acc_echo_1"
		conv      = "conv_echo_1"
		content   = "您好，请问有什么可以帮您"
	)

	ob := &model.MessageHub{
		MsgID:          ContentHashMsgID(channel, conv, content),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		SenderID:       accountID,
		ReceiverID:     conv,
		Content:        content,
		ConversationID: conv,
		IsRead:         true,
		DedupHash:      ContentHashWithSender(channel, accountID, content),
	}
	if err := db.Create(ob).Error; err != nil {
		t.Fatalf("铺设出站记录失败: %v", err)
	}

	selfEvent := &model.MessageEvent{
		EventID:        "evt_self_echo",
		Channel:        channel,
		ConversationID: conv,
		Content:        content,
		SenderType:     "self",
		SenderID:       accountID,
	}
	dec, err := svc.interceptInbound(ctx, selfEvent)
	if err != nil {
		t.Fatalf("interceptInbound(self) 失败: %v", err)
	}
	if !dec.Blocked || !dec.IsSelfEcho {
		t.Fatalf("self 回显应被拦截(IsSelfEcho)，实际 Blocked=%v IsSelfEcho=%v reason=%q", dec.Blocked, dec.IsSelfEcho, dec.Reason)
	}

	customerEvent := &model.MessageEvent{
		EventID:        "evt_customer_echo",
		Channel:        channel,
		ConversationID: conv,
		Content:        content,
		SenderType:     "customer",
		SenderID:       "customer_998",
	}
	dec2, err := svc.interceptInbound(ctx, customerEvent)
	if err != nil {
		t.Fatalf("interceptInbound(customer) 失败: %v", err)
	}
	if dec2.Blocked {
		t.Fatalf("客户复述 AI 原话不应被拦截（否则丢失客户消息），实际 reason=%q", dec2.Reason)
	}
}
