package router

// 商业闸顺序的常驻门：JWT/Tenant 必须先于商业资格，Subscription/Module 必须先于
// RoutePolicy/DataScope。Gin 在注册路由时快照 handler 链，所以这里既检查真实 Setup
// 的 live 链，也打真实请求；只看“链上有守卫”抓不到 I-01 这种顺序缺陷。

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/tenant"
)

const (
	guardTenant       = "TenantMiddleware"
	guardSubscription = "GuardSubscriptionPath"
	guardModule       = "GuardModulePath"
)

// commercialGateProbe 同时实现两个 checker 接口，并记录真实请求里的执行序列。
// tenantContextOK 证明商业闸拿到的不是裸 context，而是 TenantMiddleware 写入的租户上下文。
type commercialGateProbe struct {
	events           []string
	moduleEnabled    bool
	subscriptionLock bool
	tenantContextOK  bool
}

func (p *commercialGateProbe) record(ctx context.Context, name string) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 {
		p.tenantContextOK = false
	}
	p.events = append(p.events, name)
}

func (p *commercialGateProbe) IsEnabled(ctx context.Context, _ string) bool {
	p.record(ctx, "module")
	return p.moduleEnabled
}

func (p *commercialGateProbe) IsLocked(ctx context.Context) bool {
	p.record(ctx, "subscription")
	return p.subscriptionLock
}

func setTenantModeForTest(t *testing.T, mode tenant.Mode) {
	t.Helper()
	previous := tenant.CurrentMode()
	tenant.SetMode(mode)
	t.Cleanup(func() { tenant.SetMode(previous) })
}

func installCommercialGateProbe(t *testing.T, probe *commercialGateProbe) {
	t.Helper()
	middleware.SetSubscriptionLockChecker(probe)
	middleware.SetModuleGateChecker(probe)
	t.Cleanup(func() {
		middleware.SetSubscriptionLockChecker(nil)
		middleware.SetModuleGateChecker(nil)
	})
}

func chainGuardIndex(chain []string, guard string) int {
	for i, name := range chain {
		if strings.Contains(name, guard) {
			return i
		}
	}
	return -1
}

// TestCommercialGateChain_PrecedesPermissionGuards 直接抓生产 Setup 注册到 live 路由上的
// handler 链。选两条真实路由：GEO 口覆盖收费模块/能力面，会话转写口覆盖 DataScope 登记。
func TestCommercialGateChain_PrecedesPermissionGuards(t *testing.T) {
	setTenantModeForTest(t, tenant.ModeOn)
	chains := liveRouteGuardChains(t)

	for _, key := range []string{
		"GET /api/geo/config",
		"GET /api/customer-sessions/:id/transcript",
	} {
		chain, ok := chains[key]
		if !ok {
			t.Errorf("live 路由表里没有 %s，顺序判据无从谈起", key)
			continue
		}
		previous := -1
		for _, guard := range []string{
			guardJWT, guardTenant, guardSubscription, guardModule, guardPolicy, guardDataScope,
		} {
			index := chainGuardIndex(chain, guard)
			if index < 0 {
				t.Errorf("%s 链上没有 %s：%v", key, guard, chain)
				break
			}
			if index <= previous {
				t.Errorf("%s 的守卫顺序错误：%s 位于 %d，应晚于前一道守卫的 %d；链=%v",
					key, guard, index, previous, chain)
				break
			}
			previous = index
		}
	}
}

