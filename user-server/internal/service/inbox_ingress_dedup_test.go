package service

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
)

func TestInboxIngress_BatchMerge_Scenarios(t *testing.T) {
	ctx := context.Background()

	c := cache.NewMemoryCache()

	defer c.Close()
	svc := NewInboxIngressServiceWithDB(nil, c)

	trigger := &fakeAITrigger{}
	svc.SetAITrigger(trigger)

	makeEvent := func(channel, accountID, convID, content string, seq int) *model.MessageEvent {
		return &model.MessageEvent{
			Channel:        channel,
			SenderID:       "customer-" + convID,
			Content:        content,
			EventID:        fmt.Sprintf("evt-%s-%s-%d", channel, convID, seq),
			ConversationID: convID,
			MsgType:        model.MsgTypeText,
			Extra:          map[string]interface{}{"account_id": accountID},
		}
	}

	t.Run("场景1_相同会话多条消息_合并一次AI回复", func(t *testing.T) {
		trigger.called = 0
		convID := "conv-batch-1"

		events := []*model.MessageEvent{
			makeEvent(model.ChannelXHS, "xhs-acct-1", convID, "你好", 0),
			makeEvent(model.ChannelXHS, "xhs-acct-1", convID, "请问这个商品还在吗？", 1),
			makeEvent(model.ChannelXHS, "xhs-acct-1", convID, "多少钱？", 2),
		}

		result, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 1 {
			t.Fatalf("3 条同会话消息应合并触发 1 次 AI，实际: %d", trigger.called)
		}
		if !result.TriggeredAI {
			t.Fatal("TriggeredAI 应为 true")
		}
		expectedMerged := "你好\n请问这个商品还在吗？\n多少钱？"
		if trigger.lastContent != expectedMerged {
			t.Fatalf("合并内容应为 %q，实际: %q", expectedMerged, trigger.lastContent)
		}
		if len(result.PerEvent) != 3 {
			t.Fatalf("应返回 3 个 PerEvent，实际: %d", len(result.PerEvent))
		}
		for i, r := range result.PerEvent {
			if !r.Accepted {
				t.Fatalf("第 %d 条消息应被接受，实际: %+v", i, r)
			}
		}
		t.Logf("✅ 3 条同会话消息合并触发 1 次 AI: merged_content=%q", trigger.lastContent)
	})

	t.Run("场景2_不同会话_各自触发AI", func(t *testing.T) {
		trigger.called = 0

		events := []*model.MessageEvent{
			makeEvent(model.ChannelXHS, "xhs-acct-1", "conv-A", "这个多少钱？", 0),
			makeEvent(model.ChannelXHS, "xhs-acct-1", "conv-B", "那个多少钱？", 1),
		}

		result, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 2 {
			t.Fatalf("2 个不同会话应触发 2 次 AI，实际: %d", trigger.called)
		}
		if !result.TriggeredAI {
			t.Fatal("TriggeredAI 应为 true")
		}
		t.Logf("✅ 2 个不同会话各自触发 AI: 调用次数=%d", trigger.called)
	})

	t.Run("场景3_不同渠道相同会话_合并触发", func(t *testing.T) {
		trigger.called = 0
		convID := "conv-cross-channel"

		events := []*model.MessageEvent{
			makeEvent(model.ChannelXHS, "xhs-acct-1", convID, "小红书消息", 0),
			makeEvent(model.ChannelDouyin, "dy-acct-1", convID, "抖音消息", 1),
		}

		_, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 1 {
			t.Fatalf("相同会话不同渠道应合并触发 1 次 AI，实际: %d", trigger.called)
		}
		expectedMerged := "小红书消息\n抖音消息"
		if trigger.lastContent != expectedMerged {
			t.Fatalf("合并内容应为 %q，实际: %q", expectedMerged, trigger.lastContent)
		}
		t.Logf("✅ 不同渠道相同会话合并触发: merged=%q", trigger.lastContent)
	})

	t.Run("场景4_不同账号不同会话_各自触发AI", func(t *testing.T) {
		trigger.called = 0

		events := []*model.MessageEvent{
			{
				Channel:        model.ChannelXHS,
				SenderID:       "customer-accountA",
				Content:        "账号A消息",
				EventID:        "evt-xhs-acctA-0",
				ConversationID: "conv-cross-account-A",
				MsgType:        model.MsgTypeText,
				Extra:          map[string]interface{}{"account_id": "xhs-acct-A"},
			},
			{
				Channel:        model.ChannelXHS,
				SenderID:       "customer-accountB",
				Content:        "账号B消息",
				EventID:        "evt-xhs-acctB-1",
				ConversationID: "conv-cross-account-B",
				MsgType:        model.MsgTypeText,
				Extra:          map[string]interface{}{"account_id": "xhs-acct-B"},
			},
		}

		result, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 2 {
			t.Fatalf("2 个不同账号不同会话应触发 2 次 AI，实际: %d", trigger.called)
		}
		if !result.TriggeredAI {
			t.Fatal("TriggeredAI 应为 true")
		}
		t.Logf("✅ 不同账号不同会话各自触发 AI: 调用次数=%d", trigger.called)
	})

	t.Run("场景5_sender_type平等处理", func(t *testing.T) {
		trigger.called = 0

		events := []*model.MessageEvent{
			{
				Channel:        model.ChannelXHS,
				SenderID:       "customer-1",
				Content:        "客户消息",
				EventID:        "evt-customer-0",
				ConversationID: "conv-filter-1",
				SenderType:     "customer",
				MsgType:        model.MsgTypeText,
				Extra:          map[string]interface{}{"account_id": "xhs-acct-1"},
			},
			{
				Channel:        model.ChannelXHS,
				SenderID:       "self-1",
				Content:        "自己发的消息",
				EventID:        "evt-self-1",
				ConversationID: "conv-filter-1",
				SenderType:     "customer",
				MsgType:        model.MsgTypeText,
				Extra:          map[string]interface{}{"account_id": "xhs-acct-1"},
			},
		}

		result, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 1 {
			t.Fatalf("同会话 2 条消息应合并触发 1 次 AI，实际: %d", trigger.called)
		}
		if len(result.PerEvent) != 2 {
			t.Fatalf("应返回 2 个 PerEvent，实际: %d", len(result.PerEvent))
		}
		if result.PerEvent[0].Accepted != true || result.PerEvent[1].Accepted != true {
			t.Fatalf("两条消息均应被接受: %+v / %+v", result.PerEvent[0], result.PerEvent[1])
		}
		t.Logf("✅ sender_type 不再区分：两条消息同等对待，批次合并触发 1 次 AI")
	})

	t.Run("场景6_系统消息仅落库", func(t *testing.T) {
		trigger.called = 0

		events := []*model.MessageEvent{
			{
				Channel:        model.ChannelXHS,
				SenderID:       "system-1",
				Content:        "系统通知",
				EventID:        "evt-system-0",
				ConversationID: "conv-system-1",
				SenderType:     "system",
				MsgType:        model.MsgTypeText,
				Extra:          map[string]interface{}{"account_id": "xhs-acct-1"},
			},
		}

		result, err := svc.HandleIngressBatch(ctx, events)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 0 {
			t.Fatalf("系统消息不应触发 AI，实际: %d", trigger.called)
		}
		if result.TriggeredAI {
			t.Fatal("TriggeredAI 应为 false")
		}
		t.Logf("✅ 系统消息仅落库不触发 AI")
	})

	t.Run("场景7_空batch", func(t *testing.T) {
		trigger.called = 0

		result, err := svc.HandleIngressBatch(ctx, nil)
		if err != nil {
			t.Fatalf("HandleIngressBatch: %v", err)
		}
		if trigger.called != 0 {
			t.Fatalf("空 batch 不应触发 AI，实际: %d", trigger.called)
		}
		if result.TriggeredAI {
			t.Fatal("TriggeredAI 应为 false")
		}
		t.Logf("✅ 空 batch 不触发 AI")
	})
}
