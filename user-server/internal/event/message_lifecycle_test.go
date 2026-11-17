package event

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// TestEvent_AuditTopicDelivery 平台事件能投到订阅者，且租户由 PublishCtx 统一盖章。
func TestEvent_AuditTopicDelivery(t *testing.T) {
	prev := GetGlobalBus()
	bus := New(2, 64)
	SetGlobalBus(bus)
	t.Cleanup(func() {
		bus.Stop()
		SetGlobalBus(prev)
	})

	got := make(chan MessageLifecycleEvent, 1)
	bus.Subscribe(TopicCustomerMessageEdited, func(evt Event) error {
		if p, ok := evt.Payload.(MessageLifecycleEvent); ok {
			got <- p
		}
		return nil
	})

	ctx := tenant.WithTenantID(context.Background(), 7)
	PublishCtx(ctx, TopicCustomerMessageEdited, MessageLifecycleEvent{
		EventID:    "m-1",
		Channel:    "telegram",
		EventType:  "message.edited",
		OccurredAt: time.Now(),
	})

	select {
	case p := <-got:
		if p.TenantID != 7 {
			t.Fatalf("订阅者应拿到盖章后的租户，实际 %d", p.TenantID)
		}
		if p.EventID != "m-1" {
			t.Fatalf("载荷丢失：%+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未收到 edited 事件")
	}
}

// TestEvent_NoSubscriberDoesNotBlock 无订阅者时发布必须立即返回（bus 是 best-effort 旁路）。
func TestEvent_NoSubscriberDoesNotBlock(t *testing.T) {
	prev := GetGlobalBus()
	bus := New(2, 16)
	SetGlobalBus(bus)
	t.Cleanup(func() {
		bus.Stop()
		SetGlobalBus(prev)
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 16; i++ {
			PublishCtx(context.Background(), TopicCustomerGroupMemberChanged, MessageLifecycleEvent{EventID: "e"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("无订阅者/队列满时发布会阻塞")
	}
}
