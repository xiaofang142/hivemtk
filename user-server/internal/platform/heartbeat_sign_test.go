package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
)

// TestReportHeartbeat_SignedWhenMerchantKeyPresent L-15：带 merchant_key 的心跳
// 必须携带三头（X-Merchant-Key/X-Timestamp/X-Signature），且签名可用平台同口径复算
// （payload = method+"\n"+path+"\n"+timestamp+"\n"+body，secret=PlatformCfg.Secret）。
func TestReportHeartbeat_SignedWhenMerchantKeyPresent(t *testing.T) {
	const secret = "l15-shared-secret"
	var gotHeaders http.Header
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: secret})
	c := &Client{httpClient: http.DefaultClient, merchantSecret: secret}
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "ins-1", MerchantKey: "mk-9", Version: "v1"}); err != nil {
		t.Fatalf("签名心跳应成功: %v", err)
	}

	if gotHeaders.Get("X-Merchant-Key") != "mk-9" {
		t.Fatalf("X-Merchant-Key 应与 body 声明一致，实际 %q", gotHeaders.Get("X-Merchant-Key"))
	}
	ts := gotHeaders.Get("X-Timestamp")
	if ts == "" {
		t.Fatalf("X-Timestamp 缺失")
	}
	payload := "POST" + "\n" + "/api/platform/heartbeat" + "\n" + ts + "\n" + string(gotBody)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if gotHeaders.Get("X-Signature") != want {
		t.Fatalf("签名与平台口径复算不一致：got %q want %q", gotHeaders.Get("X-Signature"), want)
	}
	if !strings.Contains(string(gotBody), `"merchant_key":"mk-9"`) {
		t.Fatalf("body 应保留 merchant_key 声明: %s", gotBody)
	}
}

// TestReportHeartbeat_DegradesToPureHeartbeatWhenSecretMissing secret 缺失：
// 降级为纯心跳（剥离 merchant_key 声明，不携带签名头），保活统计面。
func TestReportHeartbeat_DegradesToPureHeartbeatWhenSecretMissing(t *testing.T) {
	var gotHeaders http.Header
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: ""})
	c := &Client{httpClient: http.DefaultClient, merchantSecret: ""}
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "ins-2", MerchantKey: "mk-9", Version: "v1"}); err != nil {
		t.Fatalf("降级纯心跳应成功: %v", err)
	}
	if gotHeaders.Get("X-Signature") != "" {
		t.Fatalf("降级路径不应携带签名头")
	}
	var req ReportHeartbeatReq
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("body 解析失败: %v", err)
	}
	if req.MerchantKey != "" {
		t.Fatalf("降级应剥离 merchant_key，实际 %q", req.MerchantKey)
	}
}
