package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestSendOutbound_Bridge_DuplicateAuditRemainsNonDeliverable(t *testing.T) {
	svc, baseCtx := newBridgeSentTestService(t)
	ctx := tenant.WithTenantID(baseCtx, 18)
	defer svc.Stop(ctx)
	g := svc.eventRepo.GetDB()
	accountID := seedCanonicalBridgeOutboundAccount(t, g, 18, "douyin", "audit-account")
	hub := bridgeInboundHub("douyin", accountID, "audit-conversation", "audit-customer")
	hub.TenantID = 18
	content := "这是一条已经送达的回复"
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContextCustomEdition(t, ctx, hub.Platform, hub.AccountID, "text")
	originalID := ContentHashMsgID("douyin", hub.ConversationID, content)
	original := &model.MessageHub{
		TenantID: 18, Platform: "douyin", AccountID: hub.AccountID,
		ConversationID: hub.ConversationID, MsgID: originalID, Direction: "outbound",
		Status: "delivered", MsgType: "text", Content: content, SentAt: time.Now(),
	}
	if err := g.Create(original).Error; err != nil {
		t.Fatalf("准备已送达原回复失败：%v", err)
	}
	for round := 1; round <= 3; round++ {
		eventID := fmt.Sprintf("evt-audit-round-%d", round)
		releaseReplyClaimForTest(t, eventID)
		p := &ParsedPayload{EventID: eventID, Sender: hub.SenderID, Content: "再次追问", ChatID: hub.ConversationID}
		sent, sendErr := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, content, hub, nil)
		channelErr := AsChannelError(sendErr)
		if sent || channelErr == nil || channelErr.Retryable {
			t.Fatalf("第%d轮重复不得交付或重试：交付=%v 错误=%v", round, sent, sendErr)
		}
		var audit model.MessageHub
		if err := g.Where("msg_id = ?", originalID+":dup").First(&audit).Error; err != nil {
			t.Fatalf("第%d轮应留下可见审计：%v", round, err)
		}
		if audit.Status != "failed" || audit.PushAttempts != 0 || audit.ClaimedAt != nil {
			t.Fatalf("第%d轮审计不得恢复待投：状态=%s 次数=%d", round, audit.Status, audit.PushAttempts)
		}
	}
	var n int64
	if err := g.Model(&model.MessageHub{}).Where("msg_id = ?", originalID+":dup").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("重复审计应保持幂等单行：数量=%d 错误=%v", n, err)
	}
}

func TestSendOutbound_Bridge_AuditSafetyKeepsRealDBFailureRetry(t *testing.T) {
	svc, baseCtx := newBridgeSentTestService(t)
	ctx := tenant.WithTenantID(baseCtx, 18)
	defer svc.Stop(ctx)
	conv := "audit-db-error-" + strings.Repeat("x", 105)
	accountID := seedCanonicalBridgeOutboundAccount(t, svc.eventRepo.GetDB(), 18, "douyin", "audit-db-account")
	hub := bridgeInboundHub("douyin", accountID, conv, "audit-db-customer")
	hub.TenantID = 18
	eventID := "evt-audit-real-db-error"
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContextCustomEdition(t, ctx, hub.Platform, hub.AccountID, "text")
	releaseReplyClaimForTest(t, eventID)
	p := &ParsedPayload{EventID: eventID, Sender: hub.SenderID, Content: "合法客户问题", ChatID: conv}
	sent, err := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "正确但暂时未能入库的回复", hub, nil)
	channelErr := AsChannelError(err)
	if sent || channelErr == nil || !channelErr.Retryable {
		t.Fatalf("真实字段长度错误仍应进入重试而非审计终态：入队=%v 错误=%v", sent, err)
	}
	var retry DelayedOutboundReply
	if err := svc.eventRepo.GetDB().Where("kind = ? AND conversation_id = ?", model.DelayedKindSendRetry, conv).
		First(&retry).Error; err != nil {
		t.Fatalf("真实入库失败应有持久恢复记录：%v", err)
	}
}
