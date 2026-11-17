package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
)

// TestPublishPlatformEvent_TopicPerType 事件类型 → topic 的映射必须逐类正确：
// 投错 topic 会让「新消息」订阅者去触发 AI（幽灵回复）。
func TestPublishPlatformEvent_TopicPerType(t *testing.T) {
	prev := event.GetGlobalBus()
	bus := event.New(2, 128)
	event.SetGlobalBus(bus)
	t.Cleanup(func() {
		bus.Stop()
		event.SetGlobalBus(prev)
	})

	cases := []struct {
		eventType string
		topic     string
	}{
		{model.EventTypeMessageEdited, event.TopicCustomerMessageEdited},
		{model.EventTypeMessageDeleted, event.TopicCustomerMessageDeleted},
		{model.EventTypeMessageReaction, event.TopicCustomerMessageReaction},
		{model.EventTypeInteractionCallback, event.TopicCustomerInteractionCallback},
		{model.EventTypeGroupMemberJoined, event.TopicCustomerGroupMemberChanged},
		{model.EventTypeBotInvited, event.TopicCustomerGroupMemberChanged},
	}
	for _, c := range cases {
		hit := make(chan struct{}, 1)
		bus.Subscribe(c.topic, func(evt event.Event) error {
			hit <- struct{}{}
			return nil
		})
		publishPlatformEvent(context.Background(), &model.MessageEvent{
			EventID: "e-1", Channel: model.ChannelTelegram, EventType: c.eventType,
			SenderID: "u1", Timestamp: time.Now(),
		}, "acc-1")
		select {
		case <-hit:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s 未投到 %s", c.eventType, c.topic)
		}
	}
}

// TestPublishPlatformEvent_DoesNotUseReceivedTopic 平台事件绝不能混进「新消息」topic。
func TestPublishPlatformEvent_DoesNotUseReceivedTopic(t *testing.T) {
	prev := event.GetGlobalBus()
	bus := event.New(2, 64)
	event.SetGlobalBus(bus)
	t.Cleanup(func() {
		bus.Stop()
		event.SetGlobalBus(prev)
	})

	leaked := make(chan struct{}, 1)
	bus.Subscribe(event.TopicCustomerMessageReceived, func(evt event.Event) error {
		leaked <- struct{}{}
		return nil
	})
	publishPlatformEvent(context.Background(), &model.MessageEvent{
		EventID: "e-2", Channel: model.ChannelTelegram, EventType: model.EventTypeGroupMemberLeft,
	}, "")
	select {
	case <-leaked:
		t.Fatal("成员事件不得投到 customer.message.received")
	case <-time.After(300 * time.Millisecond):
	}
}
