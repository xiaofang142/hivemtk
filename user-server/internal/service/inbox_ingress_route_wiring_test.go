package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestIngress_DiscardRuleStopsPipeline discard 规则：不落库、不触发 AI、必须留审计。
func TestIngress_DiscardRuleStopsPipeline(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelRouteRule{}, &model.ChannelEventAudit{})
	ctx := context.Background()

	const (
		tenantKey = "7001"
		tenant    = uint(7001)
	)
	repo := repository.NewChannelRouteRuleRepository(db)
	rule := &model.ChannelRouteRule{
		Channel:   model.ChannelTelegram,
		EventType: "message.*",
		Target:    model.ChannelRouteTargetDiscard,
		Priority:  10,
		Enabled:   true,
		Note:      "测试：全员消息丢弃",
	}
	if err := repo.Upsert(ctx, tenant, rule); err != nil {
		t.Fatalf("建规则: %v", err)
	}
	if rule.ID == 0 {
		t.Fatal("规则应有自增 id")
	}

	svc := NewInboxIngressService()
	evt := &model.MessageEvent{
		EventID:        "route-discard-evt-1",
		Channel:        model.ChannelTelegram,
		SenderID:       "u-route-1",
		ConversationID: "chat-route-1",
		MsgType:        model.MsgTypeText,
		Content:        "这条应被规则丢弃",
		Extra:          map[string]any{"tenant_id": tenantKey, "account_id": "acc-route"},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Accepted {
		t.Fatal("discard 规则命中时 Accepted 必须 false")
	}
	if res.QueuedForAI {
		t.Fatal("discard 规则命中时不得触发 AI")
	}
	if res.RouteRuleID != rule.ID {
		t.Fatalf("结果应记录命中规则 id=%d，实际 %d", rule.ID, res.RouteRuleID)
	}

	var hubCount int64
	db.Model(&model.MessageHub{}).Where("platform=? AND account_id=?", model.ChannelTelegram, "acc-route").Count(&hubCount)
	if hubCount != 0 {
		t.Fatalf("discard 不得落 message_hub，实际 %d 行", hubCount)
	}
	var audit model.ChannelEventAudit
	if err := db.Where("channel=? AND channel_msg_id=?", model.ChannelTelegram, evt.EventID).First(&audit).Error; err != nil {
		t.Fatalf("discard 必须留审计：%v", err)
	}
	if audit.RouteRuleID != rule.ID {
		t.Fatalf("审计应带命中规则 id，实际 %d", audit.RouteRuleID)
	}
	if audit.Payload["decision"] != "discard" {
		t.Fatalf("审计 payload 应标 decision=discard，实际 %+v", audit.Payload)
	}
}

// TestIngress_RouteRuleIDRecorded 非 discard 规则命中时仍走原路径，但结果要带规则 id（可观测）。
func TestIngress_RouteRuleIDRecorded(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelRouteRule{}, &model.ChannelEventAudit{})
	ctx := context.Background()

	const (
		tenantKey = "7002"
		tenant    = uint(7002)
	)
	repo := repository.NewChannelRouteRuleRepository(db)
	rule := &model.ChannelRouteRule{
		Channel:   model.ChannelTelegram,
		EventType: "message",
		Target:    model.ChannelRouteTargetAgent,
		Priority:  20,
		Enabled:   true,
	}
	if err := repo.Upsert(ctx, tenant, rule); err != nil {
		t.Fatalf("建规则: %v", err)
	}

	svc := NewInboxIngressService()
	evt := &model.MessageEvent{
		EventID:        "route-agent-evt-1",
		Channel:        model.ChannelTelegram,
		SenderID:       "u-route-2",
		ConversationID: "chat-route-2",
		MsgType:        model.MsgTypeText,
		Content:        "正常消息",
		Extra:          map[string]any{"tenant_id": tenantKey, "account_id": "acc-agent"},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.RouteRuleID != rule.ID {
		t.Fatalf("结果应带命中规则 id=%d，实际 %d", rule.ID, res.RouteRuleID)
	}
	if !res.Accepted {
		t.Fatal("agent 规则不应阻断入站")
	}
}

// TestIngress_NoRuleKeepsLegacyBehavior 无规则租户的行为必须与接线前一致：正常接收。
func TestIngress_NoRuleKeepsLegacyBehavior(t *testing.T) {
	testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelRouteRule{}, &model.ChannelEventAudit{})
	svc := NewInboxIngressService()
	evt := &model.MessageEvent{
		EventID:        "route-none-evt-1",
		Channel:        model.ChannelTelegram,
		SenderID:       "u-route-3",
		ConversationID: "chat-route-3",
		MsgType:        model.MsgTypeText,
		Content:        "无规则租户",
		Extra:          map[string]any{"tenant_id": "7999", "account_id": "acc-none"},
	}
	res, err := svc.HandleIngressMessage(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("无规则时应正常接收，实际 reason=%q", res.Reason)
	}
	if res.RouteRuleID != 0 {
		t.Fatalf("无规则时 RouteRuleID 应为 0，实际 %d", res.RouteRuleID)
	}
}

// TestChannelRouteRule_TenantIsolation 仓储层租户隔离：A 租户读不到 B 租户规则。
func TestChannelRouteRule_TenantIsolation(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.ChannelRouteRule{})
	ctx := context.Background()
	repo := repository.NewChannelRouteRuleRepository(db)

	a := &model.ChannelRouteRule{Channel: "telegram", Target: model.ChannelRouteTargetDiscard, Enabled: true}
	b := &model.ChannelRouteRule{Channel: "feishu", Target: model.ChannelRouteTargetAgent, Enabled: true}
	if err := repo.Upsert(ctx, 11, a); err != nil {
		t.Fatalf("A 规则: %v", err)
	}
	if err := repo.Upsert(ctx, 12, b); err != nil {
		t.Fatalf("B 规则: %v", err)
	}

	got, err := repo.ListByTenant(ctx, 11)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range got {
		if r.TenantID != 11 {
			t.Fatalf("A 租户列表出现越界行：%+v", r)
		}
	}
	// 跨租户改/删必须命中 0 行
	if err := repo.Upsert(ctx, 12, &model.ChannelRouteRule{ID: a.ID, Channel: "hacked"}); err == nil {
		t.Fatal("B 租户不应能改 A 租户的规则")
	}
	ok, err := repo.Delete(ctx, 12, a.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok {
		t.Fatal("B 租户不应能删 A 租户的规则")
	}
}
