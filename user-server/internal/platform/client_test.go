package platform

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// TestClient_Do_401SelfHeal 验证遇 401 时清空 token 并重试一次，最终成功（V6 自愈）。
func TestClient_Do_401SelfHeal(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"msg":"unauthorized"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BaseResp{Code: 0, Msg: "ok"})
	}))
	defer srv.Close()

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})
	c := NewPlatformClient("test-key")

	var resp BaseResp
	if err := c.Do("GET", "/test", nil, &resp); err != nil {
		t.Fatalf("expected success after 401 retry, got: %v", err)
	}
	if hits != 2 {
		t.Fatalf("expected 2 hits (1x401 + 1x200), got %d", hits)
	}
}

// TestClient_Do_StructuredError 验证非 2xx 返回结构化 *PlatformError，透传状态码与业务 msg。
func TestClient_Do_StructuredError(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
	}))
	defer srv.Close()

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})
	c := NewPlatformClient("test-key")

	var resp BaseResp
	err := c.Do("GET", "/missing", nil, &resp)
	perr, ok := err.(*PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if perr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", perr.StatusCode)
	}
	if perr.Msg() != "not found" {
		t.Fatalf("expected msg 'not found', got %q", perr.Msg())
	}
}

// --- R11：平台成功与拒绝都是 HTTP 200，真值只在信封 code 里 ---

// refusalServer 模拟平台业务口：HTTP 200 + 信封 code 非 200（response.Error 的写法）。
// 命中路径经带缓冲 channel 回传：handler 跑在服务器 goroutine 上，共享变量会被 -race 判竞态；
// 且"必须报错"型断言只有先证明请求真的发出去了，才不会把签名失败/根本没发也当成通过。
func refusalServer(t *testing.T, code int, msg string) (*httptest.Server, chan string) {
	t.Helper()
	paths := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"code":%d,"msg":%q}`, code, msg)
	}))
	t.Cleanup(srv.Close)
	return srv, paths
}

// mustHaveHit 确认服务端真收到过请求；收不到就是假绿，直接判失败。
func mustHaveHit(t *testing.T, paths chan string, want string) {
	t.Helper()
	select {
	case got := <-paths:
		if got != want {
			t.Fatalf("请求打到 %s，期望 %s", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("平台侧从未收到 %s 请求，断言的是根本没发生的分支", want)
	}
}

func TestClient_Do_EnvelopeRefusalIsError(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv, paths := refusalServer(t, http.StatusConflict, "该邮箱已被注册")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("test-key")
	var resp BaseResp
	err := c.Do("POST", "/merchant-api/merchant/register", RegisterMerchantReq{Name: "x"}, &resp)
	mustHaveHit(t, paths, "/merchant-api/merchant/register")

	perr, ok := err.(*PlatformError)
	if !ok {
		t.Fatalf("信封 code=409 必须判失败，得到 %T: %v", err, err)
	}
	if perr.StatusCode != http.StatusOK {
		t.Fatalf("HTTP 状态码应原样保留 200，得到 %d", perr.StatusCode)
	}
	if perr.Resp == nil || perr.Resp.Code != http.StatusConflict {
		t.Fatalf("业务码应为 409，得到 %+v", perr.Resp)
	}
	if perr.Msg() != "该邮箱已被注册" {
		t.Fatalf("应透传平台 msg，得到 %q", perr.Msg())
	}
}

// TestClient_RegisterMerchant_RefusalIsError 钉住最坏后果：平台拒绝时不能既返回 nil
// 又打出"商户注册成功"——那会让营销链路以为已拿到 per-merchant 密钥继续往下跑。
func TestClient_RegisterMerchant_RefusalIsError(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	t.Setenv("MERCHANT_STATE_DIR", t.TempDir())
	srv, paths := refusalServer(t, http.StatusBadRequest, "参数错误")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("test-key")
	err := c.RegisterMerchant(RegisterMerchantReq{Name: "某某公司"})
	mustHaveHit(t, paths, "/merchant-api/merchant/register")
	if err == nil {
		t.Fatal("平台返回 code=400 参数错误，RegisterMerchant 却报成功")
	}
	if c.merchantSecret != "" {
		t.Fatalf("拒绝响应不该改动签名密钥，得到 %q", c.merchantSecret)
	}
}

// TestClient_Do_EnvelopeSuccessStillParsesData 反向闸门：严格化不能把成功响应一起打死。
func TestClient_Do_EnvelopeSuccessStillParsesData(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":200,"msg":"ok","data":{"key":"m-1","secret":"s-1"}}`)
	}))
	defer srv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("test-key")
	var resp BaseResp
	if err := c.Do("POST", "/merchant-api/merchant/register", RegisterMerchantReq{Name: "x"}, &resp); err != nil {
		t.Fatalf("code=200 应成功，得到 %v", err)
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("信封 code 应透传给调用方，得到 %d", resp.Code)
	}
	var data struct {
		Key    string `json:"key"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("data 应可解析: %v", err)
	}
	if data.Secret != "s-1" {
		t.Fatalf("data 内容丢失，得到 %+v", data)
	}
}

// TestClient_Do_BareBodyWithoutCodePassesThrough 边界：不是信封的响应不能凭空造出拒绝
// （无 code 键即判"未知格式"，交回调用方按自己的结构解析）。
func TestClient_Do_BareBodyWithoutCodePassesThrough(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"active","remaining_days":3}`)
	}))
	defer srv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("test-key")
	var out struct {
		Status    string `json:"status"`
		Remaining int    `json:"remaining_days"`
	}
	if err := c.Do("GET", "/merchant-api/profile", nil, &out); err != nil {
		t.Fatalf("裸 body 无 code 键时应原样交回调用方，得到 %v", err)
	}
	if out.Status != "active" || out.Remaining != 3 {
		t.Fatalf("裸 body 未被解析，得到 %+v", out)
	}
}

func TestReportInstall_RefusalIsError(t *testing.T) {
	srv, paths := refusalServer(t, http.StatusBadRequest, "参数错误")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("")
	err := c.ReportInstall(&ReportInstallReq{InstallID: "i-1"})
	mustHaveHit(t, paths, "/api/platform/install")
	if err == nil {
		t.Fatal("平台 code=400 拒绝上报，ReportInstall 却报成功")
	}
	if !strings.Contains(err.Error(), "参数错误") {
		t.Fatalf("错误里应带上平台拒绝原因，得到 %q", err.Error())
	}
}

func TestReportHeartbeat_RefusalIsError(t *testing.T) {
	srv, paths := refusalServer(t, http.StatusInternalServerError, "心跳处理失败")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("")
	err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "i-1"})
	mustHaveHit(t, paths, "/api/platform/heartbeat")
	if err == nil {
		t.Fatal("平台 code=500 拒绝心跳，ReportHeartbeat 却报成功")
	}
}

// TestReportHeartbeat_SuccessStillNil 反向闸门：成功信封不能被严格化打成失败。
func TestReportHeartbeat_SuccessStillNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":200,"msg":"","data":{"received":true}}`)
	}))
	defer srv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})

	c := NewPlatformClient("")
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "i-1"}); err != nil {
		t.Fatalf("code=200 应成功，得到 %v", err)
	}
}
