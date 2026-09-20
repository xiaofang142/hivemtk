package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/platform"

	"github.com/gin-gonic/gin"
)

// R11 之后商户客户端会把"HTTP 200 + 信封 code 非 200"的拒绝上抛，降级分支因此第一次
// 拿到"通了但被拒"这类原因。降级文案必须把它和"根本没通"分开，否则运维会去排查一个
// 并不存在的网络故障，而真实原因是商户被停用/签名不对。

// newPlatformProbeController 用假平台搭一条最小的商户注册链（不碰 DB）。
func newPlatformProbeController(t *testing.T, handler http.HandlerFunc) (*PlatformController, string) {
	t.Helper()
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	withPlatformCfg(t, &config.PlatformConfig{APIURL: srv.URL})
	return &PlatformController{platformClient: platform.NewPlatformClient("test-key")}, srv.URL
}

func serveRegisterMerchant(t *testing.T, pc *PlatformController) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/local/merchant/register", pc.RegisterMerchant)
	req, _ := http.NewRequest("POST", "/local/merchant/register",
		strings.NewReader(`{"name":"某某公司"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("本层降级策略是 200 + 文案，得到 %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v %s", err, w.Body.String())
	}
	return body
}

func TestPlatformController_RegisterMerchant_RefusalCarriesPlatformReason(t *testing.T) {
	pc, _ := newPlatformProbeController(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"code":403,"msg":"商户状态异常，禁止访问"}`)
	})

	body := serveRegisterMerchant(t, pc)
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "商户状态异常，禁止访问") {
		t.Fatalf("降级文案应带上平台原话，得到 %q", msg)
	}
	if strings.Contains(msg, "不可达") {
		t.Fatalf("平台明明答话了，不能报成不可达：%q", msg)
	}
	if strings.Contains(msg, "获取成功") {
		t.Fatalf("拒绝绝不能报成功：%q", msg)
	}
}

func TestPlatformController_RegisterMerchant_UnreachableStillReportsUnreachable(t *testing.T) {
	// 反向闸门：真连不上时不许被改口成"平台拒绝"。
	t.Setenv("MERCHANT_API_SECRET", "test-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead := srv.URL // 关闭后端口即无人监听
	srv.Close()
	withPlatformCfg(t, &config.PlatformConfig{APIURL: dead})
	pc := &PlatformController{platformClient: platform.NewPlatformClient("test-key")}

	body := serveRegisterMerchant(t, pc)
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "不可达") {
		t.Fatalf("连不上时应保持原口径，得到 %q", msg)
	}
	if strings.Contains(msg, "平台拒绝") {
		t.Fatalf("没有响应体可言，不能编出\"平台拒绝\"：%q", msg)
	}
}