// TestCommercialGateOrder_EntitlementPrecedesCapability 打真实 Setup 引擎，并用 fake
// checker/provider 记录执行序列。商业拒绝时不仅响应码要正确，角色权限 provider 也不能被调用。
func TestCommercialGateOrder_EntitlementPrecedesCapability(t *testing.T) {
	setTenantModeForTest(t, tenant.ModeOn)
	engine, _ := gateEngineAndDB(t)
	probe := &commercialGateProbe{tenantContextOK: true}
	installCommercialGateProbe(t, probe)
	middleware.SetMenuGrantProvider(func(_ context.Context, _ string) (map[string]struct{}, error) {
		probe.events = append(probe.events, "policy")
		return map[string]struct{}{}, nil
	})
	t.Cleanup(func() { middleware.SetMenuGrantProvider(nil) })

	staff, _ := gateTokens(t)
	if owner := middleware.ModuleOwnerOf("/api/geo/config"); owner != "geo" {
		t.Fatalf("测试靶子失效：/api/geo/config 模块归属=%q，期望 geo", owner)
	}
	if len(middleware.CapabilityOwnerKeysFor("/api/geo/config")) == 0 {
		t.Fatal("测试靶子失效：/api/geo/config 不在能力面表内，证不了角色权限顺序")
	}

	cases := []struct {
		name              string
		locked            bool
		enabled           bool
		wantHTTP          int
		wantBusinessCode  int
		wantEvents        []string
		wantCapabilityMsg bool
	}{
		{
			name:             "订阅到期优先于模块与角色权限",
			locked:           true,
			enabled:          false,
			wantHTTP:         http.StatusOK,
			wantBusinessCode: errors.CodeSubscriptionExpired,
			wantEvents:       []string{"subscription"},
		},
		{
			name:             "模块未开通优先于角色权限",
			locked:           false,
			enabled:          false,
			wantHTTP:         http.StatusOK,
			wantBusinessCode: errors.CodeModuleNotEnabled,
			wantEvents:       []string{"subscription", "module"},
		},
		{
			name:              "商业闸放行后才进入角色权限",
			locked:            false,
			enabled:           true,
			wantHTTP:          http.StatusForbidden,
			wantBusinessCode:  http.StatusForbidden,
			wantEvents:        []string{"subscription", "module", "policy"},
			wantCapabilityMsg: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe.events = nil
			probe.subscriptionLock = tc.locked
			probe.moduleEnabled = tc.enabled
			probe.tenantContextOK = true
			middleware.InvalidateMenuGrants("")

			code, body := gateCall(t, engine, http.MethodGet, "/api/geo/config", staff)
			if code != tc.wantHTTP {
				t.Errorf("HTTP 状态期望 %d，实得 %d：%s", tc.wantHTTP, code, strings.TrimSpace(body))
			}
			var payload struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("响应体不是 JSON：%v / %s", err, body)
			}
			if payload.Code != tc.wantBusinessCode {
				t.Errorf("响应业务码期望 %d，实得 %d：%s", tc.wantBusinessCode, payload.Code, strings.TrimSpace(body))
			}
			if !slices.Equal(probe.events, tc.wantEvents) {
				t.Errorf("守卫执行序列期望 %v，实得 %v（商业闸必须先于角色权限）", tc.wantEvents, probe.events)
			}
			if !probe.tenantContextOK {
				t.Error("商业 checker 未收到 TenantMiddleware 写入的租户上下文")
			}
			if got := strings.Contains(body, capabilityDeniedMsg); got != tc.wantCapabilityMsg {
				t.Errorf("能力面拒绝文案出现=%v，期望 %v：%s", got, tc.wantCapabilityMsg, strings.TrimSpace(body))
			}
		})
	}
}

// TestCommercialGateOrder_AnonymousStopsAtJWT 反向控制：顺序调整不能把商业闸提到鉴权之前，
// 匿名请求仍应得到 JWT 401，而不是泄露模块/订阅状态。
func TestCommercialGateOrder_AnonymousStopsAtJWT(t *testing.T) {
	setTenantModeForTest(t, tenant.ModeOn)
	engine, _ := gateEngineAndDB(t)
	probe := &commercialGateProbe{tenantContextOK: true}
	installCommercialGateProbe(t, probe)

	code, body := gateCall(t, engine, http.MethodGet, "/api/geo/config", "")
	if code != http.StatusUnauthorized || !strings.Contains(body, jwtNoTokenMsg) {
		t.Errorf("匿名请求应被 JWT 拦成 401+%q，实得 %d / %s", jwtNoTokenMsg, code, strings.TrimSpace(body))
	}
	if len(probe.events) != 0 {
		t.Errorf("匿名请求不应调用商业 checker，实得 %v", probe.events)
	}
}
