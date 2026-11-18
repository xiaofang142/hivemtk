package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func evidenceEvent(kind, account string) *model.MessageEvent {
	return &model.MessageEvent{
		EventID: "evidence-event", EventType: kind, Channel: model.ChannelTelegram,
		SenderID: "evidence-customer", ConversationID: "evidence-conversation",
		Extra: map[string]any{"account_id": account, "interaction_id": "evidence-interaction", "tenant_id": "18"},
	}
}

func TestIMEventEvidence_TenantComesOnlyFromContext(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	for _, kind := range []string{model.EventTypeGroupMemberJoined, model.EventTypeInteractionCallback} {
		t.Run(kind, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.MessageHub{}, &model.ChannelEventAudit{}, &model.ChannelInteraction{})
			svc := NewInboxIngressService()
			svc.routeEngine = nil
			trigger := &fakeAITrigger{}
			svc.SetAITrigger(trigger)
			ctx := tenant.OnlyTenant(context.Background(), 18)
			ev := evidenceEvent(kind, "evidence-account")
			ev.Extra["tenant_id"] = "19"
			result, err := svc.HandleIngressMessage(ctx, ev)
			if err != nil || result == nil || !result.Accepted {
				t.Fatalf("合法上下文应持久接受事件，不采用Extra伪租户：结果%+v，错误%v", result, err)
			}
			var audits []model.ChannelEventAudit
			if err := database.WithContext(ctx).Find(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if len(audits) != 1 || audits[0].TenantID != "18" {
				t.Fatalf("权威租户18必须有唯一证据，实际%+v", audits)
			}
			if kind == model.EventTypeInteractionCallback {
				var interactions []model.ChannelInteraction
				if err := database.WithContext(ctx).Find(&interactions).Error; err != nil {
					t.Fatal(err)
				}
				if len(interactions) != 1 || interactions[0].TenantID != "18" {
					t.Fatalf("交互租户不正确：%+v", interactions)
				}
			}
			var count int64
			if err := database.WithContext(ctx).Model(&model.MessageHub{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 || trigger.called != 0 || result.QueuedForAI {
				t.Fatal("非消息事件不得产生消息或触发AI")
			}
		})
	}
}

func TestIMEventEvidence_AccountsHaveIndependentIdempotency(t *testing.T) {
	for _, kind := range []string{model.EventTypeGroupMemberJoined, model.EventTypeInteractionCallback} {
		t.Run(kind, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.ChannelEventAudit{}, &model.ChannelInteraction{})
			svc := NewInboxIngressService()
			svc.routeEngine = nil
			ctx := tenant.OnlyTenant(context.Background(), 18)
			for _, account := range []string{"account-a", "account-b", "account-a"} {
				result, err := svc.HandleIngressMessage(ctx, evidenceEvent(kind, account))
				if err != nil || result == nil || !result.Accepted {
					t.Fatalf("合法事件与重投应接受：%+v，%v", result, err)
				}
			}
			var audits int64
			if err := database.WithContext(ctx).Model(&model.ChannelEventAudit{}).Count(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if audits != 2 {
				t.Fatalf("不同账号必须各有证据，同账号重投不重复：实际%d", audits)
			}
			if kind == model.EventTypeInteractionCallback {
				var interactions int64
				if err := database.WithContext(ctx).Model(&model.ChannelInteraction{}).Count(&interactions).Error; err != nil {
					t.Fatal(err)
				}
				if interactions != 2 {
					t.Fatalf("交互不能跨账号去重：实际%d", interactions)
				}
			}
		})
	}
}

func TestIMEventEvidence_PersistenceFailureIsNotAccepted(t *testing.T) {
	for _, tc := range []struct{ name, kind, failedTable string }{
		{"成员审计失败", model.EventTypeGroupMemberJoined, "channel_event_audits"},
		{"交互审计失败", model.EventTypeInteractionCallback, "channel_event_audits"},
		{"交互登记失败", model.EventTypeInteractionCallback, "channel_interactions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.ChannelEventAudit{}, &model.ChannelInteraction{})
			svc := NewInboxIngressService()
			svc.routeEngine = nil
			ctx := tenant.OnlyTenant(context.Background(), 18)
			fault := errors.New("合成证据写入失败")
			const callback = "test.im.evidence.failure"
			if err := database.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == tc.failedTable {
					tx.AddError(fault)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Callback().Create().Remove(callback) })
			result, err := svc.HandleIngressMessage(ctx, evidenceEvent(tc.kind, "fault-account"))
			if err == nil || result == nil || result.Accepted || result.QueuedForAI {
				t.Fatalf("证据故障必须明确拒绝：结果%+v，错误%v", result, err)
			}
			var count int64
			if err := database.WithContext(ctx).Model(&model.ChannelInteraction{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("交互及其审计必须同提交或同回滚：残留%d", count)
			}
		})
	}
}
