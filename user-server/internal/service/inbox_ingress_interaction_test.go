package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestFeishuPlatformEventType 飞书事件类型映射（未知类型必须返回空串，不能瞎归类）。
func TestFeishuPlatformEventType(t *testing.T) {
	mk := func(et string) *struct {
		EventType    string          `json:"event_type"`
		AppID        string          `json:"app_id"`
		TenantKey    string          `json:"tenant_key"`
		EventID      string          `json:"event_id"`
		Token        string          `json:"token"`
		CreateTime   feishuTimestamp `json:"create_time"`
		AppSecretVer int             `json:"app_secret_ver"`
	} {
		return &struct {
			EventType    string          `json:"event_type"`
			AppID        string          `json:"app_id"`
			TenantKey    string          `json:"tenant_key"`
			EventID      string          `json:"event_id"`
			Token        string          `json:"token"`
			CreateTime   feishuTimestamp `json:"create_time"`
			AppSecretVer int             `json:"app_secret_ver"`
		}{EventType: et}
	}
	cases := map[string]string{
		"im.message.recalled_v1":         model.EventTypeMessageDeleted,
		"im.chat.member.user.added_v1":   model.EventTypeGroupMemberJoined,
		"im.chat.member.user.deleted_v1": model.EventTypeGroupMemberLeft,
		"im.message.receive_v1":          "",
		"some.future.event":              "",
	}
	for in, want := range cases {
		if got := feishuPlatformEventType(mk(in)); got != want {
			t.Fatalf("%s → 期望 %q，实际 %q", in, want, got)
		}
	}
	if feishuPlatformEventType(nil) != "" {
		t.Fatal("header 缺失时应返回空串")
	}
}

// TestIngress_InteractionCallback_NoMessageRow 交互回调不得在 message_hub 留下任何行。
func TestIngress_InteractionCallback_NoMessageRow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelInteraction{}, &model.ChannelEventAudit{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const account = "acct-interaction"
	evt := &model.MessageEvent{
		EventID:        "tg_cb_acct-interaction_cb-1",
		EventType:      model.EventTypeInteractionCallback,
		Channel:        model.ChannelTelegram,
		SenderID:       "u-1",
		ConversationID: "chat-1",
		Content:        "approve:42",
		Extra: map[string]any{
			"account_id":      account,
			"interaction_id":  "tg_cb_acct-interaction_cb-1",
			"action":          "approve:42",
			"platform_msg_id": "555",
		},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.QueuedForAI {
		t.Fatal("交互回调不得触发 AI")
	}
	if res.InteractionID == "" {
		t.Fatal("结果应带出 interaction_id")
	}
	var hubCount int64
	db.Model(&model.MessageHub{}).Where("platform=? AND account_id=?", model.ChannelTelegram, account).Count(&hubCount)
	if hubCount != 0 {
		t.Fatalf("交互回调不得产生 message_hub 行，实际 %d", hubCount)
	}
	var inter model.ChannelInteraction
	if err := db.Where("channel=? AND interaction_id=?", model.ChannelTelegram, res.InteractionID).First(&inter).Error; err != nil {
		t.Fatalf("交互行未落库: %v", err)
	}
	if inter.Status != model.ChannelInteractionPending {
		t.Fatalf("首次回调应为 pending，实际 %q", inter.Status)
	}
	if inter.PlatformMsgID != "555" || inter.Action != "approve:42" {
		t.Fatalf("交互行字段丢失：%+v", inter)
	}
}

// TestIngress_InteractionCallback_DuplicateIsNoop 平台重投同一回调：不建第二行、不重复审计。
func TestIngress_InteractionCallback_DuplicateIsNoop(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelInteraction{}, &model.ChannelEventAudit{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const account = "acct-interaction-dup"
	evt := &model.MessageEvent{
		EventID:        "tg_cb_acct-interaction-dup_cb-2",
		EventType:      model.EventTypeInteractionCallback,
		Channel:        model.ChannelTelegram,
		SenderID:       "u-2",
		ConversationID: "chat-2",
		Content:        "approve:43",
		Extra: map[string]any{
			"account_id":     account,
			"interaction_id": "tg_cb_acct-interaction-dup_cb-2",
			"action":         "approve:43",
		},
	}
	first, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Reason == second.Reason {
		t.Fatalf("重投与首次的 reason 应可区分，都是 %q", first.Reason)
	}
	if second.Reason != "interaction.callback.replayed" {
		t.Fatalf("重投 reason 应为 interaction.callback.replayed，实际 %q", second.Reason)
	}

	var n int64
	db.Model(&model.ChannelInteraction{}).
		Where("channel=? AND interaction_id=?", model.ChannelTelegram, first.InteractionID).Count(&n)
	if n != 1 {
		t.Fatalf("同键两次应只有 1 行，实际 %d", n)
	}
	var audits int64
	db.Model(&model.ChannelEventAudit{}).
		Where("channel=? AND channel_msg_id=?", model.ChannelTelegram, evt.EventID).Count(&audits)
	if audits != 1 {
		t.Fatalf("重投不应重复审计，实际 %d 行", audits)
	}
}

// TestIngress_InteractionCallback_MarkAnsweredOnlyOnce 答复状态只能从 pending 迁一次。
func TestIngress_InteractionCallback_MarkAnsweredOnlyOnce(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.ChannelInteraction{}, &model.ChannelEventAudit{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	evt := &model.MessageEvent{
		EventID:        "tg_cb_acct-answer_cb-3",
		EventType:      model.EventTypeInteractionCallback,
		Channel:        model.ChannelTelegram,
		SenderID:       "u-3",
		ConversationID: "chat-3",
		Content:        "ok",
		Extra: map[string]any{
			"account_id":     "acct-answer",
			"interaction_id": "tg_cb_acct-answer_cb-3",
		},
	}
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("ingress: %v", err)
	}
	if !svc.MarkInteractionAnswered(ctx, model.ChannelTelegram, res.InteractionID) {
		t.Fatal("首次 MarkAnswered 应命中")
	}
	if svc.MarkInteractionAnswered(ctx, model.ChannelTelegram, res.InteractionID) {
		t.Fatal("重复 MarkAnswered 必须返回 false（已是 answered）")
	}
	var row model.ChannelInteraction
	if err := db.Where("interaction_id=?", res.InteractionID).First(&row).Error; err != nil {
		t.Fatalf("查交互行: %v", err)
	}
	if row.Status != model.ChannelInteractionAnswered || row.AnsweredAt == nil {
		t.Fatalf("状态应为 answered 且有时间：%+v", row)
	}
}
