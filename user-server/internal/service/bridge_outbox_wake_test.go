package service

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
)

func TestBridgeOutboxWake_IsolatedBoundedSignal(t *testing.T) {
	old := bridgeChannels
	SetBridgeChannels([]string{"douyin", "xiaohongshu"})
	t.Cleanup(func() { bridgeChannels = old })
	b := NewBridgeOutboxWakeBroker()
	a := tenant.OnlyTenant(context.Background(), 18)
	z := tenant.OnlyTenant(context.Background(), 19)
	signals, cancel, err := b.Subscribe(a, "douyin", "account-a")
	if err != nil {
		t.Fatalf("明确范围必须能订阅：%v", err)
	}
	defer cancel()
	foreign, stopForeign, err := b.Subscribe(z, "douyin", "account-a")
	if err != nil {
		t.Fatal(err)
	}
	defer stopForeign()
	for i := 0; i < 100; i++ {
		b.Publish(a, "douyin", "account-a")
	}
	if len(signals) != 1 {
		t.Fatalf("只合并一个无正文信号，实际=%d", len(signals))
	}
	if len(foreign) != 0 {
		t.Fatal("同名账号不能跨租户收到通知")
	}
	if b.Publish(a, "xiaohongshu", "account-a") {
		t.Fatal("不同渠道不能串通知")
	}
	<-signals
	cancel()
	cancel()
	if _, ok := <-signals; ok {
		t.Fatal("取消后信号通道必须关闭")
	}
	if b.Publish(a, "douyin", "account-a") {
		t.Fatal("取消后不能复活订阅")
	}
}

func TestBridgeOutboxWake_RejectsUnconfirmedScope(t *testing.T) {
	b := NewBridgeOutboxWakeBroker()
	for _, ctx := range []context.Context{nil, context.Background(), tenant.WithSystemScope(tenant.OnlyTenant(context.Background(), 18))} {
		if _, _, err := b.Subscribe(ctx, "douyin", "account-a"); err == nil {
			t.Fatal("缺明确租户不得订阅")
		}
		if b.Publish(ctx, "douyin", "account-a") {
			t.Fatal("缺明确租户不得广播")
		}
	}
}

func TestBridgeOutboxWake_OneTenantCannotConsumeAllCapacity(t *testing.T) {
	old := bridgeChannels
	SetBridgeChannels([]string{"douyin"})
	t.Cleanup(func() { bridgeChannels = old })
	b := NewBridgeOutboxWakeBroker()
	a := tenant.OnlyTenant(context.Background(), 18)
	stops := []func(){}
	t.Cleanup(func() {
		for _, stop := range stops {
			stop()
		}
	})
	for i := 0; i < 256; i++ {
		_, stop, err := b.Subscribe(a, "douyin", fmt.Sprintf("account-%d", i))
		if err != nil {
			t.Fatalf("租户预算内正常订阅必须保留：%v", err)
		}
		stops = append(stops, stop)
	}
	_, extraStop, err := b.Subscribe(a, "douyin", "over-budget")
	if err == nil {
		extraStop()
		t.Error("单租户超出256订阅不得继续消耗全局容量")
	}
	_, stopOther, err := b.Subscribe(tenant.OnlyTenant(context.Background(), 19), "douyin", "account-other")
	if err != nil {
		t.Fatalf("另一租户的正常连接不应被单租户预算挤占：%v", err)
	}
	stopOther()
	stops[0]()
	_, stopRecovered, err := b.Subscribe(a, "douyin", "replacement")
	if err != nil {
		t.Fatalf("释放订阅必须恢复预算：%v", err)
	}
	stopRecovered()
}
