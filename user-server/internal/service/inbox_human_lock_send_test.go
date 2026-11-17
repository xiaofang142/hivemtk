package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/redis/go-redis/v9"
)

func TestConfirmHumanLockBeforeSend(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		readErr error
		locked  bool
		wantErr bool
	}{
		{name: "已接管", value: "true", locked: true},
		{name: "明确解锁", value: "false"},
		{name: "Redis未命中", readErr: redis.Nil},
		{name: "Memory未命中", readErr: cache.ErrCacheMiss},
		{name: "连接故障", readErr: errors.New("合成连接故障"), wantErr: true},
		{name: "未知值", value: "unknown", wantErr: true},
		{name: "空值不能当未命中", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &InboxIngressService{cache: &resultExitLockValueCache{value: tc.value, err: tc.readErr}}
			locked, err := svc.ConfirmHumanLockBeforeSend(context.Background(), "confirmation-session")
			if locked != tc.locked || (err != nil) != tc.wantErr {
				t.Fatalf("锁确认结果不符：持有=%v 错误=%v", locked, err)
			}
		})
	}
	var missing *InboxIngressService
	for _, svc := range []*InboxIngressService{missing, {}} {
		if _, err := svc.ConfirmHumanLockBeforeSend(context.Background(), "confirmation-session"); err == nil {
			t.Fatal("缺服务或缓存必须报告不可确认")
		}
	}
	if _, err := (&InboxIngressService{cache: &redisNilCache{}}).ConfirmHumanLockBeforeSend(context.Background(), ""); err == nil {
		t.Fatal("缺会话身份必须报告不可确认")
	}
}

func TestConfirmHumanLockBeforeSend_ContextScope(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	memory := cache.NewMemoryCache()
	t.Cleanup(func() { memory.Close() })
	svc := &InboxIngressService{cache: memory}
	ctx := tenant.OnlyTenant(context.Background(), 18)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺上下文"},
		{name: "缺SaaS租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(ctx)},
		{name: "已取消", ctx: canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ConfirmHumanLockBeforeSend(tc.ctx, "scope-session"); err == nil {
				t.Fatal("未确认上下文不能取得发送许可")
			}
		})
	}
	if err := memory.Set(ctx, InboxHumanLockKey+"scope-session", "true", 0); err != nil {
		t.Fatal(err)
	}
	locked, err := svc.ConfirmHumanLockBeforeSend(ctx, "scope-session")
	if err != nil || !locked {
		t.Fatalf("本租户人工锁必须命中：%v %v", locked, err)
	}
	locked, err = svc.ConfirmHumanLockBeforeSend(tenant.OnlyTenant(context.Background(), 19), "scope-session")
	if err != nil || locked {
		t.Fatalf("另一租户不能串用锁：%v %v", locked, err)
	}
	// off模式也保留明确租户，不能把显式18改成默认1。
	tenant.SetMode(tenant.ModeOff)
	if _, err := svc.ConfirmHumanLockBeforeSend(context.Background(), "default-session"); err != nil {
		t.Fatal(err)
	}
}
