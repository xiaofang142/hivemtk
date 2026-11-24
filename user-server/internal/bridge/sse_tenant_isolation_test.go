package bridge

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
)

// 同一渠道账号在两个租户的订阅键必须分离；缺租户发布不得命中任一租户。
func TestSSEBus_TenantKeyIsolation(t *testing.T) {
	// 多租户隔离语义只在 ModeOn 下成立；off 模式全实例单租户，键统一落默认租户。
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	bus := NewSSEBus()
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	chA, cancelA := bus.SubscribeCtx(ctxA, "douyin", "acc-tenant")
	defer cancelA()
	chB, cancelB := bus.SubscribeCtx(ctxB, "douyin", "acc-tenant")
	defer cancelB()

	event := SSEEvent{
		ID: "tenant-isolation", Event: "inbound_message",
		ConversationID: "conv-isolated",
		Data:           map[string]any{"platform": "douyin", "account_id": "acc-tenant"},
	}
	bus.PublishCtx(tenant.WithTenantID(context.Background(), 41), event)
	select {
	case got := <-chA:
		if got.ConversationID != event.ConversationID {
			t.Fatalf("A收到错误会话: %s", got.ConversationID)
		}
	case <-make(chan struct{}, 1):
		t.Fatal("同租户订阅者未收到事件")
	}
	select {
	case got := <-chB:
		t.Fatalf("跨租户零投递失败，B收到%v", got)
	default:
	}

	bus.PublishCtx(tenant.WithTenantID(context.Background(), 42), event)
	select {
	case got := <-chB:
		if got.ConversationID != event.ConversationID {
			t.Fatalf("B收到错误会话: %s", got.ConversationID)
		}
	default:
		t.Fatal("B租户订阅者未收到事件")
	}
	select {
	case got := <-chA:
		t.Fatalf("第二次跨租户零投递失败，A收到%v", got)
	default:
	}

	bus.PublishCtx(context.Background(), event)
	select {
	case got := <-chA:
		t.Fatalf("缺租户发布命中A: %v", got)
	case got := <-chB:
		t.Fatalf("缺租户发布命中B: %v", got)
	default:
	}
}

// 在线判定同样按可信租户键，不得把另一租户的同账号订阅当成在线。
func TestSSEBus_HasSubscribersTenantScoped(t *testing.T) {
	// 多租户隔离语义只在 ModeOn 下成立；off 模式全实例单租户，键统一落默认租户。
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	bus := NewSSEBus()
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	_, cancelA := bus.SubscribeCtx(ctxA, "douyin", "acc-online")
	defer cancelA()
	if !bus.HasSubscribersCtx(ctxA, "douyin", "acc-online") {
		t.Fatal("A应判在线")
	}
	if bus.HasSubscribersCtx(ctxB, "douyin", "acc-online") {
		t.Fatal("B把A订阅误判在线")
	}
	if bus.HasSubscribers("douyin", "acc-online") {
		t.Fatal("兼容无租户口不得把有租户订阅误判在线")
	}
}

// 判据补充：重连/退出分别断言跨租户零正文、零在线误判。
// 订阅/广播/在线键在 SubscribeCtx / PublishCtx / hasSubscribersFor 三处都从可信 ctx 派生，
// 重连走的就是同一条 SubscribeCtx，故退订后重建不会串到对方租户的命名空间。
func TestSSEBus_TenantScopedReconnectAndExit(t *testing.T) {
	// 多租户隔离语义只在 ModeOn 下成立；off 模式全实例单租户，键统一落默认租户。
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	bus := NewSSEBus()
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	event := SSEEvent{
		ID: "reconnect", Event: "inbound_message",
		ConversationID: "conv-reconnect",
		Data:           map[string]any{"platform": "douyin", "account_id": "acc-reconnect"},
	}

	// A 退订后重新订阅（重连），再发布：A 的新连接不得收到 B 的正文。
	_, cancelA1 := bus.SubscribeCtx(ctxA, "douyin", "acc-reconnect")
	cancelA1()
	chA2, cancelA2raw := bus.SubscribeCtx(ctxA, "douyin", "acc-reconnect")
	canceled := false
	cancelA2 := func() {
		if !canceled {
			canceled = true
			cancelA2raw()
		}
	}
	defer cancelA2()
	chB, cancelB := bus.SubscribeCtx(ctxB, "douyin", "acc-reconnect")
	defer cancelB()

	bus.PublishCtx(ctxB, event)
	select {
	case got := <-chB:
		if got.ID != event.ID {
			t.Fatalf("B收到错误事件: %v", got)
		}
	default:
		t.Fatal("B租户订阅者未收到事件")
	}
	select {
	case got := <-chA2:
		t.Fatalf("重连后跨租户零投递失败，A收到%v", got)
	default:
	}

	// A 退出：退订后在线判定必须回到离线，且 A 发布不得命中 B。
	cancelA2()
	if bus.HasSubscribersCtx(ctxA, "douyin", "acc-reconnect") {
		t.Fatal("退订后A仍被判在线")
	}
	bus.PublishCtx(ctxA, event)
	select {
	case got := <-chB:
		t.Fatalf("已退出租户的发布命中B: %v", got)
	default:
	}
}

// 判据补充：订阅满（buffer 容量耗尽）不得造成跨租户投递或在线误判。
func TestSSEBus_TenantScopedBufferFull(t *testing.T) {
	// 多租户隔离语义只在 ModeOn 下成立；off 模式全实例单租户，键统一落默认租户。
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	bus := NewSSEBus()
	bus.buffer = 1
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	_, cancelA := bus.SubscribeCtx(ctxA, "douyin", "acc-full")
	defer cancelA()
	chB, cancelB := bus.SubscribeCtx(ctxB, "douyin", "acc-full")
	defer cancelB()

	// A 租户队列塞满，丢弃只发生在 A 的命名空间。
	for i := 0; i < 5; i++ {
		bus.PublishCtx(ctxA, SSEEvent{
			ID: fmt.Sprintf("a-%d", i), Event: "inbound_message",
			Data: map[string]any{"platform": "douyin", "account_id": "acc-full"},
		})
	}
	select {
	case got := <-chB:
		t.Fatalf("A租户订阅满导致跨租户投递: %v", got)
	default:
	}
	if !bus.HasSubscribersCtx(ctxB, "douyin", "acc-full") {
		t.Fatal("B租户订阅被A租户订阅满误判为离线")
	}
}
