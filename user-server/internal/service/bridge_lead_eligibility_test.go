package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestBridgeLead_OnlyTrustedLiveInboundIsEligible(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sender  string
		history bool
		old     bool
		want    bool
	}{
		{name: "正常实时客户消息", sender: "customer", want: true},
		{name: "历史不能伪标资格", sender: "customer", history: true},
		{name: "系统公告不挖掘", sender: "system"},
		{name: "出站自回显不挖掘", sender: "agent"},
		{name: "过期消息不触发新主动动作", sender: "customer", old: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.MessageHub{})
			svc := NewInboxIngressService()
			svc.hubRepo = repository.NewMessageHubRepositoryWithDB(database)
			svc.cache = nil
			ctx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)
			event := &model.MessageEvent{EventID: "lead-eligibility", Channel: "douyin", SenderID: "customer-one", SenderType: tc.sender,
				ConversationID: "lead-conversation", Content: "请问采购方案和报价", MsgType: "text", Timestamp: time.Now(),
				Extra: map[string]any{"account_id": "lead-account", "bridge_lead_eligible": true}}
			if tc.old {
				event.Timestamp = time.Now().Add(-10 * time.Minute)
			}
			var err error
			if tc.history {
				err = svc.PersistBridgeHistory(ctx, event, "inbound")
			} else {
				_, err = svc.HandleIngressMessage(ctx, event)
			}
			if err != nil {
				t.Fatalf("消息应正常留痕：%v", err)
			}
			var hub model.MessageHub
			if err := database.WithContext(ctx).First(&hub).Error; err != nil {
				t.Fatalf("必须有持久消息：%v", err)
			}
			field := reflect.ValueOf(hub).FieldByName("BridgeLeadEligible")
			if !field.IsValid() {
				t.Fatal("可信资格必须随消息持久保存，不能只在内存队列")
			}
			if field.Bool() != tc.want {
				t.Fatalf("客户/历史/自回显资格错误：实际%v，期望%v", field.Bool(), tc.want)
			}
		})
	}
}
