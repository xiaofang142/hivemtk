package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
)

// 订阅闸测试（003 T022）。
//
// 与模块闸同一条纪律：钉的是「expired 态下**哪些口必须活**、哪些口必须死**」——
// 白名单漏一条 = 用户付不了费（砖头）；客户面误锁 = 商户存量客户的 AI 应答断线。

// fakeLockChecker 可编排的订阅锁判定
type fakeLockChecker struct{ locked bool }

func (f fakeLockChecker) IsLocked(_ context.Context) bool { return f.locked }

func withLockChecker(t *testing.T, c subscriptionLockChecker) {
	t.Helper()
	SetSubscriptionLockChecker(c)
	t.Cleanup(func() { SetSubscriptionLockChecker(nil) })
}

func withOnMode(t *testing.T) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
}

// runThroughAny 把中间件挂到 /*path 通配路由上跑任意路径。
// 与 module_gate_test.runThroughPath 同判据：handler 未执行 = 被拦。
func runThroughAny(mw gin.HandlerFunc, path string) bool {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	reachedHandler := false
	r.GET("/*path", mw, func(c *gin.Context) { reachedHandler = true })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if reachedHandler {
		return false
	}
	if w.Code != http.StatusOK {
		return true
	}
	var body struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code != 0
}

// TestSubscriptionGateOffModeAlwaysAllows off 模式（离线单机）没有订阅概念，
// 误挂也不许锁 —— 离线用户被锁死是不可接受的回归。
func TestSubscriptionGateOffModeAlwaysAllows(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	withLockChecker(t, fakeLockChecker{locked: true}) // 判定说锁，模式说不锁 → 模式赢
	for _, p := range []string{"/api/clues", "/api/customers", "/api/analytics/x"} {
		if runThroughAny(GuardSubscriptionPath(), p) {
			t.Fatalf("off 模式下 %s 被拦（应放行：离线无订阅概念）", p)
		}
	}
}

// TestSubscriptionGateNilCheckerAllows 未注入判定 = 装配缺失 → 放行（与模块闸同口径）。
func TestSubscriptionGateNilCheckerAllows(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, nil)
	if runThroughAny(GuardSubscriptionPath(), "/api/clues") {
		t.Fatal("checker 未注入时被拦（应放行：装配缺失不该把系统锁死）")
	}
}

// TestSubscriptionGateLocksBusinessPaths expired 态：有模块归属的业务面必须拦 4305。
func TestSubscriptionGateLocksBusinessPaths(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: true})
	// 这些路径在 module_route_patterns 表里有归属（对账：ModuleOwnerOf 非空）
	for _, p := range []string{
		"/api/customer/oneid/list", // customer 模块
		"/api/geo/keywords/list",   // geo 模块
		"/api/community/groups",    // im-intl 模块
	} {
		if owner := ModuleOwnerOf(p); owner == "" {
			t.Fatalf("测试靶子失效：路径 %s 在模式表里无归属，测不出「有归属→拦」", p)
		}
		if !runThroughAny(GuardSubscriptionPath(), p) {
			t.Fatalf("expired 态下 %s 未被拦（应 4305：有模块归属的业务面）", p)
		}
	}
}

// TestSubscriptionGateAllowsWhitelist expired 态：续费必需面白名单一条都不能漏。
// 本表与 middleware/subscription_guard.go 的三张清单对账 —— 改白名单必须同批改这里。
func TestSubscriptionGateAllowsWhitelist(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: true})
	allowed := []string{
		// /api/auth/*：登录/登出/改密（logout 也在这前缀下）
		"/api/auth/login",
		"/api/auth/logout",
		"/api/auth/change-password",
		// /api/public/*：公开配置（登录页/注册页锁态也要渲染）
		"/api/public/config",
		"/api/public/geo",
		// /api/module-market/*：浏览/下单/pay-status（市场页依赖集，防死循环 P1-12）
		"/api/module-market/list",
		"/api/module-market/detail/crm",
		"/api/module-market/order",
		"/api/module-market/orders",
		"/api/module-market/orders/MO123/pay-status",
		"/api/module-market/payment-guide",
		// /api/subscription/*：订阅状态 + 续费下单（墙页依赖集）
		"/api/subscription/status",
		"/api/subscription/renew",
		// sync / my：续费成功后的恢复通道 + 购买引导面
		"/api/modules/sync",
		"/api/modules/my",
	}
	for _, p := range allowed {
		if runThroughAny(GuardSubscriptionPath(), p) {
			t.Fatalf("expired 态下白名单路径 %s 被拦（续费面必须活）", p)
		}
	}
}

