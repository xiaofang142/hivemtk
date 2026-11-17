package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"
)

func TestBridgeOutreachGate_UnconfirmedContextCannotAuthorize(t *testing.T) {
	oldMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(oldMode) })
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺租户", ctx: context.Background()},
		{name: "系统作用域不能授权主动私信", ctx: tenant.WithSystemScope(tenant.OnlyTenant(context.Background(), 18))},
		{name: "已经取消", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(tenant.OnlyTenant(context.Background(), 18))
			cancel()
			return ctx
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if dmOutreachAllowed(tc.ctx, "mtk:dy:dm_outreach:gate-test:"+tc.name) {
				t.Fatal("缺少可信上下文或已经取消不得取得主动触达许可")
			}
		})
	}
}

type outreachGateCache struct {
	cache.Cache
	err      error
	afterSet context.CancelFunc
}

func (c *outreachGateCache) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if c.err != nil {
		return true, c.err
	}
	set, err := c.Cache.SetNX(ctx, key, value, ttl)
	if c.afterSet != nil {
		c.afterSet()
	}
	return set, err
}

func TestBridgeOutreachGate_FailureAndCancellationKeepBudget(t *testing.T) {
	memory := cache.NewMemoryCache()
	t.Cleanup(memory.Close)
	ctx := tenant.OnlyTenant(context.Background(), 18)
	key := "mtk:dy:dm_outreach:fault-account:customer"
	if dmOutreachAllowedWithCache(ctx, key, &outreachGateCache{Cache: memory, err: errors.New("测试缓存故障")}) {
		t.Fatal("即使SetNX返回true，伴随故障也不能确认许可")
	}
	if dmOutreachAllowedWithCache(ctx, key, nil) || dmOutreachAllowedWithCache(nil, key, memory) {
		t.Fatal("缺缓存或上下文必须拒绝")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if dmOutreachAllowedWithCache(cancelCtx, key, &outreachGateCache{Cache: memory, afterSet: cancel}) {
		t.Fatal("占用预算期间取消不能继续外发")
	}
	if dmOutreachAllowedWithCache(ctx, key, memory) {
		t.Fatal("取消不能释放已经占用的冷却预算")
	}
}

func TestBridgeOutreachGate_PreservesKeysAndTenantIsolation(t *testing.T) {
	oldMode := tenant.CurrentMode()
	t.Cleanup(func() { tenant.SetMode(oldMode) })
	memory := cache.NewMemoryCache()
	t.Cleanup(memory.Close)
	key := "mtk:dy:dm_outreach:legacy-account:customer"
	tenant.SetMode(tenant.ModeOff)
	ctx := tenant.ContextWithDefault(context.Background())
	if err := memory.Set(ctx, key, "1", dyDMOutreachCooldown); err != nil {
		t.Fatal(err)
	}
	if dmOutreachAllowedWithCache(ctx, key, memory) || bridgeDMKeyShort("douyin") != "dy" {
		t.Fatal("已有冷却记录和dy键形必须继续生效")
	}
	tenant.SetMode(tenant.ModeOn)
	for _, id := range []uint{18, 19} {
		tenantCtx := tenant.OnlyTenant(context.Background(), id)
		if !dmOutreachAllowedWithCache(tenantCtx, key, memory) || dmOutreachAllowedWithCache(tenantCtx, key, memory) {
			t.Fatalf("租户%d必须独立首次许可且重复受限", id)
		}
	}
}

func TestBridgeOutreachGate_RequiresExplicitIdentity(t *testing.T) {
	for _, channel := range []string{"douyin", "tiktok", "xiaohongshu", "kuaishou", "xianyu"} {
		if !bridgeDMOutreachIdentityValid(channel, "account-a", "customer-a") {
			t.Fatalf("已有合法渠道应保留：%s", channel)
		}
	}
	for _, input := range [][3]string{{"", "a", "b"}, {"other", "a", "b"}, {"douyin", "", "b"}, {"douyin", "default", "b"}, {"douyin", "a", " "}} {
		if bridgeDMOutreachIdentityValid(input[0], input[1], input[2]) {
			t.Fatal("缺明确渠道/账户/收件身份不能猜测外发")
		}
	}
}
