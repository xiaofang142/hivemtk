package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// /api/bridge/* 整组只认 X-Bridge-Token 这一枚凭证头（不认 Authorization），而浏览器发自定义头
// 之前必须先过预检。预检响应里没有这枚头，请求在预检阶段就被浏览器拦下，调用方拿到的报错与
// 「凭证不对」长得完全不同，会被当成服务端故障。这条契约只有浏览器能观察到，所以钉在装配层。

// preflight 是一次预检的读数：状态码、按逗号拆开并去空白的允许头集合、来源回显与 Vary。
type preflight struct {
	code         int
	allowHeaders map[string]bool
	allowOrigin  string
	vary         string
}

// optionsProbe 打一枚不带任何凭证的预检请求。
func optionsProbe(t *testing.T, r *gin.Engine, target, origin string) preflight {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, target, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type,X-Bridge-Token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	raw := w.Header().Get("Access-Control-Allow-Headers")
	if raw == "" {
		t.Fatalf("预检响应没写 Access-Control-Allow-Headers（状态码 %d），后面的成员断言将无牙", w.Code)
	}
	set := map[string]bool{}
	for _, h := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(h); s != "" {
			set[s] = true
		}
	}
	return preflight{
		code:         w.Code,
		allowHeaders: set,
		allowOrigin:  w.Header().Get("Access-Control-Allow-Origin"),
		vary:         strings.Join(w.Header().Values("Vary"), ","),
	}
}

func setupWithBridgeRoutes(t *testing.T) *gin.Engine {
	t.Helper()
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	registered := false
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodGet && ri.Path == "/api/bridge/outbox" {
			registered = true
			break
		}
	}
	if !registered {
		t.Fatal("GET /api/bridge/outbox 未注册，靶子失效（桥接路由整组没挂上，换真实存在的口再来）")
	}
	return r
}

// TestCorsPreflightAdvertisesBridgeTokenHeader 预检必须把 X-Bridge-Token 列进允许头，
// 同时把既有那几枚（Content-Type/Authorization/Last-Event-ID）留着——扩展的 SSE 重连要带
// Last-Event-ID，丢了它等于丢了断线续传。
func TestCorsPreflightAdvertisesBridgeTokenHeader(t *testing.T) {
	r := setupWithBridgeRoutes(t)

	pf := optionsProbe(t, r, "http://localhost:8204/api/bridge/outbox", "http://other.example")
	if pf.code != http.StatusNoContent {
		t.Errorf("预检返回 %d，期望 204 ⇒ CORS 层没有在鉴权前接住 OPTIONS", pf.code)
	}
	for _, want := range []string{"X-Bridge-Token", "Content-Type", "Authorization", "Last-Event-ID"} {
		if !pf.allowHeaders[want] {
			t.Errorf("允许头里没有 %s，浏览器会在预检阶段拦掉带这枚头的请求", want)
		}
	}
	// 反向对账：成员判定不是「整串里含子串」，否则随便一个更长的名字也能把它判绿。
	if pf.allowHeaders["X-Bridge-Token-Not-Advertised"] {
		t.Errorf("允许头集合里出现了没登记的哨兵头，拆开后的成员判定已经不可信")
	}
}

// TestCorsPreflightAnswersBeforeAuth 管理台口（auth 组 + 超管）的预检同样不得要求凭证：
// 浏览器按规范发预检时不会带 Authorization / Cookie，若这层漏到鉴权中间件后面，
// 管理台一整个跨源场景都会以 401 形态死在预检上。
func TestCorsPreflightAnswersBeforeAuth(t *testing.T) {
	r := setupWithBridgeRoutes(t)

	req := httptest.NewRequest(http.MethodOptions, "http://localhost:8204/api/bridge/token/status", nil)
	req.Header.Set("Origin", "http://other.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "X-Bridge-Token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("不带凭证的预检被打到 %d ⇒ 预检没在鉴权之前接住", w.Code)
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("预检返回 %d，期望 204", w.Code)
	}
}

// TestCorsPreflightEchoesAllowlistedOrigin 列入白名单的网页来源要被回显 Allow-Origin、带 Vary，
// 且允许头里必须有 X-Bridge-Token——只回显来源却漏了这枚头，浏览器照样把请求拦在预检。
// 来源白名单是 init 时读 env 的包级变量，这里就地换掉并在 Cleanup 里成对还原。
func TestCorsPreflightEchoesAllowlistedOrigin(t *testing.T) {
	r := setupWithBridgeRoutes(t)

	const listed = "http://console.example"
	before := allowedCORSOrigins
	allowedCORSOrigins = []string{listed}
	t.Cleanup(func() { allowedCORSOrigins = before })

	pf := optionsProbe(t, r, "http://localhost:8204/api/bridge/outbox", listed)
	if pf.code != http.StatusNoContent {
		t.Fatalf("白名单来源的预检返回 %d，期望 204", pf.code)
	}
	if pf.allowOrigin != listed {
		t.Errorf("白名单来源 %q 未被回显（实际 %q）", listed, pf.allowOrigin)
	}
	if !strings.Contains(pf.vary, "Origin") {
		t.Errorf("回显了来源却没带 Vary: Origin（实际 %q），共享缓存会把一个来源的响应发给别的来源", pf.vary)
	}
	if !pf.allowHeaders["X-Bridge-Token"] {
		t.Error("已放行来源却没允许 X-Bridge-Token，带凭证头的请求仍会被浏览器拦在预检")
	}

	// 没列进来的来源不得回显：否则「任意来源 + 允许自定义凭证头」等于把桥接口开放给任何网页。
	// 这一腿也自证上面那枚夹具真参与了判定——同一个口，列进去的回显、没列进去的不回显。
	unlisted := optionsProbe(t, r, "http://localhost:8204/api/bridge/outbox", "http://not-allowlisted.example")
	if unlisted.allowOrigin != "" {
		t.Errorf("未列入白名单的来源被回显 Access-Control-Allow-Origin=%q", unlisted.allowOrigin)
	}
}

// TestCorsSSEPreflightToleratesSameOrigin SSE 那条口额外放行同源来源（部署时不必把控制台域名
// 写进白名单），非 SSE 口没有这条宽容：两处口径本来不同，分开钉住，免得日后「统一一下」
// 把其中一侧悄悄改没。
func TestCorsSSEPreflightToleratesSameOrigin(t *testing.T) {
	r := setupWithBridgeRoutes(t)

	before := allowedCORSOrigins
	allowedCORSOrigins = nil
	t.Cleanup(func() { allowedCORSOrigins = before })

	const origin = "http://localhost:8204"
	pf := optionsProbe(t, r, origin+"/api/bridge/outbox/sse", origin)
	if pf.code != http.StatusNoContent {
		t.Fatalf("同源 SSE 预检返回 %d，期望 204", pf.code)
	}
	if pf.allowOrigin != origin {
		t.Errorf("同源 SSE 预检未回显来源（实际 %q）：白名单为空时控制台自己就断流了", pf.allowOrigin)
	}
	if !pf.allowHeaders["Last-Event-ID"] {
		t.Error("SSE 预检未允许 Last-Event-ID，断线续传带的游标头会被浏览器拦在预检")
	}
}
