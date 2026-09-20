package platform

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// R12：/merchant-api/license/status 这个端点平台从未实现（开源版还专门把 License 相关字段
// 与接口一并删了），于是探测恒拿 404 ⇒ DegradeReason 判 unreachable ⇒ /health 与 app-config
// 在平台明明健康时也报故障。下面四条用例把"探测打哪儿、怎么分类"钉住。

// TestCheckConnection_HitsPlatformLivenessAndNeedsNoAuth 探测必须打平台真实存在的存活性端点，
// 且不搭商户签名与 JWT —— 连通性探针一旦绑到鉴权链上，就等于再造两类假故障。
func TestCheckConnection_HitsPlatformLivenessAndNeedsNoAuth(t *testing.T) {
	paths := make(chan string, 8)
	auths := make(chan string, 8)
	keys := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		auths <- r.Header.Get("Authorization")
		keys <- r.Header.Get("X-Merchant-Key")
		_, _ = w.Write([]byte(`{"status":"alive","timestamp":1700000000}`))
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	if err := NewPlatformClient("mk").CheckConnection(); err != nil {
		t.Fatalf("平台活着就该判可达，得到 %v", err)
	}
	if got := recvWithin(t, paths); got != "/health" {
		t.Fatalf("探测端点=%q，want /health（不得再打从未存在的 license 端点）", got)
	}
	if got := recvWithin(t, auths); got != "" {
		t.Errorf("存活性探测不应带 JWT 头，得到 %q", got)
	}
	if got := recvWithin(t, keys); got != "" {
		t.Errorf("存活性探测不应做商户签名，得到 %q", got)
	}
}

// TestCheckConnection_NotConfiguredIsSentinel 没接平台必须命中哨兵，让 DegradeReason 判成
// not_configured —— 私域独立部署里这是常态，报成 unreachable 就是把常态刷成故障。
func TestCheckConnection_NotConfiguredIsSentinel(t *testing.T) {
	withPlatformConfig(t, nil)

	err := NewPlatformClient("mk").CheckConnection()
	if !errors.Is(err, ErrPlatformNotConfigured) {
		t.Fatalf("未配置平台时必须命中哨兵，得到 %v", err)
	}
	if got := DegradeReason(err); got != "not_configured" {
		t.Errorf("DegradeReason=%q，want not_configured", got)
	}
}

// TestCheckConnection_Non200IsUnreachable 平台返回 503（就绪性检查会主动这么回）必须判故障。
func TestCheckConnection_Non200IsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	err := NewPlatformClient("mk").CheckConnection()
	if err == nil {
		t.Fatal("503 必须返回错误")
	}
	if got := DegradeReason(err); got != "unreachable" {
		t.Errorf("DegradeReason=%q，want unreachable", got)
	}
}

// TestCheckConnection_LivenessEndpointMissingIsUnreachable 反向闸门：探测打到一个没有 /health
// 的地址时必须判不可达，否则"探测点写错"会被读成"平台健康"。
func TestCheckConnection_LivenessEndpointMissingIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	if err := NewPlatformClient("mk").CheckConnection(); err == nil {
		t.Fatal("没有存活性端点时不能判可达")
	}
}

// recvWithin 取 handler 回传的观察值。handler 跑在服务器 goroutine 上，
// 所以值只能经带缓冲 channel 传来，不能直读闭包共享变量（-race 会当场报竞态）。
func recvWithin(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("3s 内没等到探测请求，说明请求根本没发出")
		return ""
	}
}
