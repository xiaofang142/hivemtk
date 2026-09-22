package controller

// 审计 D-04 的 HTTP 层收口：TikTok 官方签名头必须能穿过控制器的取参白名单到达
// service.Verify。批A 在钉钉上踩过同一个洞（extractHeaders 不含 sign/timestamp，
// service 层单测自己构造 headers 全绿，真实回调却在门口被判缺签名），
// 所以这条契约只能在真实路由上测，不能只在 service 里测。

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	d04Secret    = "tt_client_secret_http_d04"
	d04TS        = "1633174587"
	d04Challenge = `{"event_id":"d04-http-ping","event_type":"url_verification","challenge":"cz1"}`
)

func d04Sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func d04SignBodyOnly(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func postWebhookHeaders(t *testing.T, g *gin.Engine, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}

func newD04TiktokRoute(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	g, _, db := newWebhookRouteEnv(t)
	if err := db.Create(&model.IntegrationAccount{Platform: "tiktok", APISecret: d04Secret, Status: 1}).Error; err != nil {
		t.Fatalf("seed tiktok account: %v", err)
	}
	return g, db
}

// TestWebhookRoute_TikTok_OfficialSignatureHeaderReachesService 带官方 t=/s= 签名的回调
// 必须被受理（200 + accepted:true）。若控制器白名单漏了 TikTok-Signature，
// 这里会以 401 + "missing TikTok-Signature header" 失败 —— 那正是修复前的现场。
func TestWebhookRoute_TikTok_OfficialSignatureHeaderReachesService(t *testing.T) {
	g, _ := newD04TiktokRoute(t)

	body := []byte(d04Challenge)
	rec := postWebhookHeaders(t, g, "/api/webhook/tiktok/1", body, map[string]string{
		"TikTok-Signature": "t=" + d04TS + ",s=" + d04Sign(d04Secret, d04TS, body),
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"accepted":true`) {
		t.Fatalf("官方签名回调必须 200 且 accepted，got %d %s", rec.Code, rec.Body.String())
	}
}

// TestWebhookRoute_TikTok_LegacyAndMissingSignaturesRejected 反向：
// 旧口径（只签 body）与缺头都不得被当作受理，且缺头要报出缺的是哪个头。
func TestWebhookRoute_TikTok_LegacyAndMissingSignaturesRejected(t *testing.T) {
	g, db := newD04TiktokRoute(t)
	body := []byte(d04Challenge)

	cases := []struct {
		name    string
		headers map[string]string
		wantIn  string
	}{
		{"旧 body-only 口径（放在任意受支持头里）", map[string]string{"Signature": d04SignBodyOnly(d04Secret, body)}, "signature"},
		{"完全没有签名头", map[string]string{}, "TikTok-Signature"},
		{"有头但缺 s= 段", map[string]string{"TikTok-Signature": "t=" + d04TS}, "必须同时带"},
	}
	for _, tc := range cases {
		rec := postWebhookHeaders(t, g, "/api/webhook/tiktok/1", body, tc.headers)
		if rec.Code == http.StatusOK {
			t.Errorf("%s 不得被受理，got %d %s", tc.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(tc.wantIn)) {
			t.Errorf("%s 响应理由要点名 %q，got %d %s", tc.name, tc.wantIn, rec.Code, rec.Body.String())
		}
	}
	var n int64
	if err := db.Model(&model.WebhookEvent{}).Where("raw_data LIKE ?", "%d04-http-ping%").Count(&n).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 0 {
		t.Errorf("三条被拒回调不应留下事件行，实际 %d 条", n)
	}
}