// TestSubscriptionGateAllowsCustomerFacing expired 态：客户面显式不锁（产品裁决）。
// 锁了这些 = 商户存量客户的 AI 应答断线，比"员工摸鱼"严重一个量级。
func TestSubscriptionGateAllowsCustomerFacing(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: true})
	allowed := []string{
		"/api/chat/public/sessions/tok123", // widget 对话面
		"/api/chat/public/sessions/tok123/messages",
		"/ws/chat",               // chat WebSocket（引擎级）
		"/api/ws/visitor",        // 访客 WebSocket
		"/api/channelbus/ingest", // 总线 ingress（扩展端上报）
		"/api/channelbus/outbox", // 总线 outbox
		"/api/channelbus/outbox/ack",
		"/api/channelbus/outbox/sse",
		"/api/webhook/wechat/account1", // wechat webhook
		"/api/webhook/wechat/account1/msg",
	}
	for _, p := range allowed {
		if runThroughAny(GuardSubscriptionPath(), p) {
			t.Fatalf("expired 态下客户面路径 %s 被拦（客户面显式不锁，产品裁决）", p)
		}
	}
}

// TestSubscriptionGateUncoveredPathPassesWithWarn expired 态：无模块归属的路径
// 放行（与 4301 未覆盖路由同风险等级取舍：新接口漏归模块不该全站 403）。
func TestSubscriptionGateUncoveredPathPassesWithWarn(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: true})
	// /api/analytics/ai-performance 有归属（core）；挑一个确实无归属的
	p := "/api/monitor-trace-eval-not-exist/xyz"
	if owner := ModuleOwnerOf(p); owner != "" {
		t.Fatalf("测试靶子失效：路径 %s 竟有归属 %s", p, owner)
	}
	if runThroughAny(GuardSubscriptionPath(), p) {
		t.Fatalf("expired 态下无归属路径 %s 被拦（应放行 + WARN）", p)
	}
}

// TestSubscriptionWhitelistCoversRenewalSurface 白名单完整性对账（修订 #1「逐路径列全」）。
// 直接对三张清单做包含断言：新增续费必需接口却忘了进白名单 → 这里红。
func TestSubscriptionWhitelistCoversRenewalSurface(t *testing.T) {
	// 续费墙页依赖的接口集（oracle P1-12：这些不得返回 4305，否则死循环）
	renewalSurface := []string{
		"/api/subscription/status",
		"/api/subscription/renew",
		"/api/module-market/orders/MO1/pay-status",
		"/api/modules/sync",
		"/api/auth/login",
		"/api/auth/logout",
		"/api/public/config",
	}
	for _, p := range renewalSurface {
		if !subscriptionAllowPath(p) {
			t.Fatalf("续费依赖路径 %s 不在白名单（墙页/市场页会 4305 死循环）", p)
		}
	}
	// 市场页依赖集
	marketSurface := []string{
		"/api/module-market/list",
		"/api/module-market/detail/crm",
		"/api/module-market/order",
		"/api/module-market/orders",
		"/api/modules/my",
	}
	for _, p := range marketSurface {
		if !subscriptionAllowPath(p) {
			t.Fatalf("市场页依赖路径 %s 不在白名单", p)
		}
	}
	// 客户面清单对账
	customerFacing := []string{
		"/api/chat/public/sessions/x",
		// "/ws/chat" 已随 D-7 删除（2026-10-23）：端点与死代码一起移除，白名单行同步撤下；
		// 客户面不再有该路径，未来上真坐席 WS 时按 /api/ws/visitor 的口径重新登记。
		"/api/ws/visitor",
		"/api/channelbus/ingest",
		"/api/channelbus/outbox",
		"/api/channelbus/outbox/sse",
		"/api/webhook/wechat/a1",
	}
	for _, p := range customerFacing {
		if !subscriptionAllowPath(p) {
			t.Fatalf("客户面路径 %s 不在放行清单（expired 会断商户客户应答）", p)
		}
	}
}

// TestSubscriptionGateDelivers4305 被拦时业务码必须是 4305（不是 4301/4302）。
func TestSubscriptionGateDelivers4305(t *testing.T) {
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: true})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/*path", GuardSubscriptionPath(), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/customer/oneid/list", nil))
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体不是 JSON: %v / %s", err, w.Body.String())
	}
	if body.Code != 4305 {
		t.Fatalf("expired 业务码 = %d，want 4305（body: %s）", body.Code, w.Body.String())
	}
}

// TestSubscriptionGateAllowsWhenNotLocked 未到期（active/trial）：全部放行。
func TestSubscriptionGateAllowsWhenNotLocked(t *testing.T) {
	// 未到期（active/trial）：全部放行
	withOnMode(t)
	withLockChecker(t, fakeLockChecker{locked: false})
	for _, p := range []string{"/api/customer/oneid/list", "/api/geo/keywords/list", "/api/community/groups"} {
		if runThroughAny(GuardSubscriptionPath(), p) {
			t.Fatalf("未到期态下 %s 被拦（应放行）", p)
		}
	}
}
